// @module BridgeMaintenanceTests verifies startup recovery snapshots and cancellation.
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
)

func TestBridgeMaintenanceStartupSnapshotAndCancellation(t *testing.T) {
	directory := t.TempDir()
	store, err := bridgeapp.Open(filepath.Join(directory, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var output bytes.Buffer
	runBridgeMaintenance(ctx, store, filepath.Join(directory, "backups"), &output)
	entries, err := os.ReadDir(filepath.Join(directory, "backups"))
	if err != nil || len(entries) != 1 || output.Len() != 0 {
		t.Fatalf("snapshots=%d err=%v output=%q", len(entries), err, output.String())
	}
}
