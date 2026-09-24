// @module BridgeHardeningTests verifies hostile enrollment, replay, and browser requests.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

// Ownership cannot be replaced by re-registering a known public key or by a
// second owner entering a link code the device later shows.
func TestLinkingCannotReplaceExistingOwner(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, s, "owner@example.com", "Original", public)
	again, err := s.RegisterDevice("Stolen", public)
	if err != nil || again.Owner != "owner@example.com" || again.Name != "Original" || again.WorkspaceID != device.WorkspaceID {
		t.Fatalf("re-registration changed the device: %+v err=%v", again, err)
	}
	if _, err = s.ClaimDeviceLink("attacker@example.com", testLinkCode(t, s, device.ID), ""); err == nil {
		t.Fatal("second owner took over a linked device")
	}
	owned, err := s.OwnedDevices("owner@example.com")
	if err != nil || len(owned) != 1 || owned[0].ID != device.ID {
		t.Fatal("original ownership changed")
	}
	if stolen, _ := s.OwnedDevices("attacker@example.com"); len(stolen) != 0 {
		t.Fatal("attacker owns the device")
	}
	if err = s.RevokeDevice(device.ID, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterDevice("Laptop", public); err == nil {
		t.Fatal("a disconnected identity re-registered")
	}
}

func TestPairedBackupRestoresAuditAndEncryptedTasks(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, s, "owner@example.com", "Laptop", public)
	_, err = s.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionGet}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.Request{Version: protocol.Version, RequestID: "audit-test", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"private-session"}`)}
	task, err := s.Submit("owner@example.com", device.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	var workspace, operation, status string
	if err = s.db.QueryRow(`SELECT workspace_id,operation,status FROM bridge_audit WHERE id=?`, task.ID).Scan(&workspace, &operation, &status); err != nil || workspace != device.WorkspaceID || operation != "session.get" || status != "queued" {
		t.Fatalf("audit unavailable: %v", err)
	}
	backup, err := s.Backup(context.Background(), filepath.Join(root, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bridge.db", "bridge.db.key"} {
		info, err := os.Stat(filepath.Join(backup, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe backup %s: %v", name, err)
		}
	}
	restored, err := Open(filepath.Join(backup, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	claimed, found, err := restored.Next(device.ID)
	if err != nil || !found || claimed.Request.RequestID != "audit-test" {
		t.Fatalf("restore failed: %v", err)
	}
	// A failed audit write must also prevent queueing the request.
	if _, err = s.db.Exec(`DROP TABLE bridge_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit("owner@example.com", device.ID, request); err == nil {
		t.Fatal("accepted request without durable audit")
	}
}

func TestMissingAndWrongKeysFailClosed(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "bridge.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	key, err := os.ReadFile(db + ".key")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(db + ".key"); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(db); err == nil {
		reopened.Close()
		t.Fatal("silently replaced lost key")
	}
	if err = os.WriteFile(db+".key", make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(db); err == nil {
		reopened.Close()
		t.Fatal("wrong key accepted for empty database")
	}
	if err = os.WriteFile(db+".key", key, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
}

func TestSignedRequestReplayRejectedAcrossHandlers(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, s, "owner@example.com", "Laptop", public)
	request := signedBridgeRequest(t, private, device.ID, http.MethodGet, "/agent/v1/tasks/next", nil)
	first := httptest.NewRecorder()
	NewHandler(s, nil, "").ServeHTTP(first, request)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first = %d", first.Code)
	}
	replay := httptest.NewRecorder()
	NewHandler(s, nil, "").ServeHTTP(replay, request.Clone(context.Background()))
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay accepted = %d", replay.Code)
	}
}

func TestBrowserMutationRejectsForeignOrigin(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	verifier := accessauth.VerifierFunc(func(context.Context, string) (accessauth.Principal, error) {
		return accessauth.Principal{Subject: "test", Email: "owner@example.com"}, nil
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(`{"name":"Injected"}`))
	request.Header.Set("Origin", "https://attacker.invalid")
	response := httptest.NewRecorder()
	NewHandler(s, verifier, "https://sessionsift.example").ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign origin accepted = %d", response.Code)
	}
}
