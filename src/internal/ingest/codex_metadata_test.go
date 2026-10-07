// @module CodexMetadataTests pins content-preserving metadata reconciliation.
package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestImportAllSkipsThreadHeartbeatAndRefreshesMetadataWithoutParsing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	id := "01a0bc96-f391-7802-be76-8adfbe64df54"
	path := filepath.Join(root, "sessions", "rollout-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":%q,"cwd":"/private/hidden/repo"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"turn"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Original evidence"}]}}`, id)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, name TEXT, source TEXT, thread_source TEXT, originator TEXT, sandbox_policy TEXT, approval_mode TEXT, git_branch TEXT, git_origin_url TEXT, cli_version TEXT, model TEXT, reasoning_effort TEXT, created_at_ms INTEGER, updated_at_ms INTEGER)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO threads(id,name,cli_version,updated_at_ms) VALUES(?,?,?,?)`, id, "First name", "v1", 1)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if r, err := ImportAll(ctx, store, root); err != nil || r.Imported != 1 {
		t.Fatalf("initial=%+v err=%v", r, err)
	}
	_, err = db.Exec(`UPDATE threads SET updated_at_ms=2`)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := ImportAll(ctx, store, root); err != nil || r.Imported != 0 || r.Skipped != 1 {
		t.Fatalf("heartbeat caused work: %+v err=%v", r, err)
	}

	// The size/mtime fingerprint is authoritative for transcript changes.
	// Make rereading detectable while holding that fingerprint constant.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	headerEnd := strings.IndexByte(body, '\n') + 1
	if err := os.WriteFile(path, []byte(body[:headerEnd]+strings.Repeat("x", len(body)-headerEnd)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE threads SET name='Renamed /private/hidden/repo',cli_version='v2',updated_at_ms=3`)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := ImportAll(ctx, store, root); err != nil || r.Imported != 1 || r.Failed != 0 {
		t.Fatalf("metadata=%+v err=%v", r, err)
	}
	session, err := store.GetSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if session.Title != "Renamed repo" || session.SourceVersion != "v2" || len(session.Turns) != 1 || session.Turns[0].UserPromptRedacted != "Original evidence" {
		t.Fatalf("metadata refresh replaced evidence: title=%q version=%q turns=%d", session.Title, session.SourceVersion, len(session.Turns))
	}
	if r, err := ImportAll(ctx, store, root); err != nil || r.Skipped != 1 {
		t.Fatalf("settled metadata=%+v err=%v", r, err)
	}
	if _, err := db.Exec(`UPDATE threads SET name=''`); err != nil {
		t.Fatal(err)
	}
	if r, err := ImportAll(ctx, store, root); err != nil || r.Imported != 1 || r.Failed != 0 {
		t.Fatalf("cleared rename=%+v err=%v", r, err)
	}
	session, err = store.GetSession(ctx, id)
	if err != nil || session.Title != "Original evidence" || session.TitleSource != "first_prompt" {
		t.Fatalf("cleared rename retained stale title: title=%q source=%q err=%v", session.Title, session.TitleSource, err)
	}
	if _, err := db.Exec(`DROP TABLE threads`); err != nil {
		t.Fatal(err)
	}
	if r, err := ImportAll(ctx, store, root); err != nil || r.Skipped != 1 || r.Imported != 0 {
		t.Fatalf("unavailable state caused reparse: %+v err=%v", r, err)
	}
	session, err = store.GetSession(ctx, id)
	if err != nil || session.Title != "Original evidence" {
		t.Fatalf("state failure discarded enrichment: title=%q err=%v", session.Title, err)
	}
}
