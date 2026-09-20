package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionsFindsCurrentAndArchivedJSONL(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"sessions/2026/a.jsonl", "archived_sessions/b.jsonl", "sessions/auth.json"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := Sessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || !sources[0].Archived && !sources[1].Archived {
		t.Fatalf("sources=%+v", sources)
	}
}

func TestHomeHonorsEnvironmentAndMissingDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	home, err := Home()
	if err != nil || home != root {
		t.Fatalf("home=%s err=%v", home, err)
	}
	sources, err := Sessions(filepath.Join(root, "missing"))
	if err != nil || len(sources) != 0 {
		t.Fatalf("sources=%v err=%v", sources, err)
	}
}
