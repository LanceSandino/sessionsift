package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// ASAP5-F3: under a traversable backups directory and a permissive umask, no
// entry the backup creates may ever be readable by another account.
func TestBackupNeverExposesReadableEntriesWhileWriting(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(filepath.Join(directory, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 400; i++ {
		id := "s" + strings.Repeat("x", 4) + string(rune('a'+i%26)) + time.Duration(i).String()
		session := domain.Session{ID: id, ExternalSessionID: id, Title: strings.Repeat("padding ", 200), StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}
		if err := store.SaveImport(ctx, "path-"+id, "hash", session); err != nil {
			t.Fatal(err)
		}
	}
	backups := filepath.Join(directory, "backups")
	if err := os.Mkdir(backups, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(backups, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := syscall.Umask(0)
	defer syscall.Umask(previous)
	var exposed atomic.Value
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			entries, _ := os.ReadDir(backups)
			for _, entry := range entries {
				if info, statErr := os.Lstat(filepath.Join(backups, entry.Name())); statErr == nil && info.Mode().Perm()&0o077 != 0 {
					exposed.Store(entry.Name() + " " + info.Mode().String())
				}
			}
		}
	}()
	for i := 0; i < 5; i++ {
		if err := store.Backup(ctx, filepath.Join(backups, "snapshot-"+string(rune('a'+i))+".db")); err != nil {
			t.Fatal(err)
		}
	}
	stop.Store(true)
	<-done
	if value := exposed.Load(); value != nil {
		t.Fatalf("backup exposed a group/world-accessible entry while writing: %v", value)
	}
	entries, _ := os.ReadDir(backups)
	if len(entries) != 5 {
		t.Fatalf("backups left %d entries, want exactly the 5 snapshots", len(entries))
	}
}
