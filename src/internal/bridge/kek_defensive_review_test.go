// @module BridgeKEKDefensiveReviewTests checks inventory paging and a custom-key interrupted cutover.
package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// B12/K01: a retained task inventory must traverse beyond the 128-row rewrap page.
func TestDefensiveKEKRotationRewrapsBeyondFirstPage(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 130; i++ {
		_, err = store.db.Exec(`INSERT INTO tasks(id,workspace_id,device_id,principal,request_json,status,created_at)
			SELECT ?,workspace_id,device_id,principal,request_json,status,created_at FROM tasks WHERE status='queued' LIMIT 1`, fmt.Sprintf("review-%03d", i))
		if err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	before := rotationRecords(t, path)
	old, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	result, err := RotateKEK(context.Background(), path, "paged-inventory")
	if err != nil || result.Records != len(before) {
		t.Fatalf("paged inventory result=%+v records=%d err=%v", result, len(before), err)
	}
	newCipher, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	after := rotationRecords(t, path)
	if len(after) != len(before) {
		t.Fatalf("record count changed: %d to %d", len(before), len(after))
	}
	for i := range before {
		want, err := old.Open(before[i])
		if err != nil {
			t.Fatal(err)
		}
		got, err := newCipher.Open(after[i])
		if err != nil || !bytes.Equal(got, want) || bytes.Equal(before[i], after[i]) {
			t.Fatalf("record %d not authentically rewrapped: %v", i, err)
		}
		if _, err := old.Open(after[i]); err == nil {
			t.Fatalf("record %d accepts old KEK", i)
		}
	}
}

// B12/K05,K07: interrupted two-directory replacement must retain a matching pair.
func TestDefensiveKEKRotationCustomKeyRecovery(t *testing.T) {
	for _, action := range []string{"commit", "rollback"} {
		t.Run(action, func(t *testing.T) {
			path, _, _ := rotationFixture(t, true)
			keyDir := filepath.Join(filepath.Dir(path), "separate-key-directory")
			if err := os.Mkdir(keyDir, 0700); err != nil {
				t.Fatal(err)
			}
			keyPath := filepath.Join(keyDir, "bridge.kek")
			if err := os.Rename(path+".key", keyPath); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SESSIONSIFT_BRIDGE_KEK_FILE", keyPath)
			_, err := rotateKEK(context.Background(), path, "separate-key", rotationIO{fault: func(point string) error {
				if point == "live-db-rename" {
					return errors.New("synthetic interruption after DB replacement")
				}
				return nil
			}})
			if err == nil {
				t.Fatal("interruption did not occur")
			}
			if store, err := Open(path); err == nil {
				store.Close()
				t.Fatal("startup accepted an unresolved custom-key cutover")
			}
			result, err := RecoverKEK(context.Background(), path, action)
			if err != nil {
				t.Fatal(err)
			}
			wantGeneration := int64(1)
			if action == "rollback" {
				wantGeneration = 0
			}
			if result.Generation != wantGeneration {
				t.Fatalf("generation=%d, want %d", result.Generation, wantGeneration)
			}
			generation, _, count, err := validateRotationPair(context.Background(), path, keyPath)
			if err != nil || generation != wantGeneration || count != 4 {
				t.Fatalf("custom-key recovery pair generation=%d records=%d err=%v", generation, count, err)
			}
			if _, err := os.Lstat(path + ".key"); !os.IsNotExist(err) {
				t.Fatalf("unexpected default key after custom-key recovery: %v", err)
			}
		})
	}
}

// B12/K08: replacing the live key must never replace the inode that carries the maintenance flock.
func TestDefensiveKEKRotationRejectsLockPathAsKeyPath(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	lockPath := path + ".maintenance.lock"
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".key", lockPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SESSIONSIFT_BRIDGE_KEK_FILE", lockPath)
	beforeDB, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeKey, err := fileDigest(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var bypass bool
	_, err = rotateKEK(context.Background(), path, "lock-alias", rotationIO{fault: func(point string) error {
		if point == "live-key-rename" {
			second, acquireErr := acquireMaintenanceLock(path)
			if acquireErr == nil {
				bypass = true
				second.Close()
			}
		}
		return nil
	}})
	if err == nil && bypass {
		t.Fatal("key cutover replaced the locked inode; a second maintenance process acquired the lock before the first exited")
	}
	if err == nil {
		t.Fatal("accepted a KEK path that aliases the maintenance lock")
	}
	afterDB, dbErr := fileDigest(path)
	afterKey, keyErr := fileDigest(lockPath)
	if dbErr != nil || keyErr != nil || beforeDB != afterDB || beforeKey != afterKey {
		t.Fatalf("rejected alias changed the original database or KEK: db=%v key=%v", dbErr, keyErr)
	}
	if _, journalErr := os.Lstat(path + ".rotation-journal"); !os.IsNotExist(journalErr) {
		t.Fatalf("rejected alias published a journal: %v", journalErr)
	}
	if _, rootErr := os.Lstat(path + ".rotation-lock-alias"); !os.IsNotExist(rootErr) {
		t.Fatalf("rejected alias created a rotation workspace: %v", rootErr)
	}
}

// B12/K12: preflight promises no state mutation except initial lock creation.
func TestDefensiveKEKPreflightIsReadOnly(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	beforeDB, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeKey, err := fileDigest(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	sidecars := []string{"-wal", "-shm", "-journal", ".rotation-journal"}
	beforeSidecars := make(map[string]bool, len(sidecars))
	for _, suffix := range sidecars {
		_, statErr := os.Lstat(path + suffix)
		beforeSidecars[suffix] = statErr == nil
	}
	if _, err := PreflightKEK(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	afterDB, dbErr := fileDigest(path)
	afterKey, keyErr := fileDigest(path + ".key")
	if dbErr != nil || keyErr != nil || beforeDB != afterDB || beforeKey != afterKey {
		t.Fatalf("preflight changed database or key: db=%v key=%v", dbErr, keyErr)
	}
	for _, suffix := range sidecars {
		_, statErr := os.Lstat(path + suffix)
		if (statErr == nil) != beforeSidecars[suffix] {
			t.Fatalf("preflight changed SQLite sidecar or rotation journal %q: %v", suffix, statErr)
		}
	}
}
