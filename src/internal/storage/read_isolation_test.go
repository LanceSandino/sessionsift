// @module ReadIsolationTests prevents ingestion from starving interactive session reads.
package storage

import (
	"context"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInteractiveReadsDoNotWaitForIngestionConnection(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SaveImport(context.Background(), "path", "version", domain.Session{ID: "session", ExternalSessionID: "session", Title: "Existing", ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE sessions SET title='Uncommitted' WHERE id='session'`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	got, err := s.GetSession(ctx, "session")
	if err != nil || got.Title != "Existing" {
		t.Fatalf("read stalled behind ingestion: %v", err)
	}
	if values, err := s.ListSessions(ctx); err != nil || len(values) != 1 {
		t.Fatalf("list stalled: %v", err)
	}
}

func TestInteractiveReaderIsReadOnlyAndReportsCorruption(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.reader().Exec(`DELETE FROM sessions`); err == nil {
		t.Fatal("interactive reader accepted a write")
	}
	if err = s.SaveImport(context.Background(), "path", "version", domain.Session{ID: "broken", ExternalSessionID: "broken"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE sessions SET payload_json='invalid json'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetSession(context.Background(), "broken"); err == nil {
		t.Fatal("invalid payload accepted")
	}
	if _, err = s.ListSessions(context.Background()); err == nil {
		t.Fatal("invalid list payload accepted")
	}
}

func TestBackupFailsForInvalidDestinationsAndClosedStore(t *testing.T) {
	directory := t.TempDir()
	s, err := Open(filepath.Join(directory, "read.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.Backup(ctx, filepath.Join(directory, "missing", "backup.db")); err == nil {
		t.Fatal("missing parent accepted")
	}
	file := filepath.Join(directory, "file")
	if err = os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Backup(ctx, filepath.Join(file, "backup.db")); err == nil {
		t.Fatal("non-directory parent accepted")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Backup(ctx, filepath.Join(directory, "backup.db")); err == nil {
		t.Fatal("closed database backed up")
	}
}
