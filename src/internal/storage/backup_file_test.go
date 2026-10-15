// Package storage verifies the pre-migration backup entry point used by release upgrades.
package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupDatabaseFileCopiesConsistentPrivateDatabaseAndRejectsSymlink(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "source.db")
	store, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	destination := filepath.Join(home, "backup.db")
	if err = BackupDatabaseFile(context.Background(), source, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() == 0 {
		t.Fatalf("backup info=%v err=%v", info, err)
	}
	link := filepath.Join(home, "source-link.db")
	if err = os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if err = BackupDatabaseFile(context.Background(), link, filepath.Join(home, "unsafe.db")); err == nil {
		t.Fatal("symlink database source accepted")
	}
	if err = BackupDatabaseFile(context.Background(), filepath.Join(home, "missing.db"), filepath.Join(home, "missing-backup.db")); !os.IsNotExist(err) {
		t.Fatalf("missing source err=%v", err)
	}
}
