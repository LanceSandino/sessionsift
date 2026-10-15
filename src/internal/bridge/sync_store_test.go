// Package bridge tests the isolated opaque sync object store and delta log.
package bridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestSyncStoreTransfersOpaqueObjectWithinWorkspace(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publicA, privateA, _ := ed25519.GenerateKey(rand.Reader)
	publicB, _, _ := ed25519.GenerateKey(rand.Reader)
	publicOther, _, _ := ed25519.GenerateKey(rand.Reader)
	deviceA := testOwnedDevice(t, store, "owner@example.com", "A", publicA)
	deviceB := testOwnedDeviceIn(t, store, "owner@example.com", deviceA.WorkspaceID, "B", publicB)
	other := testOwnedDevice(t, store, "other@example.com", "Other", publicOther)

	object := SyncObject{
		WorkspaceID: deviceA.WorkspaceID, ObjectID: "obj_opaque", SourceDeviceID: deviceA.ID, WriterDeviceID: deviceA.ID, Epoch: 1, Revision: 1,
		Kind: "session_summary", Nonce: []byte("opaque nonce"), Ciphertext: []byte("opaque ciphertext"),
	}
	signSyncObject(&object, privateA)
	change, err := store.PutSyncObject(deviceA.ID, object, 0)
	if err != nil || change.Cursor != 1 || change.WorkspaceID != deviceA.WorkspaceID {
		t.Fatalf("change=%+v err=%v", change, err)
	}
	changes, next, err := store.SyncChanges(deviceB.ID, 0, 10)
	if err != nil || next || len(changes) != 1 || changes[0].ObjectID != object.ObjectID {
		t.Fatalf("changes=%+v next=%v err=%v", changes, next, err)
	}
	got, err := store.GetSyncObject(deviceB.ID, object.ObjectID)
	if err != nil || string(got.Ciphertext) != "opaque ciphertext" {
		t.Fatalf("object=%+v err=%v", got, err)
	}
	if _, err = store.GetSyncObject(other.ID, object.ObjectID); !errors.Is(err, ErrSyncNotFound) {
		t.Fatalf("cross-tenant read err=%v", err)
	}
}

func TestSyncStoreCASIdempotencyQuotaAndTombstone(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "A", public)
	store.ConfigureSync(SyncLimits{MaxObjectBytes: 32, MaxWorkspaceBytes: 64, MaxPageSize: 20})
	object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: "obj_note", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Epoch: 1, Revision: 1, Kind: "note", Nonce: []byte("n"), Ciphertext: []byte("ciphertext")}
	signSyncObject(&object, private)
	first, err := store.PutSyncObject(device.ID, object, 0)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := store.PutSyncObject(device.ID, object, 0)
	if err != nil || retry.Cursor != first.Cursor {
		t.Fatalf("idempotent retry=%+v err=%v", retry, err)
	}
	stale := object
	stale.Revision = 2
	signSyncObject(&stale, private)
	if _, err = store.PutSyncObject(device.ID, stale, 0); !errors.Is(err, ErrSyncConflict) {
		t.Fatalf("stale CAS err=%v", err)
	}
	large := object
	large.ObjectID, large.Ciphertext = "obj_large", make([]byte, 33)
	signSyncObject(&large, private)
	if _, err = store.PutSyncObject(device.ID, large, 0); !errors.Is(err, ErrSyncQuota) {
		t.Fatalf("oversize err=%v", err)
	}
	deleted := object
	deleted.Revision, deleted.ParentDigest, deleted.Nonce, deleted.Ciphertext, deleted.Deleted = 2, object.Digest, nil, nil, true
	signSyncObject(&deleted, private)
	tombstone, err := store.DeleteSyncObject(device.ID, object.ObjectID, 1, 2, deleted.Digest, deleted.Signature)
	if err != nil || !tombstone.Deleted {
		t.Fatalf("tombstone=%+v err=%v", tombstone, err)
	}
	if _, err = store.PutSyncObject(device.ID, stale, 1); !errors.Is(err, ErrSyncConflict) {
		t.Fatalf("stale device resurrected tombstone: %v", err)
	}
}

func TestMutableObjectCanAdvanceFromSecondDeviceButImmutableSnapshotCannot(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	publicA, privateA, _ := ed25519.GenerateKey(rand.Reader)
	publicB, privateB, _ := ed25519.GenerateKey(rand.Reader)
	deviceA := testOwnedDevice(t, store, "owner@example.com", "A", publicA)
	deviceB := testOwnedDeviceIn(t, store, "owner@example.com", deviceA.WorkspaceID, "B", publicB)
	note := SyncObject{WorkspaceID: deviceA.WorkspaceID, ObjectID: "obj_shared_note", SourceDeviceID: deviceA.ID, WriterDeviceID: deviceA.ID, Kind: "note", Epoch: 1, Revision: 1, Nonce: []byte("n1"), Ciphertext: []byte("c1")}
	signSyncObject(&note, privateA)
	if _, err := store.PutSyncObject(deviceA.ID, note, 0); err != nil {
		t.Fatal(err)
	}
	remoteEdit := note
	remoteEdit.WriterDeviceID, remoteEdit.Revision, remoteEdit.ParentDigest = deviceB.ID, 2, note.Digest
	remoteEdit.Nonce, remoteEdit.Ciphertext = []byte("n2"), []byte("c2")
	signSyncObject(&remoteEdit, privateB)
	if _, err := store.PutSyncObject(deviceB.ID, remoteEdit, 1); err != nil {
		t.Fatalf("second enrolled device could not advance mutable note: %v", err)
	}

	immutable := remoteEdit
	immutable.ObjectID, immutable.Kind, immutable.Revision, immutable.ParentDigest = "obj_foreign_snapshot", "session_summary", 1, ""
	signSyncObject(&immutable, privateB)
	if _, err := store.PutSyncObject(deviceB.ID, immutable, 0); err == nil {
		t.Fatal("non-source device advanced immutable source snapshot")
	}
}

