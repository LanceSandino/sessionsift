// Package cloudsync tests adversarial change-feed rollback on an enrolled device.
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

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type reviewFeed struct {
	change Change
	object WireObject
}

func (feed *reviewFeed) Put(context.Context, WireObject, uint64) (Change, error) {
	return Change{}, errors.New("review feed is read-only")
}

func (feed *reviewFeed) Changes(_ context.Context, cursor int64, _ int) ([]Change, bool, error) {
	if feed.change.Cursor > cursor {
		return []Change{feed.change}, false, nil
	}
	return nil, false, nil
}

func (feed *reviewFeed) Get(_ context.Context, _ string) (WireObject, error) {
	return feed.object, nil
}

// A03/Y13: a compromised bridge cannot silently label an old, valid record as
// a new change after this device has already authenticated a later revision.
func TestSeparateReviewRejectsOldSignedObjectAtNewCursor(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "replica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key, err := GenerateMasterKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first, firstChange := signedTestObject(t, key, private, json.RawMessage(`{"note":"first"}`), false)
	feed := &reviewFeed{change: firstChange, object: first}
	engine := Engine{Store: store, Transport: feed, MasterKey: key, WorkspaceID: "workspace", DeviceID: "reader",
		Verify: func(_ string, message, signature []byte) bool { return ed25519.Verify(public, message, signature) }}
	if err := engine.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Revision, second.ParentDigest = 2, first.Digest
	record, err := json.Marshal(Record{Schema: SchemaVersion, LocalID: "record", Kind: "note", Revision: 2, Payload: json.RawMessage(`{"note":"second"}`)})
	if err != nil {
		t.Fatal(err)
	}
	metadata := Metadata{Schema: SchemaVersion, WorkspaceID: second.WorkspaceID, ObjectID: second.ObjectID,
		SourceDeviceID: second.SourceDeviceID, WriterDeviceID: second.WriterDeviceID, Kind: second.Kind,
		Epoch: second.Epoch, Revision: second.Revision, ParentDigest: second.ParentDigest}
	envelope, err := Seal(key, metadata, record, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second.Nonce, _ = base64.RawURLEncoding.DecodeString(envelope.Nonce)
	second.Ciphertext, _ = base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	digest := digestWireObject(second)
	second.Digest = hex.EncodeToString(digest[:])
	second.Signature = ed25519.Sign(private, digest[:])
	feed.object = second
	feed.change = Change{Cursor: 2, ObjectID: second.ObjectID, SourceDeviceID: second.SourceDeviceID,
		WriterDeviceID: second.WriterDeviceID, Kind: second.Kind, Epoch: second.Epoch, Revision: second.Revision, Digest: second.Digest}
	if err := engine.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	feed.object = first
	feed.change = firstChange
	feed.change.Cursor = 3 // Server supplied a fresh cursor over old signed bytes.
	if err := engine.RunOnce(ctx); !errors.Is(err, storage.ErrSyncRollback) {
		t.Fatalf("old signed object at new cursor: expected rollback alarm, got %v", err)
	}
	if cursor, err := store.SyncCursor(ctx); err != nil || cursor != 2 {
		t.Fatalf("rollback advanced cursor: cursor=%d err=%v", cursor, err)
	}
}
