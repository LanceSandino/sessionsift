// Package storage tests durable offline sync state without touching source imports.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncOutboxPersistsBackoffAndCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessionsift.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	item := SyncOutboxItem{ObjectID: "obj_one", Revision: 1, ExpectedRevision: 0, EnvelopeJSON: []byte(`{"ciphertext":"opaque"}`)}
	if err = store.EnqueueSync(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err = store.DeferSync(context.Background(), "obj_one", 1, now.Add(time.Minute), "offline"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if items, err := store.PendingSync(context.Background(), now, 10); err != nil || len(items) != 0 {
		t.Fatalf("early pending=%+v err=%v", items, err)
	}
	items, err := store.PendingSync(context.Background(), now.Add(2*time.Minute), 10)
	if err != nil || len(items) != 1 || items[0].Attempts != 1 || items[0].LastError != "offline" {
		t.Fatalf("pending=%+v err=%v", items, err)
	}
	if err = store.CompleteSync(context.Background(), "obj_one", 1); err != nil {
		t.Fatal(err)
	}
	if items, _ = store.PendingSync(context.Background(), now.Add(3*time.Minute), 10); len(items) != 0 {
		t.Fatalf("completed item remained: %+v", items)
	}
}

func TestSyncReplicaIsSeparateFromSourceAndConflictsAreDurable(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	defer store.Close()
	ctx := context.Background()
	payload := json.RawMessage(`{"note":"base","color":"blue"}`)
	result, err := store.ApplySyncReplica(ctx, SyncReplica{ObjectID: "obj", SourceDeviceID: "device_a", Kind: "session_summary", Epoch: 1, Revision: 1, Digest: "d1", PayloadJSON: payload})
	if err != nil || result.Conflict {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	replicas, err := store.ListSyncReplicas(ctx)
	if err != nil || len(replicas) != 1 || !replicas[0].RemoteSource {
		t.Fatalf("replicas=%+v err=%v", replicas, err)
	}
	if _, err = store.GetSession(ctx, "obj"); err == nil {
		t.Fatal("synced replica overwrote or became a source import")
	}
	if err = store.EditSyncReplica(ctx, "obj", json.RawMessage(`{"note":"local edit","color":"blue"}`)); err != nil {
		t.Fatal(err)
	}
	conflicted, err := store.ApplySyncReplica(ctx, SyncReplica{ObjectID: "obj", SourceDeviceID: "device_a", Kind: "note", Epoch: 1, Revision: 2, Digest: "d2", PayloadJSON: json.RawMessage(`{"note":"remote edit","color":"blue"}`)})
	if err != nil || !conflicted.Conflict {
		t.Fatalf("conflict=%+v err=%v", conflicted, err)
	}
	conflicts, err := store.SyncConflicts(ctx)
	if err != nil || len(conflicts) != 1 || conflicts[0].ObjectID != "obj" {
		t.Fatalf("conflicts=%+v err=%v", conflicts, err)
	}
	replicas, _ = store.ListSyncReplicas(ctx)
	if replicas[0].Revision != 2 || replicas[0].Digest != "d2" || string(replicas[0].PayloadJSON) != `{"note":"local edit","color":"blue"}` {
		t.Fatalf("conflict did not preserve local payload on the remote head: %+v", replicas[0])
	}
	if err = store.ResolveSyncConflict(ctx, conflicts[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if conflicts, _ = store.SyncConflicts(ctx); len(conflicts) != 0 {
		t.Fatalf("resolved conflict remained: %+v", conflicts)
	}
	replicas, _ = store.ListSyncReplicas(ctx)
	if len(replicas) != 1 || string(replicas[0].PayloadJSON) != `{"note":"remote edit","color":"blue"}` || replicas[0].LocallyEdited {
		t.Fatalf("remote resolution was not applied: %+v", replicas)
	}
}

func TestSyncReplicaNeverLetsAStaleRevisionOverwriteALocalEdit(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	defer store.Close()
	ctx := context.Background()
	replica := SyncReplica{ObjectID: "obj_stale", SourceDeviceID: "device_a", Kind: "note", Epoch: 1, Revision: 2, Digest: "d2", PayloadJSON: json.RawMessage(`{"note":"base"}`)}
	if _, err := store.ApplySyncReplica(ctx, replica); err != nil {
		t.Fatal(err)
	}
	if err := store.EditSyncReplica(ctx, replica.ObjectID, json.RawMessage(`{"note":"local"}`)); err != nil {
		t.Fatal(err)
	}
	replica.PayloadJSON = json.RawMessage(`{"note":"stale"}`)
	if result, err := store.ApplySyncReplica(ctx, replica); err != nil || result.Conflict {
		t.Fatalf("stale apply=%+v err=%v", result, err)
	}
	values, _ := store.ListSyncReplicas(ctx)
	if len(values) != 1 || string(values[0].PayloadJSON) != `{"note":"local"}` || !values[0].LocallyEdited {
		t.Fatalf("stale revision overwrote local edit: %+v", values)
	}
}

func TestSyncReplicaMergesIndependentMutableFields(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	defer store.Close()
	ctx := context.Background()
	base := SyncReplica{ObjectID: "obj_merge", SourceDeviceID: "device_a", Kind: "note", Epoch: 1, Revision: 1, Digest: "d1", PayloadJSON: json.RawMessage(`{"note":"base","tag":"one"}`)}
	if _, err := store.ApplySyncReplica(ctx, base); err != nil {
		t.Fatal(err)
	}
	if err := store.EditSyncReplica(ctx, base.ObjectID, json.RawMessage(`{"note":"local","tag":"one"}`)); err != nil {
		t.Fatal(err)
	}
	remote := base
	remote.Revision, remote.Digest, remote.PayloadJSON = 2, "d2", json.RawMessage(`{"note":"base","tag":"remote"}`)
	result, err := store.ApplySyncReplica(ctx, remote)
	if err != nil || result.Conflict {
		t.Fatalf("independent edit result=%+v err=%v", result, err)
	}
	replicas, _ := store.ListSyncReplicas(ctx)
	if len(replicas) != 1 || string(replicas[0].PayloadJSON) != `{"note":"local","tag":"remote"}` || !replicas[0].LocallyEdited {
		t.Fatalf("independent fields did not merge: %+v", replicas)
	}
}

func TestSyncCursorOnlyMovesForward(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	defer store.Close()
	ctx := context.Background()
	if err := store.SetSyncCursor(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSyncCursor(ctx, 9); !errors.Is(err, ErrSyncRollback) {
		t.Fatalf("rollback err=%v", err)
	}
	if cursor, err := store.SyncCursor(ctx); err != nil || cursor != 10 {
		t.Fatalf("cursor=%d err=%v", cursor, err)
	}
}

func TestSyncRevisionCarriesSignedParentLineage(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	defer store.Close()
	ctx := context.Background()
	revision, expected, parent, changed, err := store.PrepareSyncRevision(ctx, "obj_lineage", "content-one")
	if err != nil || revision != 1 || expected != 0 || parent != "" || !changed {
		t.Fatalf("first head revision=%d expected=%d parent=%q changed=%v err=%v", revision, expected, parent, changed, err)
	}
	if err = store.RecordSyncHead(ctx, "obj_lineage", revision, "wire-digest-one"); err != nil {
		t.Fatal(err)
	}
	revision, expected, parent, changed, err = store.PrepareSyncRevision(ctx, "obj_lineage", "content-two")
	if err != nil || revision != 2 || expected != 1 || parent != "wire-digest-one" || !changed {
		t.Fatalf("next head revision=%d expected=%d parent=%q changed=%v err=%v", revision, expected, parent, changed, err)
	}
}

func TestSyncSettingsRuntimeStatusAndResetAreDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessionsift.db")
	store, _ := Open(path)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	settings, err := store.SyncSettings(ctx)
	if err != nil || settings.Enabled || settings.KeyEpoch != 1 {
		t.Fatalf("default settings=%+v err=%v", settings, err)
	}
	if err = store.SaveSyncSettings(ctx, SyncSettings{}); err == nil {
		t.Fatal("empty category selection was accepted")
	}
	settings.Enabled, settings.Paused, settings.RotationPending = true, true, true
	settings.Summaries, settings.KeyEpoch = true, 3
	if err = store.SaveSyncSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings, err = store.SyncSettings(ctx)
	if err != nil || !settings.Enabled || !settings.Paused || !settings.RotationPending || settings.KeyEpoch != 3 {
		t.Fatalf("reopened settings=%+v err=%v", settings, err)
	}
	if pending, err := store.PendingSyncCount(ctx); err != nil || pending != 0 {
		t.Fatalf("pending=%d err=%v", pending, err)
	}
	if err = store.RecordSyncRun(ctx, 25, 100, errors.New("offline canary")); err != nil {
		t.Fatal(err)
	}
	state, err := store.SyncRuntimeState(ctx)
	if err != nil || state.LastSyncAt == nil || state.LastError != "offline canary" || state.UsageBytes != 25 || state.QuotaBytes != 100 {
		t.Fatalf("runtime state=%+v err=%v", state, err)
	}
	replica := SyncReplica{ObjectID: "obj_reset", SourceDeviceID: "device", Kind: "note", Epoch: 1, Revision: 1, Digest: "d", PayloadJSON: json.RawMessage(`{"note":"one"}`)}
	if _, err = store.ApplySyncReplica(ctx, replica); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkSyncReplicaEdited(ctx, replica.ObjectID); err != nil {
		t.Fatal(err)
	}
	if err = store.ResetSyncReplicaState(ctx); err != nil {
		t.Fatal(err)
	}
	if replicas, _ := store.ListSyncReplicas(ctx); len(replicas) != 0 {
		t.Fatalf("replicas remained after reset: %+v", replicas)
	}
	state, _ = store.SyncRuntimeState(ctx)
	if state.HighWaterCursor != 0 || state.LastSyncAt != nil || state.LastError != "" || state.UsageBytes != 0 {
		t.Fatalf("reset runtime state=%+v", state)
	}
}