func TestCloudDeletionPurgesCiphertextAndRejectsStaleOfflineEpoch(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "A", public)
	object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: "obj_delete", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Kind: "note", Epoch: 1, Revision: 1, Nonce: []byte("n"), Ciphertext: []byte("c")}
	signSyncObject(&object, private)
	if _, err := store.PutSyncObject(device.ID, object, 0); err != nil {
		t.Fatal(err)
	}
	next, err := store.DeleteSyncWorkspace(device.ID)
	if err != nil || next <= 1 {
		t.Fatalf("next epoch=%d err=%v", next, err)
	}
	if _, err = store.GetSyncObject(device.ID, object.ObjectID); !errors.Is(err, ErrSyncNotFound) {
		t.Fatalf("ciphertext remained after deletion: %v", err)
	}
	if _, err = store.PutSyncObject(device.ID, object, 0); !errors.Is(err, ErrSyncConflict) {
		t.Fatalf("stale offline epoch resurrected deleted data: %v", err)
	}
	object.Epoch, object.Revision = next, 1
	signSyncObject(&object, private)
	if _, err = store.PutSyncObject(device.ID, object, 0); err != nil {
		t.Fatalf("new key epoch rejected after deletion: %v", err)
	}
}

func TestSyncStoreReportsUsagePagesAndRejectsMalformedObjects(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "A", public)
	store.ConfigureSync(SyncLimits{MaxObjectBytes: 128, MaxWorkspaceBytes: 512, MaxPageSize: 1})
	for index := 0; index < 2; index++ {
		object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: fmt.Sprintf("obj_%d", index), SourceDeviceID: device.ID, WriterDeviceID: device.ID, Epoch: 1, Revision: 1, Kind: "note", Nonce: []byte("n"), Ciphertext: []byte("ciphertext")}
		signSyncObject(&object, private)
		if _, err := store.PutSyncObject(device.ID, object, 0); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := store.SyncUsage(device.ID)
	if err != nil || usage.Objects != 2 || usage.Bytes == 0 || usage.Limit != 512 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	changes, more, err := store.SyncChanges(device.ID, 0, 0)
	if err != nil || len(changes) != 1 || !more {
		t.Fatalf("changes=%+v more=%v err=%v", changes, more, err)
	}
	if _, _, err = store.SyncChanges(device.ID, -1, 1); err == nil {
		t.Fatal("negative cursor accepted")
	}
	if _, err = store.SyncUsage("missing"); err == nil {
		t.Fatal("unlinked device received sync usage")
	}

	valid := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: "obj_valid", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Epoch: 1, Revision: 1, Kind: "note", Nonce: []byte("n"), Ciphertext: []byte("c")}
	for name, mutate := range map[string]func(*SyncObject){
		"missing id":         func(value *SyncObject) { value.ObjectID = "bad" },
		"wrong writer":       func(value *SyncObject) { value.WriterDeviceID = "other" },
		"unsupported kind":   func(value *SyncObject) { value.Kind = "raw_transcript" },
		"missing ciphertext": func(value *SyncObject) { value.Ciphertext = nil },
	} {
		t.Run(name, func(t *testing.T) {
			object := valid
			mutate(&object)
			signSyncObject(&object, private)
			if _, err := store.PutSyncObject(device.ID, object, 0); err == nil {
				t.Fatal("malformed object accepted")
			}
		})
	}
	unsigned := valid
	unsigned.Digest, unsigned.Signature = "wrong", make([]byte, ed25519.SignatureSize)
	if _, err = store.PutSyncObject(device.ID, unsigned, 0); err == nil {
		t.Fatal("invalid signature accepted")
	}
	if _, err = store.DeleteSyncObject(device.ID, "obj_missing", 0, 1, "digest", make([]byte, ed25519.SignatureSize)); !errors.Is(err, ErrSyncNotFound) {
		t.Fatalf("missing delete err=%v", err)
	}
}

func TestSyncStoreRefusesLowDiskWithoutDroppingExistingObjects(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "A", public)
	previous := syncAvailableBytes
	syncAvailableBytes = func(string) (uint64, error) { return 0, nil }
	t.Cleanup(func() { syncAvailableBytes = previous })
	object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: "obj_low_disk", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Epoch: 1, Revision: 1, Kind: "note", Nonce: []byte("n"), Ciphertext: []byte("c")}
	signSyncObject(&object, private)
	if _, err := store.PutSyncObject(device.ID, object, 0); !errors.Is(err, ErrSyncDisk) {
		t.Fatalf("low disk err=%v", err)
	}
	usage, err := store.SyncUsage(device.ID)
	if err != nil || usage.Objects != 0 || usage.Bytes != 0 {
		t.Fatalf("rejected write changed usage=%+v err=%v", usage, err)
	}
}

func signSyncObject(object *SyncObject, private ed25519.PrivateKey) {
	digest := digestSyncObject(*object)
	object.Digest = fmt.Sprintf("%x", digest)
	object.Signature = ed25519.Sign(private, digest[:])
}
