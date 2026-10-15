// Package cli verifies that optional cloud sync has no default-mode egress path.
package cli

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

func TestCloudSyncLoopDoesNothingUntilExplicitlyEnabled(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	paths := config.Paths{SyncMasterKey: filepath.Join(t.TempDir(), "missing-sync.key")}
	if err = runCloudSyncOnce(context.Background(), store, paths, remote.Identity{}, nil, nil); err != nil {
		t.Fatalf("default local-only mode touched bridge/key path: %v", err)
	}
	settings, _ := store.SyncSettings(context.Background())
	settings.Enabled, settings.Paused = true, true
	if err = store.SaveSyncSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if err = runCloudSyncOnce(context.Background(), store, paths, remote.Identity{}, nil, nil); err != nil {
		t.Fatalf("paused sync touched bridge/key path: %v", err)
	}
}

func TestCloudSyncOnceUploadsSelectedSnapshotWhenEnabled(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	home := t.TempDir()
	paths := config.Paths{SyncMasterKey: filepath.Join(home, "sync.key")}
	store, _ := storage.Open(filepath.Join(home, "local.db"))
	defer store.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(home, "identity.json"))
	bridgeStore, _ := bridge.Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer bridgeStore.Close()
	registered, _ := bridgeStore.RegisterDevice("Laptop", identity.PublicKey)
	code, _ := supportcode.Generate()
	hash, _ := supportcode.Hash(supportcode.Link, code)
	_ = bridgeStore.CreateDeviceLink(registered.ID, hash, time.Now().Add(5*time.Minute))
	device, _ := bridgeStore.ClaimDeviceLink("owner@example.com", code, "")
	server := httptest.NewServer(bridge.NewHandler(bridgeStore, nil, ""))
	defer server.Close()
	configPath := filepath.Join(home, "hosted.json")
	_ = remote.SaveHostedConfig(configPath, remote.HostedConfig{URL: server.URL, DeviceID: device.ID, WorkspaceID: device.WorkspaceID, Owner: "owner@example.com", Name: "Laptop"})
	manager, _ := remote.OpenManager(filepath.Join(home, "remote.json"))
	hosted, _ := remote.OpenHostedClient(configPath, identity, manager, server.Client())
	defer hosted.Stop()
	previousClient := hostedHTTPClient
	hostedHTTPClient = server.Client()
	t.Cleanup(func() { hostedHTTPClient = previousClient })
	key, _ := cloudsync.GenerateMasterKey(nil)
	if err := cloudsync.SaveMasterKey(paths.SyncMasterKey, key); err != nil {
		t.Fatal(err)
	}
	settings, _ := store.SyncSettings(context.Background())
	settings.Enabled, settings.Summaries = true, true
	if err := store.SaveSyncSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveImport(context.Background(), "fixture", "one", domain.Session{ID: "session", ExternalSessionID: "session", Title: "Selected", Source: domain.SourceCodex, StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	if err := runCloudSyncOnce(context.Background(), store, paths, identity, manager, hosted); err != nil {
		t.Fatal(err)
	}
	state, _ := store.SyncRuntimeState(context.Background())
	if state.LastSyncAt == nil || state.UsageBytes == 0 || state.LastError != "" {
		t.Fatalf("sync state=%+v", state)
	}
}

func TestCLIPersonalSyncStateCopiesPortableRecords(t *testing.T) {
	if state := cliPersonalSyncState(nil); len(state.Notes) != 0 || len(state.Reviews) != 0 {
		t.Fatalf("nil manager state=%+v", state)
	}
	manager, err := remote.OpenManager(filepath.Join(t.TempDir(), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	note, err := manager.AddAnnotation("session", "turn", "owner", "note")
	if err != nil {
		t.Fatal(err)
	}
	review, err := manager.AddReview("session", "owner", "approved", "summary")
	if err != nil {
		t.Fatal(err)
	}
	state := cliPersonalSyncState(manager)
	if len(state.Notes) != 1 || state.Notes[0].ID != note.ID || len(state.Reviews) != 1 || state.Reviews[0].ID != review.ID {
		t.Fatalf("state=%+v", state)
	}
}
