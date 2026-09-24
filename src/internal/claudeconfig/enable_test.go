package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookCommands(t *testing.T, body []byte) map[string][]string {
	t.Helper()
	var root struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("invalid JSON %s: %v", body, err)
	}
	values := map[string][]string{}
	for event, entries := range root.Hooks {
		for _, entry := range entries {
			for _, handler := range entry.Hooks {
				values[event] = append(values[event], handler.Command)
			}
		}
	}
	return values
}

func TestEnablePreservesUnrelatedSettingsByteForByte(t *testing.T) {
	home := t.TempDir()
	original := "{\n    \"model\": \"opus\",\n    \"permissions\": {\"allow\": [\"Bash(git status)\"]},\n    \"hooks\": {\n      \"Stop\": [{\"hooks\": [{\"type\": \"command\", \"command\": \"say done\"}]}]\n    },\n    \"env\": {\"A\": \"1\"}\n}\n"
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(home, "bin", "sessionsift")
	result, err := Enable(home, binary)
	if err != nil || !result.HooksChanged {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	updated, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	text := string(updated)
	for _, preserved := range []string{"{\n    \"model\": \"opus\",\n    \"permissions\": {\"allow\": [\"Bash(git status)\"]},\n    \"hooks\": ", ",\n    \"env\": {\"A\": \"1\"}\n}\n"} {
		if !strings.Contains(text, preserved) {
			t.Fatalf("unrelated bytes changed; missing %q in %s", preserved, text)
		}
	}
	commands := hookCommands(t, updated)
	if len(commands["Stop"]) != 2 || commands["Stop"][0] != "say done" {
		t.Fatalf("existing Stop hook changed: %+v", commands["Stop"])
	}
	for _, event := range Events {
		if len(commands[event]) == 0 || commands[event][len(commands[event])-1] != Command(binary) {
			t.Fatalf("event %s missing SessionSift hook: %+v", event, commands[event])
		}
	}
	again, err := Enable(home, binary)
	if err != nil || again.HooksChanged {
		t.Fatalf("second enable=%+v err=%v", again, err)
	}
	stable, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	if string(stable) != text {
		t.Fatal("idempotent enable rewrote settings")
	}
}

func TestEnableCreatesOrInsertsHooks(t *testing.T) {
	home := filepath.Join(t.TempDir(), "claude")
	if _, err := Enable(home, filepath.Join(t.TempDir(), "sessionsift")); err != nil {
		t.Fatal(err)
	}
	created, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	if len(hookCommands(t, created)) != len(Events) {
		t.Fatalf("created=%s", created)
	}
	withKeys := []byte(`{"model":"sonnet"}`)
	merged, changed, err := Merge(withKeys, "sessionsift hook claude")
	if err != nil || !changed || !strings.HasPrefix(string(merged), `{"model":"sonnet",`) {
		t.Fatalf("merged=%s changed=%v err=%v", merged, changed, err)
	}
	empty, changed, err := Merge([]byte("{}"), "sessionsift hook claude")
	if err != nil || !changed || strings.HasPrefix(string(empty), "{,") {
		t.Fatalf("empty merge=%s err=%v", empty, err)
	}
	nullHooks, changed, err := Merge([]byte(`{"hooks": null}`), "x hook claude")
	if err != nil || !changed || len(hookCommands(t, nullHooks)) != len(Events) {
		t.Fatalf("null hooks=%s err=%v", nullHooks, err)
	}
}

func TestEnableRefusesUnsafeOrInvalidSettings(t *testing.T) {
	home := t.TempDir()
	if _, err := Enable(home, "relative/sessionsift"); err == nil {
		t.Fatal("relative binary accepted")
	}
	for _, body := range []string{`{not json`, `[]`, `{"hooks":[]}`, `{"hooks":{"Stop":{}}}`} {
		if _, _, err := Merge([]byte(body), "x"); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(home, filepath.Join(home, "sessionsift")); err == nil {
		t.Fatal("invalid settings were overwritten")
	}
	if body, _ := os.ReadFile(path); string(body) != `{broken` {
		t.Fatal("invalid settings changed")
	}
	target := filepath.Join(home, "real.json")
	_ = os.WriteFile(target, []byte(`{}`), 0o600)
	linkHome := filepath.Join(home, "linked")
	_ = os.MkdirAll(linkHome, 0o700)
	if err := os.Symlink(target, filepath.Join(linkHome, "settings.json")); err == nil {
		if _, err = Enable(linkHome, filepath.Join(home, "sessionsift")); err == nil {
			t.Fatal("symlinked settings were replaced")
		}
	}
}

func TestCommandQuotesOnlyWhenNeeded(t *testing.T) {
	if got := Command(`C:\Tools\sessionsift.exe`); got != `C:\Tools\sessionsift.exe hook claude` {
		t.Fatalf("plain=%q", got)
	}
	if got := Command(`/Applications/Session Sift/sessionsift`); got != `"/Applications/Session Sift/sessionsift" hook claude` {
		t.Fatalf("spaced=%q", got)
	}
}
