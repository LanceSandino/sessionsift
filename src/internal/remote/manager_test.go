// Package remote tests pairing, grants, revocation, audit, and collaboration state.
package remote

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

func TestPairingRequiresClaimAndOwnerApproval(t *testing.T) {
	manager, err := OpenManager(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	pairing, err := manager.CreatePairing("device-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.ClaimPairing(pairing.Code, "reviewer@example.com", []protocol.Operation{protocol.SessionList, protocol.SessionMetrics}); err != nil {
		t.Fatal(err)
	}
	grant, token, err := manager.ApprovePairing(pairing.Code, time.Hour)
	if err != nil || token == "" {
		t.Fatalf("grant=%+v token=%q err=%v", grant, token, err)
	}
	if err = manager.Authorize(context.Background(), token, protocol.SessionMetrics); err != nil {
		t.Fatal(err)
	}
	if err = manager.Authorize(context.Background(), token, protocol.SessionGet); err == nil {
		t.Fatal("ungranted capability was accepted")
	}
	if err = manager.Revoke(grant.ID); err != nil {
		t.Fatal(err)
	}
	if err = manager.Authorize(context.Background(), token, protocol.SessionList); err == nil {
		t.Fatal("revoked grant was accepted")
	}
}

func TestEmptyManagerCollectionsAreArrays(t *testing.T) {
	manager, err := OpenManager(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manager.Connections() == nil || manager.PendingPairings() == nil || manager.Audit() == nil || manager.Annotations("missing") == nil || manager.Reviews("missing") == nil {
		t.Fatal("empty manager collections must serialize as JSON arrays, not null")
	}
}

func TestManagerValidationConnectionsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-state.json")
	manager, err := OpenManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.CreatePairing(""); err == nil {
		t.Fatal("empty device accepted")
	}
	pairing, _ := manager.CreatePairing("device")
	if _, _, err = manager.ApprovePairing(pairing.Code, time.Hour); err == nil {
		t.Fatal("unclaimed pairing approved")
	}
	if _, err = manager.ClaimPairing(pairing.Code, "", []protocol.Operation{protocol.SessionList}); err == nil {
		t.Fatal("empty reviewer accepted")
	}
	if _, err = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{"shell.exec"}); err == nil {
		t.Fatal("unknown operation accepted")
	}
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	if len(manager.PendingPairings()) != 1 {
		t.Fatal("pending pairing missing")
	}
	if _, _, err = manager.ApprovePairing(pairing.Code, 25*time.Hour); err == nil {
		t.Fatal("long grant accepted")
	}
	grant, token, err := manager.ApprovePairing(pairing.Code, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	connections := manager.Connections()
	if len(connections) != 1 || !connections[0].Active {
		t.Fatalf("connections=%v", connections)
	}
	if _, ok := manager.GrantForToken(token); !ok {
		t.Fatal("grant lookup failed")
	}
	if _, ok := manager.GrantForToken("bad"); ok {
		t.Fatal("bad grant found")
	}
	if err = manager.Disconnect("missing"); err == nil {
		t.Fatal("missing connection disconnected")
	}
	if err = manager.Disconnect(connections[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = manager.Revoke("missing"); err == nil {
		t.Fatal("missing grant revoked")
	}
	if _, err = manager.AddAnnotation("", "", "", ""); err == nil {
		t.Fatal("invalid annotation accepted")
	}
	if _, err = manager.AddReview("", "", "", ""); err == nil {
		t.Fatal("invalid review accepted")
	}
	reopened, err := OpenManager(path)
	if err != nil || len(reopened.Connections()) != 1 || grant.ID == "" {
		t.Fatalf("reopen err=%v", err)
	}
}

func TestExpiredCodesAttemptLimitsAuditAndCollaboration(t *testing.T) {
	manager, err := OpenManager(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manager.now = func() time.Time { return now }
	pairing, _ := manager.CreatePairing("device")
	for range 5 {
		_, _ = manager.ClaimPairing("000-000", "bad", []protocol.Operation{protocol.SessionList})
	}
	if _, err = manager.ClaimPairing("000-000", "bad", []protocol.Operation{protocol.SessionList}); err == nil {
		t.Fatal("attempt limit was not enforced")
	}
	now = now.Add(11 * time.Minute)
	if _, err = manager.ClaimPairing(pairing.Code, "late", []protocol.Operation{protocol.SessionList}); err == nil {
		t.Fatal("expired code was accepted")
	}
	manager.RecordAudit(AuditRecord{ClientID: "reviewer", Operation: protocol.SessionList, Allowed: true, Status: "ok", ResponseBytes: 20})
	annotation, err := manager.AddAnnotation("s1", "t1", "reviewer", "Check token handling")
	if err != nil || annotation.ID == "" {
		t.Fatal(err)
	}
	review, err := manager.AddReview("s1", "reviewer", "changes_requested", "Please add a regression test")
	if err != nil || review.ID == "" {
		t.Fatal(err)
	}
	if len(manager.Audit()) != 1 || len(manager.Annotations("s1")) != 1 || len(manager.Reviews("s1")) != 1 {
		t.Fatal("persisted records missing")
	}
}

func TestPairingDenialAuthenticationRateLimitAndDisconnectRevocation(t *testing.T) {
	manager, _ := OpenManager(filepath.Join(t.TempDir(), "state.json"))
	denied, _ := manager.CreatePairing("device")
	_, _ = manager.ClaimPairing(denied.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	if err := manager.DenyPairing(denied.Code); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.ApprovePairing(denied.Code, time.Hour); err == nil {
		t.Fatal("denied pairing was approved")
	}
	if err := manager.DenyPairing("missing"); err == nil {
		t.Fatal("missing pairing denied")
	}
	pairing, _ := manager.CreatePairing("device")
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	_, token, _ := manager.ApprovePairing(pairing.Code, time.Hour)
	if _, err := manager.Authenticate(token); err != nil {
		t.Fatal(err)
	}
	for range 120 {
		if _, _, err := manager.AllowRequest(token); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := manager.AllowRequest(token); err == nil {
		t.Fatal("rate limit not enforced")
	}
	connection := manager.Connections()[0]
	if err := manager.Disconnect(connection.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authenticate(token); err == nil {
		t.Fatal("disconnect did not revoke grant")
	}
}
