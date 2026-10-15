// Package bridge verifies the signed sync/v1 HTTP boundary end to end.
package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/cloudsync"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestSyncHTTPTransfersClientEncryptedCanaryBetweenDevices(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	database := filepath.Join(t.TempDir(), "bridge.db")
	store, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identityA, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "a.json"))
	identityB, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "b.json"))
	deviceA := testOwnedDevice(t, store, "owner@example.com", "A", identityA.PublicKey)
	_ = testOwnedDeviceIn(t, store, "owner@example.com", deviceA.WorkspaceID, "B", identityB.PublicKey)
	bridgeHandler := NewHandler(store, nil, "")
	var capturedBodies bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBodies.Write(body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		bridgeHandler.ServeHTTP(w, r)
	}))
	defer server.Close()

	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	canary := []byte(`{"title":"Y05-CANARY-TITLE","path":"/private/Y05-CANARY-PATH","note":"Y05-CANARY-TOKEN"}`)
	metadata := cloudsync.Metadata{Schema: 1, WorkspaceID: deviceA.WorkspaceID, ObjectID: cloudsync.ObjectID(master, "session", "local-private-id"), SourceDeviceID: deviceA.ID, WriterDeviceID: deviceA.ID, Kind: "session_summary", Epoch: 1, Revision: 1}
	envelope, err := cloudsync.Seal(master, metadata, canary, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nonce, _ := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	ciphertext, _ := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	object := SyncObject{WorkspaceID: deviceA.WorkspaceID, ObjectID: metadata.ObjectID, SourceDeviceID: deviceA.ID, WriterDeviceID: deviceA.ID, Kind: metadata.Kind, Epoch: 1, Revision: 1, Nonce: nonce, Ciphertext: ciphertext}
	digest := digestSyncObject(object)
	object.Digest, object.Signature = hex.EncodeToString(digest[:]), identityA.Sign(digest[:])
	body, _ := json.Marshal(object)
	response := signedSyncRequest(t, server.URL, identityA, http.MethodPut, "/agent/v1/sync/objects/"+object.ObjectID+"?expected_revision=0", body)
	if response.StatusCode != http.StatusCreated {
		message, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("put status=%d body=%s", response.StatusCode, message)
	}
	response.Body.Close()

	response = signedSyncRequest(t, server.URL, identityB, http.MethodGet, "/agent/v1/sync/changes?cursor=0&limit=10", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("changes status=%d", response.StatusCode)
	}
	var page struct {
		Changes []SyncChange `json:"changes"`
		More    bool         `json:"more"`
	}
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Changes) != 1 || page.More {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	response.Body.Close()
	response = signedSyncRequest(t, server.URL, identityB, http.MethodGet, "/agent/v1/sync/objects/"+object.ObjectID, nil)
	var restored SyncObject
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&restored) != nil {
		t.Fatalf("get status=%d", response.StatusCode)
	}
	response.Body.Close()
	opened, err := cloudsync.Open(master, cloudsync.Envelope{Metadata: metadata, Nonce: base64.RawURLEncoding.EncodeToString(restored.Nonce), Ciphertext: base64.RawURLEncoding.EncodeToString(restored.Ciphertext)})
	if err != nil || !bytes.Equal(opened, canary) {
		t.Fatalf("restored=%q err=%v", opened, err)
	}
	if _, err = store.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	databaseBytes, _ := os.ReadFile(database)
	backup, backupErr := store.Backup(context.Background(), filepath.Join(t.TempDir(), "backups"))
	if backupErr != nil {
		t.Fatal(backupErr)
	}
	backupBytes, _ := os.ReadFile(filepath.Join(backup, "bridge.db"))
	for name, content := range map[string][]byte{"captured request": capturedBodies.Bytes(), "bridge database": databaseBytes, "bridge backup": backupBytes} {
		if bytes.Contains(content, []byte("Y05-CANARY")) || bytes.Contains(content, []byte("local-private-id")) {
			t.Fatalf("plaintext canary reached %s", name)
		}
	}
}

