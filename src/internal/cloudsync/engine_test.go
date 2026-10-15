// Package cloudsync_test verifies offline queueing and two-device recovery through public contracts.
package cloudsync_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type memoryTransport struct {
	mu       sync.Mutex
	failPut  bool
	pageSize int
	objects  map[string]cloudsync.WireObject
	changes  []cloudsync.Change
}

func (m *memoryTransport) Put(_ context.Context, object cloudsync.WireObject, expected uint64) (cloudsync.Change, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPut {
		return cloudsync.Change{}, errors.New("offline")
	}
	if m.objects == nil {
		m.objects = map[string]cloudsync.WireObject{}
	}
	if current, ok := m.objects[object.ObjectID]; ok && current.Revision != expected {
		return cloudsync.Change{}, errors.New("CAS conflict")
	}
	m.objects[object.ObjectID] = object
	change := cloudsync.Change{Cursor: int64(len(m.changes) + 1), ObjectID: object.ObjectID, SourceDeviceID: object.SourceDeviceID, WriterDeviceID: object.WriterDeviceID, Kind: object.Kind, Epoch: object.Epoch, Revision: object.Revision, Digest: object.Digest, Deleted: object.Deleted}
	m.changes = append(m.changes, change)
	return change, nil
}

func (m *memoryTransport) Changes(_ context.Context, cursor int64, limit int) ([]cloudsync.Change, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pageSize > 0 && m.pageSize < limit {
		limit = m.pageSize
	}
	result := []cloudsync.Change{}
	more := false
	for _, change := range m.changes {
		if change.Cursor > cursor {
			if len(result) < limit {
				result = append(result, change)
			} else {
				more = true
				break
			}
		}
	}
	return result, more, nil
}

func (m *memoryTransport) Get(_ context.Context, objectID string) (cloudsync.WireObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[objectID]
	if !ok {
		return cloudsync.WireObject{}, errors.New("missing")
	}
	return object, nil
}

func TestEnginePersistsOfflineUploadAndRestoresOnSecondDevice(t *testing.T) {
	ctx := context.Background()
	transport := &memoryTransport{failPut: true}
	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	publicA, privateA, _ := ed25519.GenerateKey(rand.Reader)
	publicB, privateB, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]ed25519.PublicKey{"device_a": publicA, "device_b": publicB}
	now := time.Now().UTC().Add(time.Hour)
	storeA, _ := storage.Open(filepath.Join(t.TempDir(), "a.db"))
	defer storeA.Close()
	engineA := cloudsync.Engine{Store: storeA, Transport: transport, MasterKey: master, WorkspaceID: "workspace_owner", DeviceID: "device_a", Sign: func(value []byte) []byte { return ed25519.Sign(privateA, value) }, Verify: verifyWith(keys), Now: func() time.Time { return now }}
	record := cloudsync.Record{LocalID: "private-session-id", Kind: "session_summary", Revision: 1, Payload: json.RawMessage(`{"title":"RECOVERY-CANARY","source_label":"Synced snapshot from Device A"}`)}
	if err := engineA.Queue(ctx, record, 0); err != nil {
		t.Fatal(err)
	}
	if err := engineA.RunOnce(ctx); err == nil {
		t.Fatal("offline upload reported success")
	}
	if pending, _ := storeA.PendingSync(ctx, now.Add(time.Hour), 10); len(pending) != 1 || pending[0].Attempts != 1 {
		t.Fatalf("offline pending=%+v", pending)
	}
	transport.failPut = false
	now = now.Add(time.Hour)
	if err := engineA.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if pending, _ := storeA.PendingSync(ctx, now, 10); len(pending) != 0 {
		t.Fatalf("uploaded pending=%+v", pending)
	}

	storeB, _ := storage.Open(filepath.Join(t.TempDir(), "b.db"))
	defer storeB.Close()
	engineB := cloudsync.Engine{Store: storeB, Transport: transport, MasterKey: master, WorkspaceID: "workspace_owner", DeviceID: "device_b", Sign: func(value []byte) []byte { return ed25519.Sign(privateB, value) }, Verify: verifyWith(keys), Now: func() time.Time { return now }}
	if err := engineB.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	replicas, _ := storeB.ListSyncReplicas(ctx)
	if len(replicas) != 1 || string(replicas[0].PayloadJSON) != string(record.Payload) || !replicas[0].RemoteSource {
		t.Fatalf("restored replicas=%+v", replicas)
	}
	if cursor, _ := storeB.SyncCursor(ctx); cursor != 1 {
		t.Fatalf("cursor=%d", cursor)
	}
}

