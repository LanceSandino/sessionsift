// @module SyncAssuranceTests challenges epoch, manifest, AEAD and recovery trust boundaries (A01-A03).
package cloudsync

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestA03RejectsOldEpochAndManifestDeletionMismatch(t *testing.T) {
	key, _ := GenerateMasterKey(nil)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	for _, attack := range []string{"old-epoch-tombstone", "manifest-deletion-mismatch"} {
		t.Run(attack, func(t *testing.T) {
			store, err := storage.Open(filepath.Join(t.TempDir(), "local.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			object, change := signedTestObject(t, key, private, json.RawMessage(`{"note":"synthetic"}`), attack == "old-epoch-tombstone")
			epoch := uint64(1)
			if attack == "old-epoch-tombstone" {
				epoch = 2
			} else {
				change.Deleted = true
			}
			engine := Engine{Store: store, MasterKey: key, WorkspaceID: "workspace", Epoch: epoch, Verify: func(_ string, m, s []byte) bool { return ed25519.Verify(public, m, s) }}
			if err = engine.applyObject(context.Background(), change, object); err == nil {
				t.Fatal("adversarial signed object accepted")
			}
			replicas, err := store.ListSyncReplicas(context.Background())
			if err != nil || len(replicas) != 0 {
				t.Fatalf("failed attack changed replicas: %v %v", replicas, err)
			}
		})
	}
}

func TestA01AEADPropertiesAndServerKeyCannotDecrypt(t *testing.T) {
	key, _ := GenerateMasterKey(nil)
	serverKEK, _ := GenerateMasterKey(nil)
	metadata := Metadata{Schema: 1, WorkspaceID: "workspace", ObjectID: ObjectID(key, "note", "synthetic-id"), SourceDeviceID: "source", WriterDeviceID: "writer", Kind: "note", Epoch: 1, Revision: 1}
	canary := []byte(`{"title":"synthetic-title","path":"/synthetic/path","note":"synthetic-secret"}`)
	seen := map[string]bool{}
	for i := 0; i < 256; i++ {
		envelope, err := Seal(key, metadata, canary, nil)
		if err != nil {
			t.Fatal(err)
		}
		if seen[envelope.Nonce] {
			t.Fatal("nonce reused")
		}
		seen[envelope.Nonce] = true
		wire, _ := json.Marshal(envelope)
		for _, marker := range [][]byte{[]byte("synthetic-title"), []byte("/synthetic/path"), []byte(EncodeRecoverySecret(key))} {
			if bytes.Contains(wire, marker) {
				t.Fatal("plaintext in wire")
			}
		}
		if _, err = Open(serverKEK, envelope); err == nil {
			t.Fatal("server KEK decrypted sync")
		}
		opened, err := Open(key, envelope)
		if err != nil || !bytes.Equal(opened, canary) {
			t.Fatal("positive control failed")
		}
		altered := envelope
		altered.WorkspaceID = "other"
		if _, err = Open(key, altered); err == nil {
			t.Fatal("alternate workspace accepted")
		}
		altered = envelope
		altered.Kind = "preference"
		if _, err = Open(key, altered); err == nil {
			t.Fatal("domain substitution accepted")
		}
	}
}

func FuzzA01AEADCorruption(f *testing.F) {
	f.Add([]byte{0}, uint16(0))
	f.Add([]byte{255, 7}, uint16(24))
	f.Fuzz(func(t *testing.T, data []byte, position uint16) {
		if len(data) > 4096 {
			return
		}
		key := bytes.Repeat([]byte{42}, 32)
		metadata := Metadata{Schema: 1, WorkspaceID: "workspace", ObjectID: "obj_fuzz", SourceDeviceID: "source", WriterDeviceID: "writer", Kind: "note", Epoch: 1, Revision: 1}
		envelope, err := Seal(key, metadata, data, nil)
		if err != nil {
			t.Fatal(err)
		}
		cipher, _ := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
		cipher[int(position)%len(cipher)] ^= 1
		envelope.Ciphertext = base64.RawURLEncoding.EncodeToString(cipher)
		if _, err = Open(key, envelope); err == nil {
			t.Fatal("corruption authenticated")
		}
	})
}

// Test-only signing supports malicious synthetic object construction, never application keys.
func resignAssuranceObject(object *WireObject, private ed25519.PrivateKey) {
	digest := digestWireObject(*object)
	object.Digest = hex.EncodeToString(digest[:])
	object.Signature = ed25519.Sign(private, digest[:])
}

type substitutedKeyTransport struct {
	failureTransport
	key ed25519.PublicKey
}

func (s substitutedKeyTransport) DeviceKey(context.Context, string) (ed25519.PublicKey, error) {
	return s.key, nil
}

func TestA06BridgeKeySubstitutionCannotForgeTombstone(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	master, _ := GenerateMasterKey(nil)
	attackerPublic, attackerPrivate, _ := ed25519.GenerateKey(rand.Reader)
	original := storage.SyncReplica{ObjectID: "obj_victim", SourceDeviceID: "legitimate", Kind: "note", Epoch: 1, Revision: 1, Digest: "trusted", PayloadJSON: json.RawMessage(`{"note":"original"}`)}
	if _, err = store.ApplySyncReplica(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	object := WireObject{WorkspaceID: "workspace", ObjectID: original.ObjectID, SourceDeviceID: "legitimate", WriterDeviceID: "legitimate", Kind: "note", Epoch: 1, Revision: 2, ParentDigest: "trusted", Deleted: true}
	resignAssuranceObject(&object, attackerPrivate)
	change := Change{Cursor: 2, ObjectID: object.ObjectID, SourceDeviceID: object.SourceDeviceID, WriterDeviceID: object.WriterDeviceID, Kind: object.Kind, Epoch: 1, Revision: 2, Digest: object.Digest, Deleted: true}
	engine := Engine{Store: store, Transport: substitutedKeyTransport{key: attackerPublic}, MasterKey: master, WorkspaceID: "workspace", Epoch: 1}
	if err = engine.applyObject(context.Background(), change, object); err == nil {
		t.Fatal("bridge-forged key authorized unauthenticated deletion without master key")
	}
	replicas, err := store.ListSyncReplicas(context.Background())
	if err != nil || len(replicas) != 1 || replicas[0].Deleted || replicas[0].Revision != 1 {
		t.Fatal("forged tombstone changed original replica")
	}
}
