// Package cloudsync_test verifies the public client against the real signed bridge HTTP boundary.
package cloudsync_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

func TestHTTPTransportRunsSelectedSnapshotRecoveryUsageAndDeletion(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	bridgeStore, err := bridge.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeStore.Close()
	identityA, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "a.json"))
	identityB, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "b.json"))
	deviceA := linkTestDevice(t, bridgeStore, identityA, "owner@example.com", "A", "")
	deviceB := linkTestDevice(t, bridgeStore, identityB, "owner@example.com", "B", deviceA.WorkspaceID)
	server := httptest.NewServer(bridge.NewHandler(bridgeStore, nil, ""))
	defer server.Close()

	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	localA, _ := storage.Open(filepath.Join(t.TempDir(), "a.db"))
	defer localA.Close()
	now := time.Now().UTC()
	session := domain.Session{ID: "private-local-id", ExternalSessionID: "private-external-id", Title: "TRANSPORT-CANARY", Source: domain.SourceCodex, StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion,
		Turns: []domain.Turn{{ID: "turn", UserPromptRedacted: "selected evidence"}}}
	if err = localA.SaveImport(context.Background(), "synthetic", "one", session); err != nil {
		t.Fatal(err)
	}
	transportA := &cloudsync.HTTPTransport{Client: server.Client(), Config: remote.HostedConfig{URL: server.URL, DeviceID: deviceA.ID, WorkspaceID: deviceA.WorkspaceID}, Identity: identityA}
	engineA := cloudsync.Engine{Store: localA, Transport: transportA, MasterKey: master, WorkspaceID: deviceA.WorkspaceID, DeviceID: deviceA.ID, Epoch: 1, Sign: identityA.Sign}
	queued, err := cloudsync.QueueSnapshots(context.Background(), &engineA, localA, cloudsync.SnapshotSelection{Summaries: true})
	if err != nil || queued.Queued != 1 {
		t.Fatalf("queued=%+v err=%v", queued, err)
	}
	if err = engineA.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	usage, err := transportA.Usage(context.Background())
	if err != nil || usage.Objects != 1 || usage.Bytes == 0 || usage.Limit == 0 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}

	localB, _ := storage.Open(filepath.Join(t.TempDir(), "b.db"))
	defer localB.Close()
	transportB := &cloudsync.HTTPTransport{Client: server.Client(), Config: remote.HostedConfig{URL: server.URL, DeviceID: deviceB.ID, WorkspaceID: deviceA.WorkspaceID}, Identity: identityB}
	engineB := cloudsync.Engine{Store: localB, Transport: transportB, MasterKey: master, WorkspaceID: deviceA.WorkspaceID, DeviceID: deviceB.ID, Epoch: 1, Sign: identityB.Sign}
	if err = engineB.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	replicas, _ := localB.ListSyncReplicas(context.Background())
	if len(replicas) != 1 || !replicas[0].RemoteSource {
		t.Fatalf("replicas=%+v", replicas)
	}
	currentEpoch, err := transportA.WorkspaceEpoch(context.Background())
	if err != nil || currentEpoch != 1 {
		t.Fatalf("workspace epoch before deletion=%d err=%v", currentEpoch, err)
	}
	nextEpoch, err := transportA.DeleteWorkspace(context.Background())
	if err != nil || nextEpoch <= 1 {
		t.Fatalf("next epoch=%d err=%v", nextEpoch, err)
	}
	currentEpoch, err = transportA.WorkspaceEpoch(context.Background())
	if err != nil || currentEpoch != nextEpoch {
		t.Fatalf("workspace epoch after deletion=%d want=%d err=%v", currentEpoch, nextEpoch, err)
	}
	usage, err = transportB.Usage(context.Background())
	if err != nil || usage.Objects != 0 || usage.Bytes != 0 {
		t.Fatalf("post-delete usage=%+v err=%v", usage, err)
	}
}

func TestHTTPTransportReturnsBoundedErrorsForDisabledAndRevokedDevice(t *testing.T) {
	bridgeStore, _ := bridge.Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer bridgeStore.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	device := linkTestDevice(t, bridgeStore, identity, "owner@example.com", "A", "")
	server := httptest.NewServer(bridge.NewHandler(bridgeStore, nil, ""))
	defer server.Close()
	transport := &cloudsync.HTTPTransport{Client: server.Client(), Config: remote.HostedConfig{URL: server.URL, DeviceID: device.ID, WorkspaceID: device.WorkspaceID}, Identity: identity}
	if _, err := transport.Usage(context.Background()); err == nil {
		t.Fatal("disabled sync endpoint reported success")
	}
	if _, err := transport.WorkspaceEpoch(context.Background()); err == nil {
		t.Fatal("disabled workspace epoch endpoint reported success")
	}
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := transport.Usage(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled sync request err=%v", err)
	}
	if err := bridgeStore.RevokeDevice(device.ID, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Usage(context.Background()); err == nil {
		t.Fatal("revoked device retained sync access")
	}
}

func linkTestDevice(t *testing.T, store *bridge.Store, identity remote.Identity, owner, name, workspaceID string) bridge.Device {
	t.Helper()
	registered, err := store.RegisterDevice(name, identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := supportcode.Generate()
	hash, _ := supportcode.Hash(supportcode.Link, code)
	if err = store.CreateDeviceLink(registered.ID, hash, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	linked, err := store.ClaimDeviceLink(owner, code, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return linked
}
