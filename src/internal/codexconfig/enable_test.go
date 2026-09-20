package codexconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	second, err := Enable(home, binary)
	if err != nil || second.HooksChanged || second.OTelChanged {
		t.Fatalf("second=%+v err=%v", second, err)
	}
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
