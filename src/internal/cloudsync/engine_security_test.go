// Package cloudsync anchors fail-closed behavior for malformed and tampered sync records.
package cloudsync

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type failureTransport struct {
	changes []Change
	more    bool
	err     error
	object  WireObject
}

func (f failureTransport) Put(context.Context, WireObject, uint64) (Change, error) {
	return Change{}, f.err
}
func (f failureTransport) Changes(context.Context, int64, int) ([]Change, bool, error) {
	return f.changes, f.more, f.err
}
func (f failureTransport) Get(context.Context, string) (WireObject, error) {
	return f.object, f.err
}
func (f failureTransport) DeviceKey(context.Context, string) (ed25519.PublicKey, error) {
	return nil, errors.New("device key unavailable")
}

func TestEngineRejectsInvalidQueueAndChangeInputs(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.Open(filepath.Join(t.TempDir(), "local.db"))
	defer store.Close()
	key, _ := GenerateMasterKey(rand.Reader)
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	configured := Engine{Store: store, Transport: failureTransport{}, MasterKey: key, WorkspaceID: "workspace", DeviceID: "device", Sign: func(value []byte) []byte { return ed25519.Sign(private, value) }}
	if err := (&Engine{}).Queue(ctx, Record{}, 0); err == nil {
		t.Fatal("unconfigured queue accepted")
	}
	for _, record := range []Record{
		{LocalID: "", Kind: "note", Revision: 1, Payload: json.RawMessage(`{}`)},
		{Schema: 2, LocalID: "one", Kind: "note", Revision: 1, Payload: json.RawMessage(`{}`)},
		{LocalID: "one", Kind: "session_summary", Revision: 1, SourceDeviceID: "other", Payload: json.RawMessage(`{}`)},
	} {
		if err := configured.Queue(ctx, record, 0); err == nil {
			t.Fatalf("invalid record accepted: %+v", record)
		}
	}
	if err := (&Engine{}).RunOnce(ctx); err == nil {
		t.Fatal("unconfigured run accepted")
	}
	if _, err := (&Engine{}).QueueReplicaEdits(ctx); err == nil {
		t.Fatal("unconfigured replica edit queue accepted")
	}
	if queued, err := configured.QueueReplicaEdits(ctx); err != nil || queued != 0 {
		t.Fatalf("clean replica queue=%d err=%v", queued, err)
	}
	if err := store.EnqueueSync(ctx, storage.SyncOutboxItem{ObjectID: "obj_broken", Revision: 1, EnvelopeJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	configured.Transport = failureTransport{err: errors.New("offline")}
	if err := configured.RunOnce(ctx); err == nil {
		t.Fatal("transport failure reported success")
	}
}

func TestEngineRejectsTamperingAndAppliesSignedTombstone(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.Open(filepath.Join(t.TempDir(), "local.db"))
	defer store.Close()
	key, _ := GenerateMasterKey(rand.Reader)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	engine := Engine{Store: store, Transport: failureTransport{}, MasterKey: key, WorkspaceID: "workspace", DeviceID: "reader", Verify: func(_ string, message, signature []byte) bool { return ed25519.Verify(public, message, signature) }}
	object, change := signedTestObject(t, key, private, json.RawMessage(`{"note":"value"}`), false)

	mismatch := change
	mismatch.Kind = "preference"
	if err := engine.applyObject(ctx, mismatch, object); err == nil {
		t.Fatal("change/object mismatch accepted")
	}
	brokenDigest := object
	brokenDigest.Digest = "00"
	change.Digest = "00"
	if err := engine.applyObject(ctx, change, brokenDigest); err == nil {
		t.Fatal("object digest mismatch accepted")
	}
	change.Digest = object.Digest
	badSignature := object
	badSignature.Signature = make([]byte, ed25519.SignatureSize)
	if err := engine.applyObject(ctx, change, badSignature); err == nil {
		t.Fatal("invalid writer signature accepted")
	}
	resolverEngine := engine
	resolverEngine.Verify = nil
	if err := resolverEngine.applyObject(ctx, change, object); err == nil {
		t.Fatal("missing writer key accepted")
	}

	tombstone, tombstoneChange := signedTestObject(t, key, private, json.RawMessage(`{}`), true)
	if err := engine.applyObject(ctx, tombstoneChange, tombstone); err != nil {
		t.Fatal(err)
	}
	replicas, _ := store.ListSyncReplicas(ctx)
	if len(replicas) != 1 || !replicas[0].Deleted {
		t.Fatalf("tombstone replica=%+v", replicas)
	}

	invalid, invalidChange := signedTestObject(t, key, private, nil, false)
	if err := engine.applyObject(ctx, invalidChange, invalid); err == nil {
		t.Fatal("invalid decrypted portable record accepted")
	}
}

func TestEngineBoundsChangePagesAndBackoff(t *testing.T) {
	store, _ := storage.Open(filepath.Join(t.TempDir(), "local.db"))
	defer store.Close()
	key, _ := GenerateMasterKey(rand.Reader)
	engine := Engine{Store: store, Transport: failureTransport{more: true}, MasterKey: key, WorkspaceID: "workspace", DeviceID: "device"}
	if err := engine.RunOnce(context.Background()); err == nil {
		t.Fatal("unbounded change feed accepted")
	}
	if syncBackoff("obj", 0) < 2*time.Second || syncBackoff("obj", 100) > 15*time.Minute {
		t.Fatal("retry backoff escaped its bounds")
	}
	rollback := Engine{Store: store, Transport: failureTransport{changes: []Change{{Cursor: 0}}}, MasterKey: key, WorkspaceID: "workspace", DeviceID: "device"}
	if err := rollback.RunOnce(context.Background()); !errors.Is(err, storage.ErrSyncRollback) {
		t.Fatalf("rollback err=%v", err)
	}
}

func signedTestObject(t *testing.T, key []byte, private ed25519.PrivateKey, payload json.RawMessage, deleted bool) (WireObject, Change) {
	t.Helper()
	objectID := ObjectID(key, "note", "record")
	object := WireObject{WorkspaceID: "workspace", ObjectID: objectID, SourceDeviceID: "source", WriterDeviceID: "writer", Kind: "note", Epoch: 1, Revision: 1, Deleted: deleted}
	{
		record := []byte(`{}`)
		if payload != nil {
			record, _ = json.Marshal(Record{Schema: SchemaVersion, LocalID: "record", Kind: "note", Revision: 1, Payload: payload, Deleted: deleted})
		}
		envelope, err := Seal(key, Metadata{Schema: SchemaVersion, WorkspaceID: "workspace", ObjectID: objectID, SourceDeviceID: "source", WriterDeviceID: "writer", Kind: "note", Epoch: 1, Revision: 1, Deleted: deleted}, record, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		object.Nonce, _ = base64.RawURLEncoding.DecodeString(envelope.Nonce)
		object.Ciphertext, _ = base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	}
	digest := digestWireObject(object)
	object.Digest = hex.EncodeToString(digest[:])
	object.Signature = ed25519.Sign(private, digest[:])
	return object, Change{Cursor: 1, ObjectID: object.ObjectID, SourceDeviceID: object.SourceDeviceID, WriterDeviceID: object.WriterDeviceID, Kind: object.Kind, Epoch: object.Epoch, Revision: object.Revision, Digest: object.Digest, Deleted: object.Deleted}
}