func TestEngineWrongRecoveryKeyFailsClosedBeforeCursorAdvance(t *testing.T) {
	ctx := context.Background()
	transport := &memoryTransport{}
	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	wrong, _ := cloudsync.GenerateMasterKey(rand.Reader)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	storeA, _ := storage.Open(filepath.Join(t.TempDir(), "a.db"))
	defer storeA.Close()
	engineA := cloudsync.Engine{Store: storeA, Transport: transport, MasterKey: master, WorkspaceID: "workspace", DeviceID: "device_a", Sign: func(value []byte) []byte { return ed25519.Sign(private, value) }, Verify: verifyWith(map[string]ed25519.PublicKey{"device_a": public})}
	if err := engineA.Queue(ctx, cloudsync.Record{LocalID: "one", Kind: "note", Revision: 1, Payload: json.RawMessage(`{"note":"secret"}`)}, 0); err != nil {
		t.Fatal(err)
	}
	if err := engineA.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	storeB, _ := storage.Open(filepath.Join(t.TempDir(), "b.db"))
	defer storeB.Close()
	engineB := cloudsync.Engine{Store: storeB, Transport: transport, MasterKey: wrong, WorkspaceID: "workspace", DeviceID: "device_b", Verify: verifyWith(map[string]ed25519.PublicKey{"device_a": public})}
	if err := engineB.RunOnce(ctx); err == nil {
		t.Fatal("wrong recovery key restored ciphertext")
	}
	if cursor, _ := storeB.SyncCursor(ctx); cursor != 0 {
		t.Fatalf("wrong-key cursor advanced to %d", cursor)
	}
	if replicas, _ := storeB.ListSyncReplicas(ctx); len(replicas) != 0 {
		t.Fatalf("wrong-key replicas=%+v", replicas)
	}
}

func TestEnginePullsEveryBoundedChangePage(t *testing.T) {
	ctx := context.Background()
	transport := &memoryTransport{pageSize: 1}
	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	storeA, _ := storage.Open(filepath.Join(t.TempDir(), "a.db"))
	defer storeA.Close()
	engineA := cloudsync.Engine{Store: storeA, Transport: transport, MasterKey: master, WorkspaceID: "workspace", DeviceID: "device_a", Sign: func(value []byte) []byte { return ed25519.Sign(private, value) }, Verify: verifyWith(map[string]ed25519.PublicKey{"device_a": public})}
	for _, id := range []string{"one", "two"} {
		record := cloudsync.Record{LocalID: id, Kind: "note", Revision: 1, Payload: json.RawMessage(`{"note":"page"}`)}
		if err := engineA.Queue(ctx, record, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := engineA.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	storeB, _ := storage.Open(filepath.Join(t.TempDir(), "b.db"))
	defer storeB.Close()
	engineB := cloudsync.Engine{Store: storeB, Transport: transport, MasterKey: master, WorkspaceID: "workspace", DeviceID: "device_b", Verify: verifyWith(map[string]ed25519.PublicKey{"device_a": public})}
	if err := engineB.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	replicas, _ := storeB.ListSyncReplicas(ctx)
	cursor, _ := storeB.SyncCursor(ctx)
	if len(replicas) != 2 || cursor != 2 {
		t.Fatalf("replicas=%d cursor=%d", len(replicas), cursor)
	}
}

func TestEngineQueuesMutableReplicaEditAndClearsItAfterRoundTrip(t *testing.T) {
	ctx := context.Background()
	transport := &memoryTransport{}
	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	publicA, privateA, _ := ed25519.GenerateKey(rand.Reader)
	publicB, privateB, _ := ed25519.GenerateKey(rand.Reader)
	keys := map[string]ed25519.PublicKey{"device_a": publicA, "device_b": publicB}
	storeA, _ := storage.Open(filepath.Join(t.TempDir(), "a.db"))
	defer storeA.Close()
	engineA := cloudsync.Engine{Store: storeA, Transport: transport, MasterKey: master, WorkspaceID: "workspace", DeviceID: "device_a", Sign: func(value []byte) []byte { return ed25519.Sign(privateA, value) }, Verify: verifyWith(keys)}
	if err := engineA.Queue(ctx, cloudsync.Record{LocalID: "shared-note", Kind: "note", Revision: 1, Payload: json.RawMessage(`{"note":"base"}`)}, 0); err != nil {
		t.Fatal(err)
	}
	if err := engineA.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	storeB, _ := storage.Open(filepath.Join(t.TempDir(), "b.db"))
	defer storeB.Close()
	engineB := cloudsync.Engine{Store: storeB, Transport: transport, MasterKey: master, WorkspaceID: "workspace", DeviceID: "device_b", Sign: func(value []byte) []byte { return ed25519.Sign(privateB, value) }, Verify: verifyWith(keys)}
	if err := engineB.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	replicas, _ := storeB.ListSyncReplicas(ctx)
	if err := storeB.EditSyncReplica(ctx, replicas[0].ObjectID, json.RawMessage(`{"note":"edited on B"}`)); err != nil {
		t.Fatal(err)
	}
	if queued, err := engineB.QueueReplicaEdits(ctx); err != nil || queued != 1 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	if err := engineB.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	replicas, _ = storeB.ListSyncReplicas(ctx)
	if len(replicas) != 1 || replicas[0].Revision != 2 || replicas[0].LocallyEdited || string(replicas[0].PayloadJSON) != `{"note":"edited on B"}` {
		t.Fatalf("round trip replica=%+v", replicas)
	}
}

func verifyWith(keys map[string]ed25519.PublicKey) func(string, []byte, []byte) bool {
	return func(deviceID string, message, signature []byte) bool {
		return ed25519.Verify(keys[deviceID], message, signature)
	}
}
