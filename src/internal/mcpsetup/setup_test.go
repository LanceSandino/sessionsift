// @module MCPSetupTests exercises explicit ownership, preservation and fail-closed setup.
package mcpsetup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func options(t *testing.T, client string) Options {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Options{Client: client, ConfigPath: filepath.Join(root, "profile", "config"), StateDir: filepath.Join(root, "owned"), Command: "/usr/bin/true", Args: []string{"mcp"}}
}
func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func get(t *testing.T, path string) []byte {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestPreviewIsReadOnlyAndDoesNotRevealExistingConfig(t *testing.T) {
	o := options(t, "codex")
	put(t, o.ConfigPath, "secret = \"DO_NOT_PRINT\"\n")
	p, e := Preview(o)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(p.Snippet, "DO_NOT_PRINT") || !strings.Contains(p.Snippet, "[mcp_servers.sessionsift]") {
		t.Fatalf("preview=%+v", p)
	}
	if _, e = os.Stat(o.StateDir); !os.IsNotExist(e) {
		t.Fatal("preview wrote state")
	}
}

func TestPreviewNewProfileCreatesNoFilesOrDirectories(t *testing.T) {
	for _, client := range []string{"codex", "claude-code"} {
		t.Run(client, func(t *testing.T) {
			o := options(t, client)
			p, e := Preview(o)
			if e != nil || p.Snippet == "" {
				t.Fatalf("preview %+v %v", p, e)
			}
			for _, path := range []string{o.ConfigPath, filepath.Dir(o.ConfigPath), o.StateDir} {
				if _, e = os.Stat(path); !os.IsNotExist(e) {
					t.Fatalf("preview created %s", path)
				}
			}
		})
	}
}
func TestApplyIdempotentAndUndoPreservesUserAdditions(t *testing.T) {
	for _, client := range []string{"codex", "claude-code"} {
		t.Run(client, func(t *testing.T) {
			o := options(t, client)
			original := "theme = \"dark\"\n"
			if client == "claude-code" {
				original = `{"other":{"token":"keep"},"mcpServers":{"unrelated":{"command":"leave"}}}`
			}
			put(t, o.ConfigPath, original)
			r, e := Apply(o)
			if e != nil || !r.Changed {
				t.Fatalf("apply=%+v %v", r, e)
			}
			if !bytes.Equal(get(t, r.BackupPath), []byte(original)) {
				t.Fatal("backup changed")
			}
			if info, _ := os.Stat(r.BackupPath); info.Mode().Perm() != 0600 {
				t.Fatal("backup not private")
			}
			after := get(t, o.ConfigPath)
			r, e = Apply(o)
			if e != nil || r.Changed || !bytes.Equal(after, get(t, o.ConfigPath)) {
				t.Fatalf("idempotence %v %v", r, e)
			}
			if client == "codex" {
				put(t, o.ConfigPath, string(after)+"\n[unrelated]\nadded = true\n")
			} else {
				put(t, o.ConfigPath, strings.Replace(string(after), `"other":`, `"added":true,"other":`, 1))
			}
			r, e = Undo(o)
			if e != nil || !r.Changed {
				t.Fatalf("undo %v %v", r, e)
			}
			got := string(get(t, o.ConfigPath))
			if strings.Contains(got, "sessionsift") || !strings.Contains(got, "added") {
				t.Fatalf("undo lost user changes: %s", got)
			}
			r, e = Undo(o)
			if e != nil || r.Changed {
				t.Fatalf("undo idempotence %v %v", r, e)
			}
		})
	}
}
func TestConflictsMalformedAndDuplicatesNeverWrite(t *testing.T) {
	cases := []struct{ client, input string }{{"codex", "[mcp_servers.sessionsift]\ncommand=\"owned-by-user\"\n"}, {"codex", "[mcp_servers . \"sessionsift\"]\n"}, {"codex", "mcp_servers = {}\n"}, {"codex", "x = '''multiline'''\n"}, {"claude-code", `{"mcpServers":{"sessionsift":{"url":"https://invalid.example"}}}`}, {"claude-code", `{"mcpServers":{},"mcpServers":{}}`}, {"claude-code", `{"nested":{"duplicate":1,"duplicate":2}}`}, {"claude-code", `[]`}, {"claude-code", `{"mcpServers":true}`}, {"claude-code", `{} {}`}}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			o := options(t, c.client)
			put(t, o.ConfigPath, c.input)
			if _, e := Apply(o); e == nil {
				t.Fatal("accepted conflict")
			}
			if string(get(t, o.ConfigPath)) != c.input {
				t.Fatal("changed rejected config")
			}
		})
	}
}
func TestModifiedOwnedEntryRefusesApplyAndUndo(t *testing.T) {
	for _, client := range []string{"codex", "claude-code"} {
		t.Run(client, func(t *testing.T) {
			o := options(t, client)
			if _, e := Apply(o); e != nil {
				t.Fatal(e)
			}
			altered := strings.Replace(string(get(t, o.ConfigPath)), "/usr/bin/true", "/usr/bin/false", 1)
			put(t, o.ConfigPath, altered)
			if _, e := Undo(o); e == nil {
				t.Fatal("undo clobbered changed entry")
			}
			if _, e := Apply(o); e == nil {
				t.Fatal("apply clobbered changed entry")
			}
			if string(get(t, o.ConfigPath)) != altered {
				t.Fatal("modified config changed")
			}
		})
	}
}
func TestSymlinksAndUnsafeCommandsRefused(t *testing.T) {
	for _, kind := range []string{"file", "ancestor", "state"} {
		t.Run(kind, func(t *testing.T) {
			o := options(t, "codex")
			outside := filepath.Join(filepath.Dir(o.StateDir), "outside")
			put(t, outside, "sentinel")
			switch kind {
			case "file":
				os.MkdirAll(filepath.Dir(o.ConfigPath), 0700)
				os.Symlink(outside, o.ConfigPath)
			case "ancestor":
				os.Symlink(filepath.Dir(outside), filepath.Dir(o.ConfigPath))
			case "state":
				os.Symlink(filepath.Dir(outside), o.StateDir)
			}
			if _, e := Apply(o); e == nil {
				t.Fatal("accepted symlink")
			}
			if string(get(t, outside)) != "sentinel" {
				t.Fatal("outside changed")
			}
		})
	}
	for _, cmd := range []string{"sessionsift", "https://remote.invalid/mcp", "/bin/sh", "/bin/bash", "/usr/bin/env", "/missing/executable"} {
		o := options(t, "codex")
		o.Command = cmd
		if _, e := Preview(o); e == nil {
			t.Fatalf("accepted command %q", cmd)
		}
	}
}
