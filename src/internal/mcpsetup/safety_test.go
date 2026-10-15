// @module MCPSetupSafetyTests pins interruption recovery and profile isolation.
package mcpsetup

import (
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPendingApplyRecoveryAndUndoExactBytes(t *testing.T) {
	for _, client := range []string{"codex", "claude-code"} {
		t.Run(client, func(t *testing.T) {
			o := options(t, client)
			original := "theme = \"dark\""
			if client == "claude-code" {
				original = `{ "unrelated": 12345678901234567890123456789 }`
			}
			put(t, o.ConfigPath, original)
			if _, e := Apply(o); e != nil {
				t.Fatal(e)
			}
			installed := get(t, o.ConfigPath)
			// Simulate durable intent+backup followed by interruption before config rename.
			put(t, o.ConfigPath, original)
			different := o
			different.Args = []string{"changed"}
			if _, e := Apply(different); e == nil {
				t.Fatal("resumed a different command")
			}
			if r, e := Apply(o); e != nil || !r.Changed || string(get(t, o.ConfigPath)) != string(installed) {
				t.Fatalf("resume %+v %v", r, e)
			}
			if _, e := Undo(o); e != nil {
				t.Fatal(e)
			}
			if string(get(t, o.ConfigPath)) != original {
				t.Fatal("undo changed original formatting or numbers")
			}
		})
	}
}
func TestPendingUndoRetriesByExactOutcomeHash(t *testing.T) {
	o := options(t, "codex")
	put(t, o.ConfigPath, "theme=\"dark\"\n")
	if _, e := Apply(o); e != nil {
		t.Fatal(e)
	}
	receiptPath := filepath.Join(o.StateDir, "ownership.json")
	var record receipt
	if e := json.Unmarshal(get(t, receiptPath), &record); e != nil {
		t.Fatal(e)
	}
	put(t, o.ConfigPath, string(get(t, o.ConfigPath))+"[other]\nnew=true\n")
	after, e := removeOwned(o, get(t, o.ConfigPath), record)
	if e != nil {
		t.Fatal(e)
	}
	record.UndoHash = digest(after)
	encoded, _ := json.Marshal(record)
	put(t, receiptPath, string(encoded))
	put(t, o.ConfigPath, string(after))
	r, e := Undo(o)
	if e != nil || r.Changed {
		t.Fatalf("retry %+v %v", r, e)
	}
	if string(get(t, o.ConfigPath)) != string(after) {
		t.Fatal("retry changed unowned entries")
	}
}

func TestUndoDoesNotRequireRemovedLauncher(t *testing.T) {
	o := options(t, "codex")
	if _, e := Apply(o); e != nil {
		t.Fatal(e)
	}
	o.Command = ""
	o.Args = nil
	if r, e := Undo(o); e != nil || !r.Changed {
		t.Fatalf("undo removed launcher %+v %v", r, e)
	}
}
func TestBackupTamperProfileDriftAndReceiptCorruptionRefused(t *testing.T) {
	for _, variant := range []string{"backup", "profile", "receipt"} {
		t.Run(variant, func(t *testing.T) {
			o := options(t, "codex")
			put(t, o.ConfigPath, "theme=\"dark\"\n")
			r, e := Apply(o)
			if e != nil {
				t.Fatal(e)
			}
			before := get(t, o.ConfigPath)
			switch variant {
			case "backup":
				put(t, r.BackupPath, "changed")
			case "profile":
				o.ConfigPath = filepath.Join(filepath.Dir(o.ConfigPath), "another")
			case "receipt":
				put(t, filepath.Join(o.StateDir, "ownership.json"), "invalid")
			}
			if _, e = Undo(o); e == nil {
				t.Fatal("unsafe undo accepted")
			}
			if variant != "profile" && string(get(t, o.ConfigPath)) != string(before) {
				t.Fatal("failed undo mutated config")
			}
		})
	}
}
func TestAtomicReplacementRejectsConcurrentChangeAndExistingCreation(t *testing.T) {
	o := options(t, "codex")
	put(t, o.ConfigPath, "new owner edit")
	if e := replaceChecked(o.ConfigPath, []byte("prior"), true, []byte("overwrite")); e == nil {
		t.Fatal("lost concurrent edit")
	}
	if e := replaceChecked(o.ConfigPath, nil, false, []byte("overwrite")); e == nil {
		t.Fatal("replaced newly created file")
	}
	if string(get(t, o.ConfigPath)) != "new owner edit" {
		t.Fatal("config overwritten")
	}
}
func TestHeldLockRefusesWithoutMutation(t *testing.T) {
	o := options(t, "codex")
	if e := privateDir(o.StateDir); e != nil {
		t.Fatal(e)
	}
	root, e := openDirectory(o.StateDir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	release, e := lock(root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e = Apply(o); e == nil || !strings.Contains(e.Error(), "lock") {
		t.Fatalf("concurrent apply error=%v", e)
	}
	if _, e = os.Stat(o.ConfigPath); !os.IsNotExist(e) {
		t.Fatal("lock refusal wrote profile")
	}
}
func TestUnsafeModesOversizeAndArgumentsRefused(t *testing.T) {
	o := options(t, "codex")
	put(t, o.ConfigPath, "theme=\"dark\"")
	os.Chmod(o.ConfigPath, 0666)
	if _, e := Apply(o); e == nil {
		t.Fatal("world writable file accepted")
	}
	os.Chmod(o.ConfigPath, 0600)
	os.Chmod(filepath.Dir(o.ConfigPath), 0777)
	if _, e := Apply(o); e == nil {
		t.Fatal("world writable profile accepted")
	}
	o = options(t, "codex")
	for _, args := range [][]string{nil, {"https://bridge.invalid/mcp"}, {"mcp\nmalicious"}, {strings.Repeat("x", 4097)}} {
		bad := o
		bad.Args = args
		if _, e := Preview(bad); e == nil {
			t.Fatalf("bad args accepted %+v", args)
		}
	}
	put(t, o.ConfigPath, strings.Repeat("x", (8<<20)+1))
	if _, e := Preview(o); e == nil {
		t.Fatal("oversize input accepted")
	}
}

func TestNonRegularAndExposedOwnershipRefused(t *testing.T) {
	for _, variant := range []string{"fifo-config", "fifo-lock", "exposed-state", "exposed-lock"} {
		t.Run(variant, func(t *testing.T) {
			o := options(t, "codex")
			os.MkdirAll(filepath.Dir(o.ConfigPath), 0700)
			os.MkdirAll(o.StateDir, 0700)
			switch variant {
			case "fifo-config":
				if e := unix.Mkfifo(o.ConfigPath, 0600); e != nil {
					t.Fatal(e)
				}
			case "fifo-lock":
				if e := unix.Mkfifo(filepath.Join(o.StateDir, "setup.lock"), 0600); e != nil {
					t.Fatal(e)
				}
			case "exposed-state":
				os.Chmod(o.StateDir, 0755)
			case "exposed-lock":
				put(t, filepath.Join(o.StateDir, "setup.lock"), "")
				os.Chmod(filepath.Join(o.StateDir, "setup.lock"), 0644)
			}
			if _, e := Apply(o); e == nil {
				t.Fatal("unsafe special/exposed state accepted")
			}
		})
	}
}

func TestUnsafeUTF8AndControlsCannotMutateCodexProfile(t *testing.T) {
	for _, value := range []string{"mcp\x7f", "mcp\t", "mcp\x01", "mcp\u0085", "mcp\u202e", string([]byte{'m', 'c', 'p', 0xff})} {
		t.Run(value, func(t *testing.T) {
			o := options(t, "codex")
			original := "model=\"synthetic\"\n"
			put(t, o.ConfigPath, original)
			o.Args = []string{value}
			if _, e := Apply(o); e == nil {
				t.Fatal("unsafe argument accepted")
			}
			if string(get(t, o.ConfigPath)) != original {
				t.Fatal("unsafe argument mutated profile")
			}
			if _, e := os.Stat(o.StateDir); !os.IsNotExist(e) {
				t.Fatal("unsafe argument created ownership state")
			}
		})
	}
}

func TestUnsafeExecutableFilenameRejectedBeforeMutation(t *testing.T) {
	for _, suffix := range []string{"\x7f", "\t", "\u0085", "\u202e", string([]byte{0xff})} {
		t.Run(suffix, func(t *testing.T) {
			o := options(t, "codex")
			original := "model=\"synthetic\"\n"
			put(t, o.ConfigPath, original)
			o.Command = filepath.Join(filepath.Dir(o.StateDir), "launcher"+suffix)
			if utf8.ValidString(suffix) {
				if e := os.WriteFile(o.Command, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := Apply(o); e == nil || (!utf8.ValidString(suffix) && !strings.Contains(e.Error(), "clean absolute")) {
				t.Fatal("unsafe executable filename accepted")
			}
			if string(get(t, o.ConfigPath)) != original {
				t.Fatal("unsafe executable filename mutated profile")
			}
		})
	}
}

func TestSafeTextRejectsEveryASCIIAndC1Control(t *testing.T) {
	for r := rune(0); r <= 0x9f; r++ {
		if r >= 0x20 && r < 0x7f {
			continue
		}
		if safeText("argument" + string(r)) {
			t.Fatalf("accepted control U+%04X", r)
		}
	}
	for _, text := range []string{"mcp", "normal path/with spaces", "café", "quoted\"and\\backslash"} {
		if !safeText(text) {
			t.Fatalf("rejected ordinary text %q", text)
		}
	}
}
