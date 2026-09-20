package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAndEnsurePrivateState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "state")
	t.Setenv("SESSIONSIFT_HOME", home)
	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if paths.Database != filepath.Join(home, "sessionsift.db") {
		t.Fatalf("database=%s", paths.Database)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.RawEvents)
	if err != nil || !info.IsDir() {
		t.Fatalf("raw dir error=%v", err)
	}
}

func TestResolveDefaultHome(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", "")
	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(paths.Home, ".sessionsift") {
		t.Fatalf("home=%s", paths.Home)
	}
}
