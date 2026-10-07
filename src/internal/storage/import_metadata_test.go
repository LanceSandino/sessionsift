// @module ImportMetadataTests pins atomic, content-preserving enrichment updates.
package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestImportMetadataPreservesEvidenceAndRollsBackMissingManifest(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session := domain.Session{ID: "session", ExternalSessionID: "session", Title: "Before", Turns: []domain.Turn{{ID: "turn", SessionID: "session", UserPromptRedacted: "Original"}}}
	if err := store.SaveImport(ctx, "path", "one", session); err != nil {
		t.Fatal(err)
	}
	// A separately captured child is deliberately absent from the payload.
	if _, err := store.db.Exec(`INSERT INTO events(id,session_id,event_type,source,payload_json) VALUES('extra','session','hook','hook','{}')`); err != nil {
		t.Fatal(err)
	}
	records, err := store.ImportRecords(ctx)
	if err != nil || len(records) != 1 || records["path"].SessionID != "session" || records["path"].Fingerprint != "one" {
		t.Fatalf("records=%v err=%v", records, err)
	}
	session.Title = "After"
	if err := store.SaveImportMetadata(ctx, "path", "two", session); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, "session")
	if err != nil || got.Title != "After" || len(got.Turns) != 1 || got.Turns[0].UserPromptRedacted != "Original" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM events WHERE id='extra'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("child records replaced: %d err=%v", count, err)
	}
	records, err = store.ImportRecords(ctx)
	if err != nil || records["path"].Fingerprint != "two" {
		t.Fatalf("records=%v err=%v", records, err)
	}
	session.Title = "Must roll back"
	if err := store.SaveImportMetadata(ctx, "missing", "three", session); err == nil {
		t.Fatal("missing manifest accepted")
	}
	got, err = store.GetSession(ctx, "session")
	if err != nil || got.Title != "After" {
		t.Fatalf("failed transaction changed title: %q err=%v", got.Title, err)
	}
	session.ID = "missing"
	if err := store.SaveImportMetadata(ctx, "path", "three", session); err == nil {
		t.Fatal("missing session accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ImportRecords(cancelled); err == nil {
		t.Fatal("cancelled manifest read succeeded")
	}
	if err := store.SaveImportMetadata(cancelled, "path", "three", session); err == nil {
		t.Fatal("cancelled metadata write succeeded")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportRecords(ctx); err == nil {
		t.Fatal("closed manifest read succeeded")
	}
	if err := store.SaveImportMetadata(ctx, "path", "three", session); err == nil {
		t.Fatal("closed metadata write succeeded")
	}
}
