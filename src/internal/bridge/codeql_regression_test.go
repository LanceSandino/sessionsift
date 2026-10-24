// Package bridge regression tests for the 2.1.1 code-scanning review.
package bridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"math"
	"path/filepath"
	"testing"
)

// A sync page request must stay bounded even when an operator configures an
// absurd page size: the requested limit sizes an allocation and a SQL LIMIT.
func TestSyncChangesPageSizeHasHardCeiling(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "A", public)
	store.ConfigureSync(SyncLimits{MaxPageSize: math.MaxInt})
	if store.syncLimits.MaxPageSize != MaxSyncPageSize {
		t.Fatalf("configured page size = %d, want ceiling %d", store.syncLimits.MaxPageSize, MaxSyncPageSize)
	}
	object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: "obj_page", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Epoch: 1, Revision: 1, Kind: "note", Nonce: []byte("n"), Ciphertext: []byte("c")}
	signSyncObject(&object, private)
	if _, err = store.PutSyncObject(device.ID, object, 0); err != nil {
		t.Fatal(err)
	}
	// Before the ceiling this panicked in makeslice (cap out of range) and
	// limit+1 overflowed into an unbounded SQL LIMIT.
	changes, more, err := store.SyncChanges(device.ID, 0, math.MaxInt)
	if err != nil || more || len(changes) != 1 || cap(changes) > MaxSyncPageSize {
		t.Fatalf("changes=%d cap=%d more=%v err=%v", len(changes), cap(changes), more, err)
	}
	// A bounded store also clamps a direct caller that bypasses ConfigureSync.
	store.syncLimits.MaxPageSize = math.MaxInt
	if changes, _, err = store.SyncChanges(device.ID, 0, math.MaxInt); err != nil || len(changes) != 1 || cap(changes) > MaxSyncPageSize {
		t.Fatalf("unconfigured clamp changes=%d cap=%d err=%v", len(changes), cap(changes), err)
	}
}
