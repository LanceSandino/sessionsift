// Package api tests explicit local sync consent and recovery-key confirmation.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

func TestSyncPrepareRequiresRecoveryProofBeforeEnable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", home)
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	store, _ := storage.Open(filepath.Join(home, "sessionsift.db"))
	defer store.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(home, "identity.json"))
	manager, _ := remote.OpenManager(filepath.Join(home, "remote.json"))
	configPath := filepath.Join(home, "hosted.json")
	_ = remote.SaveHostedConfig(configPath, remote.HostedConfig{URL: "https://sync.example.test", DeviceID: identity.ID, WorkspaceID: "workspace_owner", Owner: "owner@example.com", Name: "Laptop"})
	hosted, _ := remote.OpenHostedClient(configPath, identity, manager, nil)
	handler := HandlerWithHostedRuntime(store, manager, identity, hosted)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodGet, "/api/v1/sync", nil))
	var initial map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &initial)
	if response.Code != http.StatusOK || initial["enabled"] != false || initial["key_configured"] != false {
		t.Fatalf("initial=%v status=%d", initial, response.Code)
	}
	selection := []byte(`{"summaries":true,"findings_notes":true,"detailed_evidence":false}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/prepare", bytes.NewReader(selection)))
	var prepared struct {
		RecoverySecret string `json:"recovery_secret"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &prepared)
	if response.Code != http.StatusCreated || prepared.RecoverySecret == "" {
		t.Fatalf("prepare status=%d body=%s", response.Code, response.Body.String())
	}
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/prepare", selection, http.StatusConflict)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/enable", []byte(`{`), http.StatusBadRequest)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/enable", []byte(`{"summaries":true}`), http.StatusBadRequest)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/enable", []byte(`{"recovery_secret":"invalid","summaries":true}`), http.StatusBadRequest)
	info, err := os.Stat(filepath.Join(home, "sync-master.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode=%v err=%v", info.Mode().Perm(), err)
	}

	wrong := []byte(`{"recovery_secret":"wrong","summaries":true,"findings_notes":true}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/enable", bytes.NewReader(wrong)))
	if response.Code == http.StatusOK {
		t.Fatal("wrong recovery proof enabled sync")
	}
	confirmed, _ := json.Marshal(map[string]any{"recovery_secret": prepared.RecoverySecret, "summaries": true, "findings_notes": true, "detailed_evidence": false})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/enable", bytes.NewReader(confirmed)))
	if response.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", response.Code, response.Body.String())
	}
	settings, _ := store.SyncSettings(context.Background())
	if !settings.Enabled || settings.Paused || !settings.Summaries || settings.DetailedEvidence {
		t.Fatalf("settings=%+v", settings)
	}
	for path, confirmation := range map[string]string{
		"/api/v1/sync/delete-cloud": "DELETE CLOUD COPIES",
		"/api/v1/sync/remove-local": "REMOVE LOCAL SYNC DATA",
		"/api/v1/sync/rotate-key":   "ROTATE SYNC KEY",
	} {
		performSyncRequest(t, handler, http.MethodPost, path, []byte(`{`), http.StatusBadRequest)
		body, _ := json.Marshal(map[string]string{"recovery_secret": prepared.RecoverySecret, "confirmation": confirmation + " WRONG"})
		performSyncRequest(t, handler, http.MethodPost, path, body, http.StatusForbidden)
	}
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/conflicts/resolve", []byte(`{`), http.StatusBadRequest)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/disable", bytes.NewReader([]byte(`{}`))))
	if response.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/pause", bytes.NewReader([]byte(`{}`))))
	if response.Code == http.StatusOK {
		t.Fatal("disabled sync was paused")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(http.MethodPost, "/api/v1/sync/recover", bytes.NewReader(confirmed)))
	if response.Code != http.StatusOK {
		t.Fatalf("recover status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSyncLifecycleRunsAgainstRealBridgeAndKeepsOriginalSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", home)
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	bridgeStore, _ := bridge.Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer bridgeStore.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(home, "identity.json"))
	registered, _ := bridgeStore.RegisterDevice("Laptop", identity.PublicKey)
	code, _ := supportcode.Generate()
	hash, _ := supportcode.Hash(supportcode.Link, code)
	_ = bridgeStore.CreateDeviceLink(registered.ID, hash, time.Now().Add(5*time.Minute))
	device, err := bridgeStore.ClaimDeviceLink("owner@example.com", code, "")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(bridge.NewHandler(bridgeStore, nil, ""))
	defer server.Close()
	store, _ := storage.Open(filepath.Join(home, "sessionsift.db"))
	defer store.Close()
	now := time.Now().UTC()
	if err = store.SaveImport(context.Background(), "fixture", "one", domain.Session{ID: "original", ExternalSessionID: "original", Title: "Original remains", Source: domain.SourceCodex, StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	manager, _ := remote.OpenManager(filepath.Join(home, "remote.json"))
	configPath := filepath.Join(home, "hosted.json")
	_ = remote.SaveHostedConfig(configPath, remote.HostedConfig{URL: server.URL, DeviceID: device.ID, WorkspaceID: device.WorkspaceID, Owner: "owner@example.com", Name: "Laptop"})
	hosted, _ := remote.OpenHostedClient(configPath, identity, manager, server.Client())
	defer hosted.Stop()
	handler := HandlerWithHostedRuntime(store, manager, identity, hosted)

	selection := []byte(`{"summaries":true,"findings_notes":true,"detailed_evidence":false}`)
	prepared := performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/prepare", selection, http.StatusCreated)
	var offer struct {
		RecoverySecret string `json:"recovery_secret"`
	}
	_ = json.Unmarshal(prepared, &offer)
	confirmed, _ := json.Marshal(map[string]any{"recovery_secret": offer.RecoverySecret, "summaries": true, "findings_notes": true})
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/enable", confirmed, http.StatusOK)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/run", []byte(`{}`), http.StatusOK)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/pause", []byte(`{}`), http.StatusOK)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/run", []byte(`{}`), http.StatusConflict)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/resume", []byte(`{}`), http.StatusOK)
	performSyncRequest(t, handler, http.MethodGet, "/api/v1/sync/conflicts", nil, http.StatusOK)

	rotate, _ := json.Marshal(map[string]string{"recovery_secret": offer.RecoverySecret, "confirmation": "ROTATE SYNC KEY"})
	rotated := performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/rotate-key", rotate, http.StatusOK)
	_ = json.Unmarshal(rotated, &offer)
	if offer.RecoverySecret == "" {
		t.Fatal("rotation did not return replacement recovery secret")
	}
	deleteBody, _ := json.Marshal(map[string]string{"recovery_secret": offer.RecoverySecret, "confirmation": "DELETE CLOUD COPIES"})
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/delete-cloud", deleteBody, http.StatusOK)
	removeBody, _ := json.Marshal(map[string]string{"recovery_secret": offer.RecoverySecret, "confirmation": "REMOVE LOCAL SYNC DATA"})
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/remove-local", removeBody, http.StatusOK)
	recoverBody, _ := json.Marshal(map[string]any{"recovery_secret": offer.RecoverySecret, "summaries": true})
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/recover", recoverBody, http.StatusOK)
	if session, getErr := store.GetSession(context.Background(), "original"); getErr != nil || session.Title != "Original remains" {
		t.Fatalf("sync deletion removed original provider session: %+v err=%v", session, getErr)
	}
}

func TestSyncAPIRejectsMissingConsentBridgeAndInvalidConflictChoice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", home)
	t.Setenv("SESSIONSIFT_SYNC_KEY_STORE", "file")
	store, _ := storage.Open(filepath.Join(home, "sessionsift.db"))
	defer store.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(home, "identity.json"))
	manager, _ := remote.OpenManager(filepath.Join(home, "remote.json"))
	handler := HandlerWithHostedRuntime(store, manager, identity, nil)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/prepare", []byte(`{"summaries":false}`), http.StatusBadRequest)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/prepare", []byte(`{"summaries":true}`), http.StatusBadRequest)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/run", []byte(`{}`), http.StatusConflict)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/conflicts/resolve", []byte(`{"id":0,"resolution":"both"}`), http.StatusBadRequest)
	performSyncRequest(t, handler, http.MethodPost, "/api/v1/sync/unknown", []byte(`{}`), http.StatusNotFound)
}

func TestPersonalSyncStateCopiesOnlyPortableNotesAndReviews(t *testing.T) {
	if state := personalSyncState(nil); len(state.Notes) != 0 || len(state.Reviews) != 0 {
		t.Fatalf("nil manager state=%+v", state)
	}
	manager, err := remote.OpenManager(filepath.Join(t.TempDir(), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	note, err := manager.AddAnnotation("session", "turn", "owner", "redacted note")
	if err != nil {
		t.Fatal(err)
	}
	review, err := manager.AddReview("session", "owner", "approved", "review summary")
	if err != nil {
		t.Fatal(err)
	}
	state := personalSyncState(manager)
	if len(state.Notes) != 1 || state.Notes[0].ID != note.ID || state.Notes[0].Body != note.Body {
		t.Fatalf("notes=%+v", state.Notes)
	}
	if len(state.Reviews) != 1 || state.Reviews[0].ID != review.ID || state.Reviews[0].Summary != review.Summary {
		t.Fatalf("reviews=%+v", state.Reviews)
	}
}

func performSyncRequest(t *testing.T, handler http.Handler, method, path string, body []byte, want int) []byte {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, localSyncRequest(method, path, reader))
	if response.Code != want {
		t.Fatalf("%s %s status=%d body=%s", method, path, response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

func localSyncRequest(method, path string, body *bytes.Reader) *http.Request {
	var reader io.Reader = http.NoBody
	if body != nil {
		reader = body
	}
	request := httptest.NewRequest(method, path, reader)
	request.Host = "127.0.0.1:7777"
	request.Header.Set("Content-Type", "application/json")
	return request
}
