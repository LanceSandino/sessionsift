package codexconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEnableInstallsHooksAndOTelIdempotently(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "sessionsift")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := Enable(home, binary)
	if err != nil || !result.HooksChanged || !result.OTelChanged {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	configBody, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(configBody)
	if !strings.Contains(config, "/v1/logs") || !strings.Contains(config, "/v1/metrics") {
		t.Fatalf("config=%s", config)
	}
	hooksBody, err := os.ReadFile(filepath.Join(home, "hooks.json"))
	if err != nil || !json.Valid(hooksBody) || !strings.Contains(string(hooksBody), binary) {
		t.Fatalf("hooks=%s err=%v", hooksBody, err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "PreCompact", "PostCompact", "Stop"} {
		assertSessionSiftHookPolicy(t, hooksBody, event, binary, 10, true, true)
	}
	assertSessionSiftHookPolicy(t, hooksBody, "SessionEnd", binary, 3, false, false)
	assertSessionSiftHookPolicy(t, hooksBody, "Interrupt", binary, 3, true, true)
	second, err := Enable(home, binary)
	if err != nil || second.HooksChanged || second.OTelChanged {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestEnableRepairsSessionEndAndInterruptWithoutChangingConPACTStop(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "sessionsift")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := shellQuote(binary) + " hook"
	conPACT := map[string]any{"hooks": []any{map[string]any{
		"type": "command", "command": "/opt/conPACT/python -m conpact.codex_cli_hook", "timeout": 600, "statusMessage": "Running queued conPACT compaction",
	}}}
	root := map[string]any{"hooks": map[string]any{
		"SessionEnd": []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": command, "async": true, "timeout": 10}}}},
		"Interrupt":  []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "async": true, "timeout": 10}}}},
		"Stop":       []any{conPACT, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "async": true, "timeout": 10}}}},
	}}
	body, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Enable(home, binary)
	if err != nil || !result.HooksChanged {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	updated, err := os.ReadFile(filepath.Join(home, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertSessionSiftHookPolicy(t, updated, "SessionEnd", binary, 3, false, false)
	assertSessionSiftHookPolicy(t, updated, "Interrupt", binary, 3, true, true)
	assertSessionSiftHookPolicy(t, updated, "Stop", binary, 10, true, true)
	var decoded map[string]any
	if err := json.Unmarshal(updated, &decoded); err != nil {
		t.Fatal(err)
	}
	stopEntries := decoded["hooks"].(map[string]any)["Stop"].([]any)
	if !reflect.DeepEqual(stopEntries[0], normalizeJSON(t, conPACT)) {
		t.Fatalf("conPACT Stop hook changed: got=%v want=%v", stopEntries[0], conPACT)
	}
	second, err := Enable(home, binary)
	if err != nil || second.HooksChanged {
		t.Fatalf("repair was not idempotent: result=%+v err=%v", second, err)
	}
}

func assertSessionSiftHookPolicy(t *testing.T, body []byte, event, binary string, timeout float64, wantAsync bool, wantAsyncField bool) {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	entries := root["hooks"].(map[string]any)[event].([]any)
	command := shellQuote(binary) + " hook"
	for _, rawEntry := range entries {
		entry := rawEntry.(map[string]any)
		for _, rawHandler := range entry["hooks"].([]any) {
			handler := rawHandler.(map[string]any)
			if handler["command"] != command {
				continue
			}
			if handler["timeout"] != timeout {
				t.Fatalf("%s timeout=%v want=%v", event, handler["timeout"], timeout)
			}
			async, exists := handler["async"]
			if exists != wantAsyncField || (exists && async != wantAsync) {
				t.Fatalf("%s async=%v exists=%v want=%v exists=%v", event, async, exists, wantAsync, wantAsyncField)
			}
			return
		}
	}
	t.Fatalf("missing SessionSift %s hook", event)
}

func normalizeJSON(t *testing.T, value any) any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var normalized any
	if err := json.Unmarshal(body, &normalized); err != nil {
		t.Fatal(err)
	}
	return normalized
}

func TestEnablePreservesExistingConfiguration(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "sessionsift")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other-tool"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(home, binary); err != nil {
		t.Fatal(err)
	}
	configBody, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	hooksBody, _ := os.ReadFile(filepath.Join(home, "hooks.json"))
	if !strings.Contains(string(configBody), "model = \"gpt\"") || !strings.Contains(string(hooksBody), "other-tool") {
		t.Fatalf("config=%s hooks=%s", configBody, hooksBody)
	}
}

func TestEnableRejectsConflictingOTelAndSymlink(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(t.TempDir(), "sessionsift")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[otel]\nexporter = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(home, binary); err == nil {
		t.Fatal("expected conflicting OTel error")
	}
	if err := os.Remove(filepath.Join(home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "real-config")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(home, binary); err == nil {
		t.Fatal("expected symlink rejection")
	}
}
