// @module BridgeAssuranceTests challenges offline latest-head delta recovery (A07).
package bridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
)

func TestA07LatestChangeFeedRecoversOfflineRevisions(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "synthetic", public)
	object := SyncObject{WorkspaceID: device.WorkspaceID, ObjectID: "obj_offline", SourceDeviceID: device.ID, WriterDeviceID: device.ID, Kind: "note", Epoch: 1, Revision: 1, Nonce: []byte("nonce"), Ciphertext: []byte("ciphertext")}
	signSyncObject(&object, private)
	if _, err = store.PutSyncObject(device.ID, object, 0); err != nil {
		t.Fatal(err)
	}
	object.Revision = 2
	object.ParentDigest = object.Digest
	object.Ciphertext = []byte("latest")
	signSyncObject(&object, private)
	if _, err = store.PutSyncObject(device.ID, object, 1); err != nil {
		t.Fatal(err)
	}
	changes, more, err := store.SyncChanges(device.ID, 0, 1)
	if err != nil || more || len(changes) != 1 || changes[0].Revision != 2 || changes[0].Digest != object.Digest {
		t.Fatalf("offline feed did not select fetchable head: %+v more=%v err=%v", changes, more, err)
	}
}
