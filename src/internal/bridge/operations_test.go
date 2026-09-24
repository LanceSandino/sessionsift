// @module BridgeOperationsTests anchors private backups, bounded storage and safe public assets.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueueAndEnrollmentBudgetsAreBounded(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	device := enrollTenantDevice(t, s, "owner@example.com", "Laptop")
	_, err = s.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if _, err = s.Submit("owner@example.com", device.ID, protocol.Request{Version: 1, RequestID: "budget", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Submit("owner@example.com", device.ID, protocol.Request{Version: 1, RequestID: "over-budget", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("queue unbounded")
	}
	h := newHandler(s, nil, "", nil)
	for i := 0; i < 4096; i++ {
		h.attempts[string(rune(i))] = attemptWindow{started: time.Now()}
	}
	if h.allowAttempt("new-source") {
		t.Fatal("enrollment map unbounded")
	}
	for key := range h.attempts {
		h.attempts[key] = attemptWindow{started: time.Now().Add(-time.Hour)}
	}
	if !h.allowAttempt("new-source") {
		t.Fatal("expired enrollment budget did not recover")
	}
}

func TestMaintenanceRetentionAndReceiptBudget(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ConsumeSignature("device", []byte("old"), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO bridge_audit VALUES('old','workspace','device','person','session.get','ok','2000-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Maintain(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"request_receipts", "bridge_audit"} {
		var count int
		if err = s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retention %s: %v", table, err)
		}
	}
	if _, err = s.db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2048) INSERT INTO request_receipts SELECT 'device',CAST(x AS TEXT),'2099-01-01' FROM n`); err != nil {
		t.Fatal(err)
	}
	if err = s.ConsumeSignature("device", []byte("new"), time.Now().Add(time.Minute)); err == nil {
		t.Fatal("unbounded receipts")
	}
	if err = s.ConsumeSignature("other", []byte("new"), time.Now().Add(time.Minute)); err != nil {
		t.Fatal("one device blocked another")
	}
	s.Close()
	if err = s.Maintain(context.Background()); err == nil {
		t.Fatal("closed storage healthy")
	}
	if err = s.ConsumeSignature("device", []byte("x"), time.Now()); err == nil {
		t.Fatal("closed storage accepted signature")
	}
	if err = s.verifyEncryptionKey(); err == nil {
		t.Fatal("closed storage verified key")
	}
}

func TestBackupRetainsSevenAndRejectsUnsafeDestinations(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	directory := filepath.Join(root, "backups")
	for i := 0; i < 8; i++ {
		if _, err = s.Backup(context.Background(), directory); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 7 {
		t.Fatalf("retention=%d err=%v", len(entries), err)
	}
	link := filepath.Join(root, "link")
	if err = os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Backup(context.Background(), link); err == nil {
		t.Fatal("backup followed symlink")
	}
	file := filepath.Join(root, "file")
	if err = os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Backup(context.Background(), file); err == nil {
		t.Fatal("backup accepted file directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Backup(ctx, directory); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	if _, err = loadOrCreateBridgeKey(link); err == nil {
		t.Fatal("key followed symlink")
	}
	if _, err = loadOrCreateBridgeKey(filepath.Join(file, "key")); err == nil {
		t.Fatal("key parent file accepted")
	}
}

func TestHostedAssetsAndDocumentationHaveNoIdentityData(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page := hostedPage(invitePage)
	prefix := `src="/invite/assets/`
	_, suffix, found := strings.Cut(page, prefix)
	if !found {
		t.Fatal("invitation assets would require main app authorization")
	}
	asset, _, _ := strings.Cut(suffix, `"`)
	for _, route := range []string{"/invite/assets/" + asset, "/assets/" + asset, "/docs/mcp"} {
		r := httptest.NewRecorder()
		NewHandler(s, nil, "").ServeHTTP(r, httptest.NewRequest(http.MethodGet, route, nil))
		if r.Code != 200 {
			t.Fatalf("route %s = %d", route, r.Code)
		}
	}
	for _, route := range []string{"/assets/nope.js", "/assets/../index.html", "/index.html"} {
		r := httptest.NewRecorder()
		serveAsset(r, httptest.NewRequest(http.MethodGet, route, nil))
		if r.Code != 404 {
			t.Fatalf("unsafe asset %s = %d", route, r.Code)
		}
	}
	policy := hostedCSP()
	if strings.Contains(policy, "script-src 'unsafe-inline'") || !strings.Contains(policy, "'sha256-") || !strings.Contains(policy, "object-src 'none'") {
		t.Fatal("unsafe CSP")
	}
}

func TestRegistrationRequiresPrivateKeyPossession(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	_ = public
	response := httptest.NewRecorder()
	NewHandler(s, nil, "").ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/agent/v1/register", strings.NewReader(`{"name":"Impersonated","public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing proof=%d", response.Code)
	}
}
