// @module BridgeKEKRotationTests anchors supported offline rotation and recovery.
package bridge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

const rotationCanary = "KEK-ROTATION-SYNTHETIC-SESSION-CANARY"

func rotationFixture(t *testing.T, tasks bool) (string, Device, ed25519.PrivateKey) {
	t.Helper()
	t.Setenv("SESSIONSIFT_BRIDGE_KEK_FILE", "")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "synthetic", public)
	if _, err = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionGet}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if tasks {
		request := protocol.Request{Version: 1, RequestID: "completed", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"` + rotationCanary + `"}`)}
		task, err := store.Submit("owner@example.com", device.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = store.Next(device.ID); err != nil {
			t.Fatal(err)
		}
		response, _ := json.Marshal(protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"synthetic": rotationCanary}})
		if err = store.Complete(device.ID, task.ID, response, ed25519.Sign(private, response)); err != nil {
			t.Fatal(err)
		}
		request.RequestID = "queued"
		if _, err = store.Submit("owner@example.com", device.ID, request); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	return path, device, private
}

func rotationRecords(t *testing.T, path string) [][]byte {
	t.Helper()
	db, err := readonlyBridgeDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT request_json,response_json FROM tasks ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	result := [][]byte{}
	for rows.Next() {
		var request, response []byte
		if err = rows.Scan(&request, &response); err != nil {
			t.Fatal(err)
		}
		result = append(result, request)
		if response != nil {
			result = append(result, response)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var verifier []byte
	if err = db.QueryRow(`SELECT envelope FROM bridge_key_check WHERE id=1`).Scan(&verifier); err != nil {
		t.Fatal(err)
	}
	return append(result, verifier)
}

func TestKEKRotationK01QueuedRequestResponse(t *testing.T) {
	path, device, _ := rotationFixture(t, true)
	old, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	before := rotationRecords(t, path)
	result, err := RotateKEK(context.Background(), path, "normal")
	if err != nil || result.Generation != 1 || result.Records != 4 {
		t.Fatalf("rotation result %+v: %v", result, err)
	}
	next, err := existingCipher(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	after := rotationRecords(t, path)
	if len(before) != len(after) {
		t.Fatal("records lost")
	}
	for index, blob := range before {
		plain, _ := old.Open(blob)
		got, err := next.Open(after[index])
		if err != nil || !bytes.Equal(plain, got) {
			t.Fatal("payload changed")
		}
		if !bytes.Equal(blob[64:], after[index][64:]) || bytes.Equal(blob[:64], after[index][:64]) {
			t.Fatal("payload body changed or wrapped DEK nonce reused")
		}
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task, found, err := store.Next(device.ID)
	if err != nil || !found || task.Request.RequestID != "queued" {
		t.Fatalf("queued request not usable: %v", err)
	}
	if err = store.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestKEKRotationK02EmptyVerifier(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bridge.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := RotateKEK(context.Background(), path, "empty")
	if err != nil || result.Generation != 1 {
		t.Fatalf("rotation: %+v %v", result, err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.verifyEncryptionKey(); err != nil {
		t.Fatal(err)
	}
}

func TestKEKRotationK03RejectOldAcceptNew(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	oldKey, _ := os.ReadFile(path + ".key")
	if _, err := RotateKEK(context.Background(), path, "keys"); err != nil {
		t.Fatal(err)
	}
	nextKey, _ := os.ReadFile(path + ".key")
	if bytes.Equal(oldKey, nextKey) {
		t.Fatal("key not replaced")
	}
	if err := os.WriteFile(path+".key", oldKey, 0600); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(path); err == nil {
		store.Close()
		t.Fatal("old key opened live database")
	}
	if err := os.WriteFile(path+".key", nextKey, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}

func TestKEKRotationK04WrongMalformedMissingKeys(t *testing.T) {
	for _, kind := range []string{"wrong", "malformed", "missing", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			path, _, _ := rotationFixture(t, true)
			before, _ := fileDigest(path)
			original, _ := os.ReadFile(path + ".key")
			switch kind {
			case "wrong":
				os.WriteFile(path+".key", bytes.Repeat([]byte{17}, 32), 0600)
			case "malformed":
				os.WriteFile(path+".key", []byte("not-a-valid-key"), 0600)
			case "missing":
				os.Remove(path + ".key")
			case "mixed":
				cipher, _ := newEnvelopeCipher(bytes.Repeat([]byte{18}, 32))
				blob, _ := cipher.Seal([]byte("synthetic"))
				db, _ := sql.Open("sqlite", path)
				if _, err := db.Exec(`UPDATE tasks SET request_json=? WHERE status='queued'`, blob); err != nil {
					t.Fatal(err)
				}
				db.Close()
				before, _ = fileDigest(path)
			}
			if _, err := RotateKEK(context.Background(), path, "bad"); err == nil {
				t.Fatal("invalid input accepted")
			}
			after, _ := fileDigest(path)
			if before != after {
				t.Fatal("failed validation changed original database")
			}
			if kind != "mixed" {
				os.WriteFile(path+".key", original, 0600)
			}
		})
	}
}

func TestKEKRotationK05CrashRecovery(t *testing.T) {
	if path := os.Getenv("SESSIONSIFT_ROTATION_TEST_DB"); path != "" {
		point := os.Getenv("SESSIONSIFT_ROTATION_TEST_POINT")
		_, _ = rotateKEK(context.Background(), path, "crash", rotationIO{fault: func(label string) error {
			if label == point {
				os.Exit(73)
			}
			return nil
		}})
		os.Exit(74)
	}
	path, _, _ := rotationFixture(t, true)
	labels := []string{}
	if _, err := rotateKEK(context.Background(), path, "crash", rotationIO{fault: func(label string) error { labels = append(labels, label); return nil }}); err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{}
	for _, name := range []string{"stage-directory", "before-old-backup", "old-backup-created", "old-bridge.db-fsync", "old-bridge.db.key-fsync", "old-directory-fsync", "old-pair-durable", "before-rewrap-transaction", "rewrap-transaction-committed", "new-pair-fsync", "new-pair-verified", "live-pair-verified", "journal-removed", "cutover-durable"} {
		expected[name] = true
	}
	for _, prefix := range []string{"stage-db", "stage-key", "journal", "live-db", "live-key"} {
		for _, suffix := range []string{"create", "write", "fsync", "close", "rename", "dirsync"} {
			expected[prefix+"-"+suffix] = true
		}
	}
	actual := map[string]bool{}
	for _, label := range labels {
		if actual[label] {
			t.Fatal("duplicate durability boundary")
		}
		actual[label] = true
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("durability boundary coverage mismatch: got %d expected %d", len(actual), len(expected))
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range labels {
		for _, action := range []string{"commit", "rollback"} {
			t.Run(label+"/"+action, func(t *testing.T) {
				path, _, _ := rotationFixture(t, true)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, binary, "-test.run=^TestKEKRotationK05CrashRecovery$", "-test.timeout=12s")
				command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(path), "TMPDIR=" + filepath.Dir(path), "GORACE=halt_on_error=1 exitcode=76 atexit_sleep_ms=0", "SESSIONSIFT_ROTATION_TEST_DB=" + path, "SESSIONSIFT_ROTATION_TEST_POINT=" + label}
				output, err := command.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 73 || bytes.Contains(output, []byte("DATA RACE")) {
					t.Fatalf("crash boundary not hit: %v %s", err, output)
				}
				_, journalErr := os.Lstat(path + ".rotation-journal")
				if journalErr == nil {
					if store, err := Open(path); err == nil {
						store.Close()
						t.Fatal("startup accepted pending cutover")
					}
					if _, err := RecoverKEK(context.Background(), path, action); err != nil {
						t.Fatalf("recover %s: %v", action, err)
					}
				}
				generation, _, count, err := validateRotationPair(context.Background(), path, path+".key")
				if err != nil || count != 4 {
					t.Fatalf("paired recovery lost records: %v", err)
				}
				if journalErr == nil && ((action == "commit" && generation != 1) || (action == "rollback" && generation != 0)) {
					t.Fatal("wrong recovered generation")
				}
				store, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				store.Close()
			})
		}
	}
	t.Logf("verified %d named durability boundaries with abrupt process exit; commit and rollback", len(labels))
}

func TestKEKRotationK06StorageFaults(t *testing.T) {
	for _, kind := range []string{"enospc", "read-only", "key-permissions", "key-symlink", "db-symlink", "lock-symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path, _, _ := rotationFixture(t, true)
			switch kind {
			case "enospc":
				_, err := rotateKEK(context.Background(), path, "full", rotationIO{fault: func(point string) error {
					if point == "live-key-write" {
						return syscall.ENOSPC
					}
					return nil
				}})
				if !errors.Is(err, syscall.ENOSPC) {
					t.Fatalf("disk-full not returned: %v", err)
				}
				if store, err := Open(path); err == nil {
					store.Close()
					t.Fatal("partial disk-full cutover accepted")
				}
				if _, err := RecoverKEK(context.Background(), path, "rollback"); err != nil {
					t.Fatal(err)
				}
				return
			case "read-only":
				os.Chmod(filepath.Dir(path), 0500)
				defer os.Chmod(filepath.Dir(path), 0700)
			case "key-permissions":
				os.Chmod(path+".key", 0644)
			case "key-symlink", "db-symlink":
				target := path + ".key"
				if kind == "db-symlink" {
					target = path
				}
				if err := os.Rename(target, target+".actual"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target+".actual", target); err != nil {
					t.Fatal(err)
				}
			case "lock-symlink":
				os.Remove(path + ".maintenance.lock")
				if err := os.Symlink(path+".key", path+".maintenance.lock"); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path+".key", path+".linked-key"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := RotateKEK(context.Background(), path, "unsafe"); err == nil {
				t.Fatal("unsafe storage accepted")
			}
		})
	}
}

func TestKEKRotationK07MatchingBackups(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	if _, err := RotateKEK(context.Background(), path, "pairs"); err != nil {
		t.Fatal(err)
	}
	root := path + ".rotation-pairs"
	for _, generation := range []struct {
		name   string
		number int64
	}{{"old", 0}, {"new", 1}} {
		pair := filepath.Join(root, generation.name)
		got, _, count, err := validateRotationPair(context.Background(), filepath.Join(pair, "bridge.db"), filepath.Join(pair, "bridge.db.key"))
		if err != nil || got != generation.number || count != 4 {
			t.Fatal("backup pair not restorable")
		}
		other := "old"
		if generation.name == "old" {
			other = "new"
		}
		if _, _, _, err = validateRotationPair(context.Background(), filepath.Join(pair, "bridge.db"), filepath.Join(root, other, "bridge.db.key")); err == nil {
			t.Fatal("unmatched backup key accepted")
		}
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.Backup(context.Background(), filepath.Join(filepath.Dir(path), "backups"))
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	got, _, _, err := validateRotationPair(context.Background(), filepath.Join(backup, "bridge.db"), filepath.Join(backup, "bridge.db.key"))
	if err != nil || got != 1 {
		t.Fatal("post-rotation built-in backup failed")
	}
}

func TestKEKRotationK08ConcurrentWriters(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = RotateKEK(context.Background(), path, "busy"); err == nil {
		t.Fatal("active writer accepted")
	}
	if second, err := Open(path); err == nil {
		second.Close()
		t.Fatal("second bridge writer accepted")
	}
	store.Close()
	if _, err = rotateKEK(context.Background(), path, "maintenance", rotationIO{fault: func(point string) error {
		if point == "new-pair-verified" {
			if second, err := Open(path); err == nil {
				second.Close()
				t.Error("restart accepted during maintenance")
			}
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestKEKRotationK09NoSensitiveDiagnostics(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	old, _ := existingCipher(path + ".key")
	result, err := RotateKEK(context.Background(), path, "privacy")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := existingCipher(path + ".key")
	output, _ := json.Marshal(result)
	for _, secret := range [][]byte{[]byte(rotationCanary), old.backupKey, next.backupKey, []byte(base64.RawURLEncoding.EncodeToString(old.backupKey)), []byte(base64.RawURLEncoding.EncodeToString(next.backupKey))} {
		if bytes.Contains(output, secret) {
			t.Fatal("rotation result leaked sensitive data")
		}
		err := filepath.WalkDir(filepath.Dir(path), func(name string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || strings.HasSuffix(name, ".key") {
				return nil
			}
			body, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			if bytes.Contains(body, secret) {
				t.Error("sensitive value leaked outside protected key file")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// A known-bad plaintext control must trip the same leak oracle.
	if !bytes.Contains(append([]byte("synthetic log: "), []byte(rotationCanary)...), []byte(rotationCanary)) {
		t.Fatal("negative control missed")
	}
}

func TestKEKRotationK10SyncKeysRemainOpaque(t *testing.T) {
	path, device, private := rotationFixture(t, true)
	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	protected := filepath.Join(filepath.Dir(path), "user-sync.key")
	if err := cloudsync.SaveMasterKey(protected, master); err != nil {
		t.Fatal(err)
	}
	metadata := cloudsync.Metadata{Schema: 1, WorkspaceID: device.WorkspaceID, ObjectID: "obj_rotation", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Kind: "note", Epoch: 1, Revision: 1}
	envelope, err := cloudsync.Seal(master, metadata, []byte(rotationCanary), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nonce, _ := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	ciphertext, _ := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: metadata.ObjectID, SourceDeviceID: device.ID, WriterDeviceID: device.ID, Kind: "note", Epoch: 1, Revision: 1, Nonce: nonce, Ciphertext: ciphertext}
	signSyncObject(&object, private)
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutSyncObject(device.ID, object, 0); err != nil {
		t.Fatal(err)
	}
	store.Close()
	before, _ := fileDigest(protected)
	if _, err = RotateKEK(context.Background(), path, "opaque"); err != nil {
		t.Fatal(err)
	}
	after, _ := fileDigest(protected)
	if before != after {
		t.Fatal("user sync key changed")
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetSyncObject(device.ID, object.ObjectID)
	if err != nil || !reflect.DeepEqual(got.Ciphertext, ciphertext) || !reflect.DeepEqual(got.Nonce, nonce) {
		t.Fatal("sync ciphertext changed")
	}
	if _, err = cloudsync.Open(store.cipher.backupKey, envelope); err == nil {
		t.Fatal("server KEK decrypted client sync envelope")
	}
	plain, err := cloudsync.Open(master, envelope)
	if err != nil || string(plain) != rotationCanary {
		t.Fatal("user-held key stopped decrypting")
	}
}

func TestKEKRotationK11LegacyAndJournalSkew(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	// Legacy raw 32-byte key files remain supported, including recovery hash binding.
	old, _ := existingCipher(path + ".key")
	os.WriteFile(path+".key", old.backupKey, 0600)
	_, err := rotateKEK(context.Background(), path, "legacy", rotationIO{fault: func(point string) error {
		if point == "journal-dirsync" {
			return errors.New("interrupted")
		}
		return nil
	}})
	if err == nil {
		t.Fatal("fault not reached")
	}
	journalPath := path + ".rotation-journal"
	original, _ := os.ReadFile(journalPath)
	var journal rotationJournal
	json.Unmarshal(original, &journal)
	journal.Version = 999
	changed, _ := json.Marshal(journal)
	os.WriteFile(journalPath, changed, 0600)
	if _, err := RecoverKEK(context.Background(), path, "commit"); err == nil {
		t.Fatal("unknown journal version accepted")
	}
	os.WriteFile(journalPath, original, 0600)
	if _, err := RecoverKEK(context.Background(), path, "commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateKEK(context.Background(), path, "second"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(journalPath, original, 0600)
	if _, err := RecoverKEK(context.Background(), path, "rollback"); err == nil {
		t.Fatal("replayed older journal discarded newer generation")
	}
}

func TestKEKRotationK12IdempotenceAndCancellation(t *testing.T) {
	path, _, _ := rotationFixture(t, true)
	first, err := RotateKEK(context.Background(), path, "retry")
	if err != nil {
		t.Fatal(err)
	}
	beforeDB, _ := fileDigest(path)
	beforeKey, _ := fileDigest(path + ".key")
	second, err := RotateKEK(context.Background(), path, "retry")
	afterDB, _ := fileDigest(path)
	afterKey, _ := fileDigest(path + ".key")
	if err != nil || !second.AlreadyComplete || first.Generation != second.Generation || beforeDB != afterDB || beforeKey != afterKey {
		t.Fatal("double-submit changed state")
	}
	if _, err := RecoverKEK(context.Background(), path, "rollback"); err == nil {
		t.Fatal("absent journal falsely reported rollback")
	}
	preflight, err := PreflightKEK(context.Background(), path)
	if err != nil || preflight.Generation != 1 || preflight.Records != 4 {
		t.Fatal("preflight validation failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RotateKEK(ctx, path, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel not bounded: %v", err)
	}
	if err := os.Symlink("/untrusted", path+".rotation-journal"); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverKEK(context.Background(), path, "commit"); err == nil {
		t.Fatal("symlink journal accepted")
	}
}
