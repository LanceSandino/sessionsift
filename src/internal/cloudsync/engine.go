// @module SyncEngine coordinates encrypted local outbox delivery and monotonic remote replica application.
package cloudsync

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type Record struct {
	Schema         int             `json:"schema"`
	LocalID        string          `json:"local_id"`
	ObjectID       string          `json:"-"`
	Kind           string          `json:"kind"`
	Revision       uint64          `json:"revision"`
	ParentDigest   string          `json:"parent_digest,omitempty"`
	SourceDeviceID string          `json:"source_device_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Deleted        bool            `json:"deleted,omitempty"`
}

type WireObject struct {
	WorkspaceID    string `json:"workspace_id,omitempty"`
	ObjectID       string `json:"object_id"`
	SourceDeviceID string `json:"source_device_id"`
	WriterDeviceID string `json:"writer_device_id"`
	Kind           string `json:"kind"`
	Epoch          uint64 `json:"epoch"`
	Revision       uint64 `json:"revision"`
	ParentDigest   string `json:"parent_digest,omitempty"`
	Nonce          []byte `json:"nonce,omitempty"`
	Ciphertext     []byte `json:"ciphertext,omitempty"`
	Digest         string `json:"digest"`
	Signature      []byte `json:"signature"`
	Deleted        bool   `json:"deleted,omitempty"`
}

type Change struct {
	Cursor         int64  `json:"cursor"`
	ObjectID       string `json:"object_id"`
	SourceDeviceID string `json:"source_device_id"`
	WriterDeviceID string `json:"writer_device_id"`
	Kind           string `json:"kind"`
	Epoch          uint64 `json:"epoch"`
	Revision       uint64 `json:"revision"`
	Digest         string `json:"digest"`
	Deleted        bool   `json:"deleted"`
}

type Transport interface {
	Put(context.Context, WireObject, uint64) (Change, error)
	Changes(context.Context, int64, int) ([]Change, bool, error)
	Get(context.Context, string) (WireObject, error)
}

type deviceKeyTransport interface {
	DeviceKey(context.Context, string) (ed25519.PublicKey, error)
}

type Engine struct {
	Store       *storage.Store
	Transport   Transport
	MasterKey   []byte
	WorkspaceID string
	DeviceID    string
	Epoch       uint64
	Sign        func([]byte) []byte
	Verify      func(deviceID string, message, signature []byte) bool
	Now         func() time.Time
}

func (e *Engine) Queue(ctx context.Context, record Record, expectedRevision uint64) error {
	if e.Store == nil || e.Transport == nil || len(e.MasterKey) != MasterKeySize || e.WorkspaceID == "" || e.DeviceID == "" || e.Sign == nil {
		return errors.New("sync engine is not configured")
	}
	if (record.LocalID == "" && record.ObjectID == "") || record.Kind == "" || record.Revision == 0 || !json.Valid(record.Payload) {
		return errors.New("invalid portable sync record")
	}
	if record.Schema == 0 {
		record.Schema = SchemaVersion
	}
	if record.Schema != SchemaVersion {
		return errors.New("unsupported portable sync record schema")
	}
	epoch := e.Epoch
	if epoch == 0 {
		epoch = 1
	}
	objectID := record.ObjectID
	if objectID == "" {
		objectID = ObjectID(e.MasterKey, record.Kind, record.LocalID)
	} else if len(objectID) < 5 || len(objectID) > 160 || objectID[:4] != "obj_" {
		return errors.New("invalid opaque sync object id")
	}
	sourceDeviceID := record.SourceDeviceID
	if sourceDeviceID == "" {
		sourceDeviceID = e.DeviceID
	}
	if immutableSyncKind(record.Kind) && sourceDeviceID != e.DeviceID {
		return errors.New("only the source device can advance immutable snapshots")
	}
	metadata := Metadata{Schema: SchemaVersion, WorkspaceID: e.WorkspaceID, ObjectID: objectID, SourceDeviceID: sourceDeviceID, WriterDeviceID: e.DeviceID, Kind: record.Kind, Epoch: epoch, Revision: record.Revision, ParentDigest: record.ParentDigest, Deleted: record.Deleted}
	plaintext, _ := json.Marshal(record)
	envelope, err := Seal(e.MasterKey, metadata, plaintext, nil)
	if err != nil {
		return err
	}
	nonce, _ := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	ciphertext, _ := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	object := WireObject{WorkspaceID: e.WorkspaceID, ObjectID: objectID, SourceDeviceID: sourceDeviceID, WriterDeviceID: e.DeviceID, Kind: record.Kind, Epoch: epoch, Revision: record.Revision, ParentDigest: record.ParentDigest, Nonce: nonce, Ciphertext: ciphertext, Deleted: record.Deleted}
	digest := digestWireObject(object)
	object.Digest = hex.EncodeToString(digest[:])
	object.Signature = e.Sign(digest[:])
	encoded, _ := json.Marshal(object)
	return e.Store.EnqueueSync(ctx, storage.SyncOutboxItem{ObjectID: objectID, Revision: record.Revision, ExpectedRevision: expectedRevision, EnvelopeJSON: encoded})
}

// QueueReplicaEdits turns locally modified mutable replicas into signed CAS
// revisions. Tombstones and immutable source snapshots are never resurrected.
func (e *Engine) QueueReplicaEdits(ctx context.Context) (int, error) {
	if e.Store == nil {
		return 0, errors.New("sync engine is not configured")
	}
	replicas, err := e.Store.ListSyncReplicas(ctx)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, replica := range replicas {
		if !replica.LocallyEdited || replica.Deleted || immutableSyncKind(replica.Kind) {
			continue
		}
		record := Record{Schema: SchemaVersion, ObjectID: replica.ObjectID, Kind: replica.Kind, Revision: replica.Revision + 1, ParentDigest: replica.Digest, SourceDeviceID: replica.SourceDeviceID, Payload: replica.PayloadJSON}
		if err = e.Queue(ctx, record, replica.Revision); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func (e *Engine) RunOnce(ctx context.Context) error {
	if e.Store == nil || e.Transport == nil || len(e.MasterKey) != MasterKeySize || e.WorkspaceID == "" || e.DeviceID == "" {
		return errors.New("sync engine is not configured")
	}
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	var firstErr error
	pending, err := e.Store.PendingSync(ctx, now, 100)
	if err != nil {
		return err
	}
	for _, item := range pending {
		var object WireObject
		if err = json.Unmarshal(item.EnvelopeJSON, &object); err != nil {
			return fmt.Errorf("decode encrypted outbox item: %w", err)
		}
		change, putErr := e.Transport.Put(ctx, object, item.ExpectedRevision)
		if putErr != nil {
			err = putErr
			next := now.Add(syncBackoff(item.ObjectID, item.Attempts+1))
			if deferErr := e.Store.DeferSync(ctx, item.ObjectID, item.Revision, next, err.Error()); deferErr != nil {
				return deferErr
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err = e.Store.RecordSyncHead(ctx, item.ObjectID, item.Revision, change.Digest); err != nil {
			return err
		}
		if err = e.Store.CompleteSync(ctx, item.ObjectID, item.Revision); err != nil {
			return err
		}
	}
	if err = e.pull(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (e *Engine) pull(ctx context.Context) error {
	cursor, err := e.Store.SyncCursor(ctx)
	if err != nil {
		return err
	}
	for page := 0; page < 100; page++ {
		changes, more, err := e.Transport.Changes(ctx, cursor, 100)
		if err != nil {
			return err
		}
		for _, change := range changes {
			if change.Cursor <= cursor {
				return storage.ErrSyncRollback
			}
			object, err := e.Transport.Get(ctx, change.ObjectID)
			if err != nil {
				return err
			}
			if err = e.applyObject(ctx, change, object); err != nil {
				return err
			}
			cursor = change.Cursor
			if err = e.Store.SetSyncCursor(ctx, cursor); err != nil {
				return err
			}
		}
		if !more {
			return nil
		}
	}
	return errors.New("sync change feed exceeded page bound")
}

func (e *Engine) applyObject(ctx context.Context, change Change, object WireObject) error {
	if object.ObjectID != change.ObjectID || object.SourceDeviceID != change.SourceDeviceID || object.WriterDeviceID != change.WriterDeviceID || object.Kind != change.Kind || object.Epoch != change.Epoch || object.Revision != change.Revision || object.Digest != change.Digest || object.Deleted != change.Deleted || object.WorkspaceID != e.WorkspaceID {
		return errors.New("sync change/object mismatch")
	}
	epoch := e.Epoch
	if epoch == 0 {
		epoch = 1
	}
	if object.Epoch != epoch {
		return errors.New("sync object key epoch mismatch; recover the current key before syncing")
	}
	digest := digestWireObject(object)
	if hex.EncodeToString(digest[:]) != object.Digest {
		return errors.New("sync object digest mismatch")
	}
	verified := false
	if e.Verify != nil {
		verified = e.Verify(object.WriterDeviceID, digest[:], object.Signature)
	} else if resolver, ok := e.Transport.(deviceKeyTransport); ok {
		key, keyErr := resolver.DeviceKey(ctx, object.WriterDeviceID)
		if keyErr != nil {
			return keyErr
		}
		verified = ed25519.Verify(key, digest[:], object.Signature)
	}
	if !verified {
		return errors.New("sync object device signature invalid")
	}
	// Server-resolved device keys are not an E2EE integrity root. Even tombstones
	// must prove possession of the user-held master key before mutating replicas.
	// Legacy signature-only tombstones fail closed instead of trusting a bridge
	// which can substitute the public key returned by its own key endpoint.
	metadata := Metadata{Schema: SchemaVersion, WorkspaceID: e.WorkspaceID, ObjectID: object.ObjectID, SourceDeviceID: object.SourceDeviceID, WriterDeviceID: object.WriterDeviceID, Kind: object.Kind, Epoch: object.Epoch, Revision: object.Revision, ParentDigest: object.ParentDigest, Deleted: object.Deleted}
	plaintext, err := Open(e.MasterKey, Envelope{Metadata: metadata, Nonce: base64.RawURLEncoding.EncodeToString(object.Nonce), Ciphertext: base64.RawURLEncoding.EncodeToString(object.Ciphertext)})
	if err != nil {
		return err
	}
	var record Record
	if err = json.Unmarshal(plaintext, &record); err != nil || record.Schema != SchemaVersion || record.Kind != object.Kind || record.Revision != object.Revision || record.Deleted != object.Deleted || !json.Valid(record.Payload) {
		return errors.New("invalid decrypted sync record")
	}
	if object.Deleted {
		record.Payload = json.RawMessage(`{}`)
	}
	_, err = e.Store.ApplySyncReplica(ctx, storage.SyncReplica{ObjectID: object.ObjectID, SourceDeviceID: object.SourceDeviceID, Kind: object.Kind, Epoch: object.Epoch, Revision: object.Revision, Digest: object.Digest, PayloadJSON: record.Payload, Deleted: object.Deleted})
	return err
}

func digestWireObject(object WireObject) [32]byte {
	value := struct {
		WorkspaceID    string `json:"workspace_id"`
		ObjectID       string `json:"object_id"`
		SourceDeviceID string `json:"source_device_id"`
		WriterDeviceID string `json:"writer_device_id"`
		Kind           string `json:"kind"`
		Epoch          uint64 `json:"epoch"`
		Revision       uint64 `json:"revision"`
		ParentDigest   string `json:"parent_digest,omitempty"`
		Nonce          []byte `json:"nonce,omitempty"`
		Ciphertext     []byte `json:"ciphertext,omitempty"`
		Deleted        bool   `json:"deleted,omitempty"`
	}{object.WorkspaceID, object.ObjectID, object.SourceDeviceID, object.WriterDeviceID, object.Kind, object.Epoch, object.Revision, object.ParentDigest, object.Nonce, object.Ciphertext, object.Deleted}
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}

func immutableSyncKind(kind string) bool {
	switch kind {
	case "session_summary", "finding", "evidence":
		return true
	default:
		return false
	}
}

func syncBackoff(objectID string, attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	shift := attempts - 1
	if shift > 8 {
		shift = 8
	}
	base := time.Duration(1<<shift) * 2 * time.Second
	digest := sha256.Sum256([]byte(objectID))
	jitter := time.Duration(int(digest[0])%500) * base / 1000
	delay := base + jitter
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}
