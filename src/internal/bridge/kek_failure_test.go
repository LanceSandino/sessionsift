// @module BridgeKEKFailureTests proves returned faults and malformed recovery state preserve a usable pair.
package bridge

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestKEKFaultReturnsPreserveRecovery(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	points := []string{}
	if _, err := rotateKEK(context.Background(), path, "enumerate", rotationIO{fault: func(point string) error { points = append(points, point); return nil }}); err != nil {
		t.Fatal(err)
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			path, _, _ := rotationFixture(t, true)
			_, err := rotateKEK(context.Background(), path, "fault", rotationIO{fault: func(label string) error {
				if label == point {
					return syscall.EIO
				}
				return nil
			}})
			if !errors.Is(err, syscall.EIO) {
				t.Fatalf("expected storage error at %s: %v", point, err)
			}
			if _, err := os.Lstat(path + ".rotation-journal"); err == nil {
				if _, err = RecoverKEK(context.Background(), path, "commit"); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, count, err := validateRotationPair(context.Background(), path, path+".key"); err != nil || count != 4 {
				t.Fatal("returned failure lost matching pair")
			}
		})
	}
}

func TestKEKRecoveryRejectsTamperedJournalAndPairs(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	_, err := rotateKEK(context.Background(), path, "tamper", rotationIO{fault: func(point string) error {
		if point == "journal-dirsync" {
			return syscall.EIO
		}
		return nil
	}})
	if !errors.Is(err, syscall.EIO) {
		t.Fatal("pending fixture not created")
	}
	journalPath := path + ".rotation-journal"
	original, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := fileDigest(path)
	for _, kind := range []string{"json", "unknown", "duplicate", "generation", "operation", "key-location", "records", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			var journal rotationJournal
			if err := json.Unmarshal(original, &journal); err != nil {
				t.Fatal(err)
			}
			body := original
			switch kind {
			case "json":
				body = []byte("invalid JSON")
			case "unknown":
				body = append(append([]byte{}, original[:len(original)-1]...), []byte(`,"unknown":true}`)...)
			case "duplicate":
				body = append(append([]byte{}, original[:len(original)-1]...), []byte(`,"version":1}`)...)
			case "generation":
				journal.NewGeneration = 77
			case "operation":
				journal.Operation = "../traversal"
			case "key-location":
				journal.KeyLocation = strings.Repeat("0", 64)
			case "records":
				journal.Records = 0
			case "oversize":
				body = []byte(strings.Repeat("x", 4097))
			}
			if kind == "generation" || kind == "operation" || kind == "key-location" || kind == "records" {
				body, _ = json.Marshal(journal)
			}
			if err := os.WriteFile(journalPath, body, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := RecoverKEK(context.Background(), path, "commit"); err == nil {
				t.Fatal("tampered journal accepted")
			}
			after, _ := fileDigest(path)
			if before != after {
				t.Fatal("tampered journal changed live state")
			}
		})
	}
	if err := os.WriteFile(journalPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	sidecar := path + "-wal"
	if err := os.WriteFile(sidecar, []byte("synthetic unrelated sidecar"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverKEK(context.Background(), path, "rollback"); err == nil {
		t.Fatal("unexpected WAL accepted")
	}
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	nextKey := filepath.Join(path+".rotation-tamper", "new", "bridge.db.key")
	originalKey, err := os.ReadFile(nextKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nextKey, []byte("bad replacement key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverKEK(context.Background(), path, "commit"); err == nil {
		t.Fatal("modified staged key accepted")
	}
	if err := os.WriteFile(nextKey, originalKey, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverKEK(context.Background(), path, "rollback"); err != nil {
		t.Fatal(err)
	}
}

func TestKEKPreflightPathAndTransactionFailures(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	for _, value := range []string{"relative.db", path + "/../bridge.db"} {
		if _, err := PreflightKEK(context.Background(), value); err == nil {
			t.Fatal("unclean database path accepted")
		}
	}
	for _, id := range []string{"", "../escape", strings.Repeat("a", 65)} {
		if _, err := RotateKEK(context.Background(), path, id); err == nil {
			t.Fatal("invalid operation accepted")
		}
	}
	for _, suffix := range []string{"", ".maintenance.lock", ".rotation-journal", "-wal", "-shm", "-journal", ".rotation-reserved/key"} {
		t.Run("reserved"+suffix, func(t *testing.T) {
			t.Setenv("SESSIONSIFT_BRIDGE_KEK_FILE", path+suffix)
			if _, err := PreflightKEK(context.Background(), path); err == nil {
				t.Fatal("reserved key location accepted")
			}
		})
	}
	if _, err := RecoverKEK(context.Background(), path, "invalid"); err == nil {
		t.Fatal("invalid recovery action accepted")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sidecar := path + suffix
		body := []byte("synthetic pending SQLite state must not be ignored")
		if err := os.WriteFile(sidecar, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreflightKEK(context.Background(), path); err == nil {
			t.Fatal("immutable preflight ignored a SQLite sidecar")
		}
		retained, err := os.ReadFile(sidecar)
		if err != nil || string(retained) != string(body) {
			t.Fatal("preflight mutated pending SQLite state")
		}
		if err := os.Remove(sidecar); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreflightKEK(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("preflight cancellation lost")
	}
	if _, err := RecoverKEK(ctx, path, "commit"); !errors.Is(err, context.Canceled) {
		t.Fatal("recovery cancellation lost")
	}
	old, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	next, err := newEnvelopeCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.rewrap([]byte("malformed envelope"), next); err == nil {
		t.Fatal("malformed rewrap accepted")
	}
	before := rotationRecords(t, path)
	db, err := openWritableSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER reject_rewrap BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT, 'synthetic denied write'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = rewrapDatabase(context.Background(), db, old, next, 1, "trigger"); err == nil {
		t.Fatal("failed task rewrite committed")
	}
	if _, err = db.Exec(`DROP TRIGGER reject_rewrap; DROP TABLE bridge_key_check`); err != nil {
		t.Fatal(err)
	}
	if _, err = rewrapDatabase(context.Background(), db, old, next, 1, "missing-verifier"); err == nil {
		t.Fatal("missing verifier accepted")
	}
	after := rotationRecordsWithoutVerifier(t, db)
	for _, blob := range after {
		if _, err := old.Open(blob); err != nil {
			t.Fatal("transaction failure partially rewrapped a task")
		}
	}
	if len(after) != len(before)-1 {
		t.Fatal("transaction failure lost tasks")
	}
}

func rotationRecordsWithoutVerifier(t *testing.T, db interface {
	Query(string, ...any) (*sql.Rows, error)
}) [][]byte {
	t.Helper()
	rows, err := db.Query(`SELECT request_json,response_json FROM tasks ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := [][]byte{}
	for rows.Next() {
		var request, response []byte
		if err := rows.Scan(&request, &response); err != nil {
			t.Fatal(err)
		}
		result = append(result, request)
		if response != nil {
			result = append(result, response)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestKEKValidationAndIOErrorsPreserveOriginals(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	before, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	fs := rotationIO{}
	missing := filepath.Join(filepath.Dir(path), "missing")
	if err := syncFile(missing); err == nil {
		t.Fatal("missing file sync succeeded")
	}
	if err := syncDirectory(missing); err == nil {
		t.Fatal("missing directory sync succeeded")
	}
	if _, err := fileDigest(missing); err == nil {
		t.Fatal("missing file digest succeeded")
	}
	if err := fs.copy("missing", missing, path); err == nil {
		t.Fatal("missing snapshot replaced database")
	}
	if err := fs.replace("denied", path, func(*os.File) error { return syscall.EIO }); !errors.Is(err, syscall.EIO) {
		t.Fatal("write failure was not propagated")
	}
	if digest, err := fileDigest(path); err != nil || digest != before {
		t.Fatal("failed replacement changed original")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := privateFile(file); err == nil {
		t.Fatal("closed protected file accepted")
	}
	if _, err := acquireMaintenanceLock(filepath.Join(path, "not-a-directory.db")); err == nil {
		t.Fatal("file used as lock directory")
	}
	root := filepath.Dir(path)
	if err := os.Chmod(root, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireMaintenanceLock(path); err == nil {
		t.Fatal("writable lock directory accepted")
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".maintenance.lock"
	if err := os.Chmod(lockPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireMaintenanceLock(path); err == nil {
		t.Fatal("permissive lock file accepted")
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, lockPath); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireMaintenanceLock(path); err == nil {
		t.Fatal("symlink lock accepted")
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	badKey := filepath.Join(root, "short-base64.key")
	if err := os.WriteFile(badKey, []byte("YQ"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := existingCipher(badKey); err == nil {
		t.Fatal("decoded short key accepted")
	}
	badDB := filepath.Join(root, "corrupt.db")
	if err := os.WriteFile(badDB, []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := validateRotationPair(context.Background(), badDB, path+".key"); err == nil {
		t.Fatal("corrupt database validated")
	}
	if db, err := openWritableSnapshot(badDB); err == nil {
		db.Close()
		t.Fatal("corrupt writable snapshot accepted")
	}
	old, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(old.backupKey)
	next, err := newEnvelopeCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	db, err := openWritableSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := old.Seal([]byte("authenticated but wrong verifier"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE bridge_key_check SET envelope=? WHERE id=1`, verifier); err != nil {
		t.Fatal(err)
	}
	if _, err := rewrapDatabase(context.Background(), db, old, next, 1, "invalid-verifier"); err == nil {
		t.Fatal("wrong verifier committed")
	}
	if _, _, _, err := validateRotationPair(context.Background(), path, path+".key"); err == nil {
		t.Fatal("wrong verifier authenticated")
	}
	verifier, err = old.Seal([]byte("sessionsift.key-check.v1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE bridge_key_check SET envelope=? WHERE id=1; DROP TABLE tasks`, verifier); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := validateRotationPair(context.Background(), path, path+".key"); err == nil {
		t.Fatal("missing inventory validated")
	}
	db.Close()
	if _, err := rewrapDatabase(context.Background(), db, old, next, 1, "closed"); err == nil {
		t.Fatal("closed database transaction accepted")
	}
}

type unavailableRotationEntropy struct{}

func (unavailableRotationEntropy) Read([]byte) (int, error) { return 0, syscall.EIO }

func TestKEKEntropyAndGenerationFailuresRemainRecoverable(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	old, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(old.backupKey)
	keyHash, err := fileDigest(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	before := rotationRecords(t, path)
	reader := rand.Reader
	func() {
		rand.Reader = unavailableRotationEntropy{}
		defer func() { rand.Reader = reader }()
		if _, err := old.rewrap(before[0], old); !errors.Is(err, syscall.EIO) {
			t.Fatal("rewrap ignored entropy failure")
		}
		if _, err := RotateKEK(context.Background(), path, "entropy-failure"); !errors.Is(err, syscall.EIO) {
			t.Fatal("key generation ignored entropy failure")
		}
	}()
	if after, err := fileDigest(path + ".key"); err != nil || after != keyHash {
		t.Fatal("entropy failure replaced the key")
	}
	if _, err := os.Lstat(path + ".rotation-journal"); !os.IsNotExist(err) {
		t.Fatal("entropy failure published a cutover")
	}
	for _, blob := range rotationRecords(t, path) {
		if _, err := old.Open(blob); err != nil {
			t.Fatal("entropy failure destroyed recoverability")
		}
	}
	db, err := openWritableSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE bridge_kek_generation(id INTEGER PRIMARY KEY,generation INTEGER,operation TEXT); INSERT INTO bridge_kek_generation VALUES(1,?,?)`, int64(math.MaxInt64), "maximum"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	beforeHash, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RotateKEK(context.Background(), path, "overflow"); err == nil {
		t.Fatal("generation overflow accepted")
	}
	if after, err := fileDigest(path); err != nil || after != beforeHash {
		t.Fatal("overflow changed database")
	}
	db, err = openWritableSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE bridge_kek_generation SET generation=-1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := PreflightKEK(context.Background(), path); err == nil {
		t.Fatal("negative generation authenticated")
	}
}
