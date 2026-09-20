package ingest

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	_ "modernc.org/sqlite"
)

func TestImportChatGPTJSONAndZip(t *testing.T) {
	jsonBody := `[{"id":"chat-one","title":"Chat one","create_time":1,"update_time":2,"current_node":"user","mapping":{"user":{"id":"user","message":{"id":"message","author":{"role":"user"},"create_time":1,"content":{"parts":["Hello"]}}}}}]`
	jsonPath := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(jsonPath, []byte(jsonBody), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "chatgpt.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result, err := ImportChatGPT(context.Background(), store, jsonPath)
	if err != nil || result.Discovered != 1 || result.Imported != 1 {
		t.Fatalf("json result=%+v err=%v", result, err)
	}
	result, err = ImportChatGPT(context.Background(), store, jsonPath)
	if err != nil || result.Skipped != 1 {
		t.Fatalf("incremental result=%+v err=%v", result, err)
	}

	zipPath := filepath.Join(t.TempDir(), "chatgpt-export.zip")
	archive, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, _ := writer.Create("conversations-2.json")
	_, _ = entry.Write([]byte(strings.ReplaceAll(jsonBody, "chat-one", "chat-two")))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	result, err = ImportChatGPT(context.Background(), store, zipPath)
	if err != nil || result.Imported != 1 {
		t.Fatalf("zip result=%+v err=%v", result, err)
	}
	sessions, err := store.ListSessions(context.Background())
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions=%d err=%v", len(sessions), err)
	}
	replacement := strings.ReplaceAll(jsonBody, "chat-one", "chat-new")
	if err := os.WriteFile(jsonPath, []byte(replacement), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(jsonPath, future, future); err != nil {
		t.Fatal(err)
	}
	result, err = ImportChatGPT(context.Background(), store, jsonPath)
	if err != nil || result.Imported != 1 {
		t.Fatalf("replacement result=%+v err=%v", result, err)
	}
	sessions, err = store.ListSessions(context.Background())
	if err != nil || len(sessions) != 2 {
		t.Fatalf("reconciled sessions=%d err=%v", len(sessions), err)
	}
	for _, session := range sessions {
		if session.ExternalSessionID == "chat-one" {
			t.Fatal("removed conversation remained after source replacement")
		}
	}
}

func TestImportChatGPTReportsInvalidDocumentsAndArchives(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "chatgpt-errors.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := ImportChatGPT(context.Background(), store, filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing export error")
	}
	badZip := filepath.Join(t.TempDir(), "broken.zip")
	if err := os.WriteFile(badZip, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportChatGPT(context.Background(), store, badZip); err == nil {
		t.Fatal("expected invalid ZIP error")
	}
	badJSON := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(badJSON, []byte(`{"bad":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ImportChatGPT(context.Background(), store, badJSON)
	if err != nil || result.Failed != 1 || len(result.Warnings) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	zipPath := filepath.Join(t.TempDir(), "empty-export.zip")
	archive, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, _ := writer.Create("notes.txt")
	_, _ = entry.Write([]byte("no conversations"))
	_ = writer.Close()
	_ = archive.Close()
	if _, err := ImportChatGPT(context.Background(), store, zipPath); err == nil {
		t.Fatal("expected missing conversations archive error")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	valid := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(valid, []byte(`[{"id":"chat"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportChatGPT(cancelled, store, valid); err == nil {
		t.Fatal("expected cancellation")
	}
}

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

func TestImportAllRefreshesRenamedCodexTitleWithoutTranscriptChange(t *testing.T) {
	root := t.TempDir()
	id := "01a0bc96-f391-7802-be76-8adfbe64df54"
	transcriptPath := filepath.Join(root, "sessions", "2026", "rollout-2026-01-01T00-00-00-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":%q,"cwd":"/tmp/repo"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"turn"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"First prompt title"}]}}`, id)
	if err := os.WriteFile(transcriptPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDB, err := sql.Open("sqlite", filepath.Join(root, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = stateDB.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, name TEXT, title TEXT, source TEXT, thread_source TEXT, originator TEXT, sandbox_policy TEXT, approval_mode TEXT, git_branch TEXT, git_origin_url TEXT, cli_version TEXT, model TEXT, reasoning_effort TEXT, created_at_ms INTEGER, updated_at_ms INTEGER)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stateDB.Exec(`INSERT INTO threads VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "Sessionsift MVP", "First prompt title", "vscode", "user", "Codex Desktop", `{"type":"managed","network":"restricted"}`, "on-request", "main", "origin", "1", "gpt", "high", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := stateDB.Close(); err != nil {
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
	session, _ := store.GetSession(context.Background(), id)
	if session.Title != "Sessionsift MVP" || session.TitleSource != "renamed" || session.Originator != "Codex Desktop" {
		t.Fatalf("session=%+v", session)
	}
	stateDB, _ = sql.Open("sqlite", filepath.Join(root, "state_5.sqlite"))
	_, err = stateDB.Exec(`UPDATE threads SET name='SessionSift dashboard', updated_at_ms=3 WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	_ = stateDB.Close()
	second, err := ImportAll(context.Background(), store, root)
	if err != nil || second.Imported != 1 || second.Skipped != 0 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	session, _ = store.GetSession(context.Background(), id)
	if session.Title != "SessionSift dashboard" {
		t.Fatalf("renamed title did not refresh: %+v", session)
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