func TestSyncEngineUsesSignedHTTPTransportForTwoDeviceRecovery(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	bridgePath := filepath.Join(t.TempDir(), "bridge.db")
	bridgeStore, err := Open(bridgePath)
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeStore.Close()
	identityA, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "a.json"))
	identityB, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "b.json"))
	deviceA := testOwnedDevice(t, bridgeStore, "owner@example.com", "A", identityA.PublicKey)
	deviceB := testOwnedDeviceIn(t, bridgeStore, "owner@example.com", deviceA.WorkspaceID, "B", identityB.PublicKey)
	server := httptest.NewServer(NewHandler(bridgeStore, nil, ""))
	defer server.Close()
	master, _ := cloudsync.GenerateMasterKey(rand.Reader)
	localA, _ := storage.Open(filepath.Join(t.TempDir(), "local-a.db"))
	defer localA.Close()
	localB, _ := storage.Open(filepath.Join(t.TempDir(), "local-b.db"))
	defer localB.Close()
	transportA := &cloudsync.HTTPTransport{Client: server.Client(), Config: remote.HostedConfig{URL: server.URL, DeviceID: deviceA.ID, WorkspaceID: deviceA.WorkspaceID}, Identity: identityA}
	transportB := &cloudsync.HTTPTransport{Client: server.Client(), Config: remote.HostedConfig{URL: server.URL, DeviceID: deviceB.ID, WorkspaceID: deviceA.WorkspaceID}, Identity: identityB}
	engineA := cloudsync.Engine{Store: localA, Transport: transportA, MasterKey: master, WorkspaceID: deviceA.WorkspaceID, DeviceID: deviceA.ID, Epoch: 1, Sign: identityA.Sign}
	payload := json.RawMessage(`{"title":"HTTP-E2EE-CANARY","label":"Synced snapshot from A"}`)
	if err = engineA.Queue(context.Background(), cloudsync.Record{LocalID: "private-local-id", Kind: "session_summary", Revision: 1, Payload: payload}, 0); err != nil {
		t.Fatal(err)
	}
	if err = engineA.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	engineB := cloudsync.Engine{Store: localB, Transport: transportB, MasterKey: master, WorkspaceID: deviceA.WorkspaceID, DeviceID: deviceB.ID, Epoch: 1, Sign: identityB.Sign}
	if err = engineB.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	replicas, _ := localB.ListSyncReplicas(context.Background())
	if len(replicas) != 1 || string(replicas[0].PayloadJSON) != string(payload) || !replicas[0].RemoteSource {
		t.Fatalf("two-device HTTP restore=%+v", replicas)
	}
	content, _ := os.ReadFile(bridgePath)
	if bytes.Contains(content, []byte("HTTP-E2EE-CANARY")) || bytes.Contains(content, []byte("private-local-id")) {
		t.Fatal("server database contained sync plaintext or local identifier")
	}
}

func TestSyncHTTPRequiresReplayNonceAndFeatureFlag(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	device := testOwnedDevice(t, store, "owner@example.com", "A", identity.PublicKey)
	server := httptest.NewServer(NewHandler(store, nil, ""))
	defer server.Close()
	response := signedSyncRequest(t, server.URL, identity, http.MethodGet, "/agent/v1/sync/usage", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled status=%d device=%s", response.StatusCode, device.ID)
	}
	response.Body.Close()
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/agent/v1/sync/usage", nil)
	request.Header.Set("X-SessionSift-Device", identity.ID)
	request.Header.Set("X-SessionSift-Timestamp", timestamp)
	request.Header.Set("X-SessionSift-Signature", base64.RawURLEncoding.EncodeToString(identity.Sign(deviceSignatureMessage(http.MethodGet, "/agent/v1/sync/usage", timestamp, nil))))
	response, _ = http.DefaultClient.Do(request)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("nonceless sync status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestSyncHTTPEnforcesPerDeviceFairUseLimit(t *testing.T) {
	t.Setenv("SESSIONSIFT_SYNC_ENABLED", "true")
	t.Setenv("SESSIONSIFT_SYNC_REQUESTS_PER_MINUTE", "1")
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	_ = testOwnedDevice(t, store, "owner@example.com", "A", identity.PublicKey)
	server := httptest.NewServer(NewHandler(store, nil, ""))
	defer server.Close()
	first := signedSyncRequest(t, server.URL, identity, http.MethodGet, "/agent/v1/sync/usage", nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status=%d", first.StatusCode)
	}
	first.Body.Close()
	second := signedSyncRequest(t, server.URL, identity, http.MethodGet, "/agent/v1/sync/usage", nil)
	if second.StatusCode != http.StatusTooManyRequests || second.Header.Get("Retry-After") != "60" {
		t.Fatalf("second status=%d retry=%q", second.StatusCode, second.Header.Get("Retry-After"))
	}
	second.Body.Close()
}

func signedSyncRequest(t *testing.T, serverURL string, identity remote.Identity, method, path string, body []byte) *http.Response {
	t.Helper()
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonceBytes := make([]byte, 16)
	_, _ = rand.Read(nonceBytes)
	replayNonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	request, err := http.NewRequest(method, serverURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	normalized := request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		normalized += "?" + request.URL.Query().Encode()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-SessionSift-Device", identity.ID)
	request.Header.Set("X-SessionSift-Timestamp", timestamp)
	request.Header.Set("X-SessionSift-Nonce", replayNonce)
	request.Header.Set("X-SessionSift-Signature", base64.RawURLEncoding.EncodeToString(identity.Sign(deviceSignatureMessageV2(method, normalized, timestamp, replayNonce, body))))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
