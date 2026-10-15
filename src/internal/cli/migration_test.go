// Package cli verifies upgrade safety before schema migration.
package cli

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestOpenStoreBacksUpPriorVersionBeforeMigration(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	paths, _ := config.Resolve()
	store, err := openStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.SaveImport(context.Background(), "fixture", "one", domain.Session{ID: "upgrade-session", ExternalSessionID: "upgrade-session", Title: "Preserved", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err = os.WriteFile(paths.VersionMarker, []byte("2.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err = openStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	entries, err := os.ReadDir(paths.Backups)
	if err != nil {
		t.Fatal(err)
	}
	var backupPath string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "pre-migration-2.0.1-to-2.1.0-") {
			backupPath = paths.Backups + "/" + entry.Name()
		}
	}
	if backupPath == "" {
		t.Fatalf("pre-migration backup missing: %v", entries)
	}
	backup, err := storage.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if session, err := backup.GetSession(context.Background(), "upgrade-session"); err != nil || session.Title != "Preserved" {
		t.Fatalf("backup lost source data: session=%+v err=%v", session, err)
	}
}
