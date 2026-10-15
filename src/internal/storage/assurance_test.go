// @module ReplicaAssuranceTests detects authenticated same-revision forks rather than silently accepting them (A03/A07).
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestA03SameRevisionForkIsReported(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original := SyncReplica{ObjectID: "obj_fork", SourceDeviceID: "source", Kind: "note", Epoch: 1, Revision: 2, Digest: "trusted", PayloadJSON: json.RawMessage(`{"note":"original"}`)}
	if _, err = store.ApplySyncReplica(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	fork := original
	fork.Digest = "fork"
	fork.PayloadJSON = json.RawMessage(`{"note":"fork"}`)
	if _, err = store.ApplySyncReplica(context.Background(), fork); !errors.Is(err, ErrSyncRollback) {
		t.Fatalf("same-revision fork not reported: %v", err)
	}
	if _, err = store.ApplySyncReplica(context.Background(), original); err != nil {
		t.Fatal("exact replay must be idempotent")
	}
	replicas, err := store.ListSyncReplicas(context.Background())
	if err != nil || len(replicas) != 1 || replicas[0].Digest != "trusted" {
		t.Fatal("fork changed replica")
	}
}
