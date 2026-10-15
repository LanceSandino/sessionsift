// Package api tests cloud-sync rotation failure ordering with disposable state.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

// A10/Y13: failing local key replacement must not erase the only cloud copy.
func TestSeparateReviewFailedKeyRotationPreservesRemoteCiphertext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", home)
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	bridgeStore, err := bridge.Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeStore.Close()
	identity, err := remote.LoadOrCreateIdentity(filepath.Join(home, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := bridgeStore.RegisterDevice("Laptop", identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	code, err := supportcode.Generate()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := supportcode.Hash(supportcode.Link, code)
	if err != nil {
		t.Fatal(err)
	}
	if err := bridgeStore.CreateDeviceLink(registered.ID, hash, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	device, err := bridgeStore.ClaimDeviceLink("owner@example.com", code, "")
	if err != nil {
		t.Fatal(err)
	}
	deleteRequests := 0
	rejectDelete := false
	loseDeleteAcknowledgement := false
	loseWorkspaceState := false
	workspaceStateUnavailable := false
	bridgeHandler := bridge.NewHandler(bridgeStore, nil, "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if workspaceStateUnavailable && r.Method == http.MethodGet && r.URL.Path == "/agent/v1/sync/workspace" {
			http.Error(w, "synthetic workspace-state outage", http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/agent/v1/sync/workspace" {
			deleteRequests++
			if rejectDelete {
				http.Error(w, "synthetic bridge rotation fault", http.StatusServiceUnavailable)
				return
			}
			if loseDeleteAcknowledgement {
				// The signed bridge request commits, but a proxy loses its 200 response.
				committed := httptest.NewRecorder()
				bridgeHandler.ServeHTTP(committed, r)
				if committed.Code != http.StatusOK {
					t.Errorf("synthetic bridge deletion did not commit: status=%d body=%s", committed.Code, committed.Body.String())
				}
				workspaceStateUnavailable = loseWorkspaceState
				http.Error(w, "synthetic acknowledgement loss", http.StatusServiceUnavailable)
				return
			}
		}
		bridgeHandler.ServeHTTP(w, r)
	}))
	defer server.Close()
	store, err := storage.Open(filepath.Join(home, "sessionsift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := remote.OpenManager(filepath.Join(home, "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "hosted.json")
	if err := remote.SaveHostedConfig(configPath, remote.HostedConfig{URL: server.URL, DeviceID: device.ID,
		WorkspaceID: device.WorkspaceID, Owner: "owner@example.com", Name: "Laptop"}); err != nil {
		t.Fatal(err)
	}
	hosted, err := remote.OpenHostedClient(configPath, identity, manager, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer hosted.Stop()
	handler := HandlerWithHostedRuntime(store, manager, identity, hosted)
	selection := []byte(`{"summaries":true,"findings_notes":true}`)
	prepared := performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/prepare", selection, http.StatusCreated)
	var offer struct {
		RecoverySecret string `json:"recovery_secret"`
	}
	if err := json.Unmarshal(prepared, &offer); err != nil || offer.RecoverySecret == "" {
		t.Fatalf("prepare recovery secret: %v", err)
	}
	confirmed, _ := json.Marshal(map[string]any{"recovery_secret": offer.RecoverySecret, "summaries": true, "findings_notes": true})
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/enable", confirmed, http.StatusOK)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/run", []byte(`{}`), http.StatusOK)
	before, err := bridgeStore.SyncUsage(device.ID)
	if err != nil || before.Objects == 0 {
		t.Fatalf("positive control has no recoverable cloud copy: %+v, %v", before, err)
	}
	rotate, _ := json.Marshal(map[string]string{"recovery_secret": offer.RecoverySecret, "confirmation": "ROTATE SYNC KEY"})
	priorPersist := persistSyncSettings
	persistSyncSettings = func(context.Context, *storage.Store, storage.SyncSettings) error {
		return errors.New("synthetic pending-state fault")
	}
	t.Cleanup(func() { persistSyncSettings = priorPersist })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/rotate-key", bytes.NewReader(rotate)))
	if response.Code < http.StatusBadRequest || deleteRequests != 0 {
		t.Fatalf("pending-state fault status=%d destructive_requests=%d body=%s", response.Code, deleteRequests, response.Body.String())
	}
	persistSyncSettings = priorPersist
	settings, err := store.SyncSettings(context.Background())
	if err != nil || settings.RotationPending || settings.Paused {
		t.Fatalf("pending-state fault changed durable settings: settings=%+v err=%v", settings, err)
	}
	priorReplace := replaceSyncKey
	replaceSyncKey = func(cloudsync.KeyStore, []byte) error { return errors.New("synthetic key-store fault") }
	t.Cleanup(func() { replaceSyncKey = priorReplace })
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/rotate-key", bytes.NewReader(rotate)))
	if response.Code < http.StatusBadRequest {
		t.Fatalf("key-store fault did not abort rotation: status=%d body=%s", response.Code, response.Body.String())
	}
	if deleteRequests != 0 {
		t.Fatalf("failed local key replacement issued %d destructive cloud requests", deleteRequests)
	}
	after, err := bridgeStore.SyncUsage(device.ID)
	if err != nil || after.Objects != before.Objects || after.Bytes != before.Bytes {
		t.Fatalf("failed rotation erased remote copy: before=%+v after=%+v err=%v", before, after, err)
	}

	// Once the replacement key is durable, a failed destructive bridge request
	// must restore the old key because the bridge still holds old-key ciphertext.
	replaceSyncKey = priorReplace
	rejectDelete = true
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/rotate-key", bytes.NewReader(rotate)))
	if response.Code < http.StatusBadRequest {
		t.Fatalf("bridge fault did not abort rotation: status=%d body=%s", response.Code, response.Body.String())
	}
	wantKey, err := cloudsync.DecodeRecoverySecret(offer.RecoverySecret)
	if err != nil {
		t.Fatal(err)
	}
	gotKey, err := cloudsync.LoadMasterKey(filepath.Join(home, "sync-master.key"))
	if err != nil || !bytes.Equal(gotKey, wantKey) {
		t.Fatalf("failed bridge rotation did not restore prior key: match=%t err=%v", bytes.Equal(gotKey, wantKey), err)
	}
	after, err = bridgeStore.SyncUsage(device.ID)
	if err != nil || after.Objects != before.Objects || after.Bytes != before.Bytes {
		t.Fatalf("failed bridge rotation changed remote copy: before=%+v after=%+v err=%v", before, after, err)
	}

	// A failed HTTP response is not proof the destructive request did not commit.
	// The old secret must not be restored while its only cloud copy has been purged.
	rejectDelete = false
	loseDeleteAcknowledgement = true
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/rotate-key", bytes.NewReader(rotate)))
	if response.Code != http.StatusOK {
		t.Fatalf("lost bridge acknowledgement was not reconciled: status=%d body=%s", response.Code, response.Body.String())
	}
	var resolved struct {
		RecoverySecret string `json:"recovery_secret"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &resolved); err != nil || resolved.RecoverySecret == "" {
		t.Fatalf("reconciled rotation omitted replacement secret: body=%s err=%v", response.Body.String(), err)
	}
	resolvedKey, err := cloudsync.DecodeRecoverySecret(resolved.RecoverySecret)
	if err != nil {
		t.Fatal(err)
	}
	gotKey, err = cloudsync.LoadMasterKey(filepath.Join(home, "sync-master.key"))
	if err != nil || !bytes.Equal(gotKey, resolvedKey) || bytes.Equal(gotKey, wantKey) {
		t.Fatalf("lost acknowledgement did not retain replacement key: replacement=%t old=%t err=%v", bytes.Equal(gotKey, resolvedKey), bytes.Equal(gotKey, wantKey), err)
	}
	after, err = bridgeStore.SyncUsage(device.ID)
	if err != nil || after.Objects != 0 || after.Bytes != 0 {
		t.Fatalf("reconciled rotation retained stale cloud copy: before=%+v after=%+v err=%v", before, after, err)
	}
	settings, err = store.SyncSettings(context.Background())
	if err != nil || !settings.Enabled || settings.Paused || settings.KeyEpoch <= 1 {
		t.Fatalf("reconciled rotation settings=%+v err=%v", settings, err)
	}

	// If both acknowledgement and reconciliation are unavailable, retain and
	// return the replacement secret and pause sync instead of guessing that the
	// destructive request failed and silently restoring an unusable old key.
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/run", []byte(`{}`), http.StatusOK)
	loseWorkspaceState = true
	secondRotate, _ := json.Marshal(map[string]string{"recovery_secret": resolved.RecoverySecret, "confirmation": "ROTATE SYNC KEY"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/rotate-key", bytes.NewReader(secondRotate)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("ambiguous rotation status=%d body=%s", response.Code, response.Body.String())
	}
	var pending struct {
		RecoverySecret string `json:"recovery_secret"`
		RetryRequired  bool   `json:"retry_required"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &pending); err != nil || pending.RecoverySecret == "" || !pending.RetryRequired {
		t.Fatalf("ambiguous rotation omitted recovery state: body=%s err=%v", response.Body.String(), err)
	}
	pendingKey, err := cloudsync.DecodeRecoverySecret(pending.RecoverySecret)
	if err != nil {
		t.Fatal(err)
	}
	gotKey, err = cloudsync.LoadMasterKey(filepath.Join(home, "sync-master.key"))
	if err != nil || !bytes.Equal(gotKey, pendingKey) || bytes.Equal(gotKey, resolvedKey) {
		t.Fatalf("ambiguous rotation key state: pending=%t prior=%t err=%v", bytes.Equal(gotKey, pendingKey), bytes.Equal(gotKey, resolvedKey), err)
	}
	settings, err = store.SyncSettings(context.Background())
	if err != nil || !settings.Paused {
		t.Fatalf("ambiguous rotation did not pause sync: settings=%+v err=%v", settings, err)
	}

	// The ordinary Resume control and the recovery-offer Confirm and enable
	// control must not undo the safety pause before the unknown DELETE outcome
	// has been reconciled against the bridge epoch.
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/resume", bytes.NewReader([]byte(`{}`))))
	settings, err = store.SyncSettings(context.Background())
	if err != nil || response.Code < http.StatusBadRequest || !settings.Paused {
		t.Errorf("ambiguous rotation resume bypass: status=%d paused=%t err=%v", response.Code, settings.Paused, err)
	}
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/pause", []byte(`{}`), http.StatusOK)
	reconfirm, _ := json.Marshal(map[string]any{"recovery_secret": pending.RecoverySecret, "summaries": true, "findings_notes": true})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/enable", bytes.NewReader(reconfirm)))
	settings, err = store.SyncSettings(context.Background())
	if err != nil || response.Code < http.StatusBadRequest || !settings.Paused {
		t.Errorf("ambiguous rotation enable bypass: status=%d paused=%t err=%v", response.Code, settings.Paused, err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/disable", bytes.NewReader([]byte(`{}`))))
	settings, err = store.SyncSettings(context.Background())
	if err != nil || response.Code < http.StatusBadRequest || !settings.RotationPending {
		t.Errorf("ambiguous rotation disable bypass: status=%d pending=%t err=%v", response.Code, settings.RotationPending, err)
	}

	// A retry is already authorized by the durable pending state and must work
	// even if the caller never received the first replacement-secret response.
	loseWorkspaceState = false
	workspaceStateUnavailable = false
	retry, _ := json.Marshal(map[string]string{"confirmation": "ROTATE SYNC KEY"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/rotate-key", bytes.NewReader(retry)))
	settings, err = store.SyncSettings(context.Background())
	if err != nil || response.Code != http.StatusOK || settings.RotationPending || settings.Paused {
		t.Errorf("pending rotation retry: status=%d settings=%+v err=%v body=%s", response.Code, settings, err, response.Body.String())
	}
	var retried struct {
		RecoverySecret string `json:"recovery_secret"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &retried); err != nil || retried.RecoverySecret == "" || retried.RecoverySecret == pending.RecoverySecret {
		t.Errorf("pending rotation retry did not issue a fresh recoverable key: response=%s err=%v", response.Body.String(), err)
	}
}
