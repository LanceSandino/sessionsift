package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestImportAllThousandSessionScale(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "sessions", "2026")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 1_000; index++ {
		data := fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"scale-%04d","cwd":"/tmp/repo"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"turn-%04d","model":"gpt-test"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Inspect session %04d"}]}}`, index, index, index)
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("%04d.jsonl", index)), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "scale.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	started := time.Now()
	result, err := ImportAll(context.Background(), store, root)
	if err != nil || result.Imported != 1_000 || result.Failed != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("1,000-session import took %s", elapsed)
	}

	started = time.Now()
	sessions, err := store.ListSessions(context.Background())
	if err != nil || len(sessions) != 1_000 {
		t.Fatalf("sessions=%d err=%v", len(sessions), err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("1,000-session list took %s", elapsed)
	}

	result, err = ImportAll(context.Background(), store, root)
	if err != nil || result.Skipped != 1_000 {
		t.Fatalf("incremental result=%+v err=%v", result, err)
	}
}

func TestSourceFingerprintIncludesParserVersion(t *testing.T) {
	got := sourceFingerprint(12, time.Unix(100, 20))
	if !strings.HasPrefix(got, domain.ParserVersion+":") {
		t.Fatalf("fingerprint %q does not include parser version", got)
	}
}

func TestImportAllIsIncremental(t *testing.T) {
	root := t.TempDir()
	transcript := filepath.Join(root, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"session_id":"s1","cwd":"/tmp/repo"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"t1","model":"gpt"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Inspect auth"}]}}`
	if err := os.WriteFile(transcript, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := ImportAll(context.Background(), store, root)
	if err != nil || first.Imported != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := ImportAll(context.Background(), store, root)
	if err != nil || second.Skipped != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	sessions, err := store.ListSessions(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].Title != "Inspect auth" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
}

func TestImportAllCancellationAndClosedStore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ImportAll(ctx, store, root); err == nil {
		t.Fatal("expected cancellation")
	}
	_ = store.Close()
	if _, err := ImportAll(context.Background(), store, root); err == nil {
		t.Fatal("expected closed store error")
	}
}

func TestImportSpoolRedactsAndRemovesImportedFiles(t *testing.T) {
	directory := t.TempDir()
	spoolFile := filepath.Join(directory, "hook.json")
	payload := `{"session_id":"live-session","turn_id":"turn-1","hook_event_name":"AfterTool","prompt":"token sk-proj-` + `abcdefghijklmnopqrstuvwxyz","cwd":"/private/repo"}`
	if err := os.WriteFile(spoolFile, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "invalid.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "missing-session.json"), []byte(`{"prompt":"ignored"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	imported, err := ImportSpool(context.Background(), store, directory)
	if err != nil || imported != 1 {
		t.Fatalf("imported=%d err=%v", imported, err)
	}
	if _, err := os.Stat(spoolFile); !os.IsNotExist(err) {
		t.Fatalf("expected imported spool file removal, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "invalid.json")); err != nil {
		t.Fatalf("invalid file should remain for diagnosis: %v", err)
	}
	session, err := store.GetSession(context.Background(), "live-session")
	if err != nil || session.Title != "Live Codex session" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
}

func TestImportSpoolReportsStorageFailure(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "hook.json"), []byte(`{"session_id":"live-session"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSpool(context.Background(), store, directory); err == nil {
		t.Fatal("expected closed store error")
	}
}

func TestImportSpoolMissingDirectoryIsEmpty(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	imported, err := ImportSpool(context.Background(), store, filepath.Join(t.TempDir(), "missing"))
	if err != nil || imported != 0 {
		t.Fatalf("imported=%d err=%v", imported, err)
	}
}
