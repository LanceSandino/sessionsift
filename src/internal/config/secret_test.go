package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateSecretIsStableAndRejectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "path-hash.key")
	first, err := LoadOrCreateSecret(path)
	if err != nil || len(first) != 32 {
		t.Fatalf("first=%x err=%v", first, err)
	}
	again, err := LoadOrCreateSecret(path)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatalf("secret changed between loads: %x vs %x err=%v", first, again, err)
	}
	other, err := LoadOrCreateSecret(filepath.Join(t.TempDir(), "other.key"))
	if err != nil || bytes.Equal(first, other) {
		t.Fatal("installations share a secret")
	}
	for _, body := range []string{"not-hex", "abcd", ""} {
		if err = os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err = LoadOrCreateSecret(path); err == nil {
			t.Fatalf("malformed secret %q accepted", body)
		}
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadOrCreateSecret(filepath.Join(blocker, "key")); err == nil {
		t.Fatal("secret created beneath a file")
	}
}

func TestResolvePathsIncludeClaudeExportAndSecret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", home)
	paths, err := Resolve()
	if err != nil || paths.ClaudeExport != filepath.Join(home, "claude-export.path") || paths.PathKey != filepath.Join(home, "path-hash.key") {
		t.Fatalf("paths=%+v err=%v", paths, err)
	}
}
