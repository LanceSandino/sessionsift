// Package remote anchors ASAP pass 4 findings for local grant scope and state integrity.
package remote

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

// ASAP4-F1: encoding/json matches struct fields case-insensitively and keeps the
// last duplicate, while scope checks read an exact map key. A single-session
// grantee must not be able to name a second session with a case variant.
func TestInviteScopeRejectsCaseVariantDuplicateSessionKeys(t *testing.T) {
	manager, err := OpenManager(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.ApproveInvitePrincipal("invite_scope", "reviewer@example.com", "session-a", "view", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []string{
		`{"session_id":"session-a","SESSION_ID":"session-b"}`,
		`{"session_id":"session-a","Session_Id":"session-b"}`,
		`{"SESSION_ID":"session-b","session_id":"session-a"}`,
	} {
		request := protocol.Request{Version: protocol.Version, RequestID: "scope", Operation: protocol.SessionGet, Arguments: json.RawMessage(arguments)}
		response := service.New(repositoryStub{sessions: []domain.Session{{ID: "session-a", Title: "shared"}, {ID: "session-b", Title: "private"}}},
			NewPrincipalAuthorizer(manager, "reviewer@example.com")).Execute(context.Background(), request)
		if response.Status == protocol.StatusOK {
			if session, ok := response.Data.(domain.Session); ok && session.ID != "session-a" {
				t.Fatalf("arguments %s disclosed %s", arguments, session.ID)
			}
			t.Fatalf("ambiguous arguments %s were accepted", arguments)
		}
	}
}

// ASAP4-F9: the CLI and the running Local Agent share remote-state.json. A
// revocation made by one process must bind the other and must never be undone
// by a later write from stale memory.
func TestRevocationFromAnotherProcessIsEnforcedAndNotResurrected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-state.json")
	running, err := OpenManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = running.ApproveOAuthPrincipal("reader@example.com", []protocol.Operation{protocol.SessionList}, time.Hour); err != nil {
		t.Fatal(err)
	}
	cli, err := OpenManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = cli.RevokePrincipal("reader@example.com"); err != nil {
		t.Fatal(err)
	}
	request := protocol.Request{Version: protocol.Version, RequestID: "after-revoke", Operation: protocol.SessionList}
	if err = running.AuthorizePrincipalRequest(context.Background(), "reader@example.com", request); err == nil {
		t.Fatal("running agent still authorized a principal revoked by another process")
	}
	running.RecordAudit(AuditRecord{ClientID: "reader@example.com", Operation: protocol.SessionList})
	reopened, err := OpenManager(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.GrantForPrincipal("reader@example.com"); err == nil {
		t.Fatal("a stale in-memory write resurrected the revoked grant")
	}
}

// ASAP4-F8: every relayed request rewrites remote-state.json; audit history
// must stay bounded so the state file cannot grow without limit.
func TestRemoteAuditHistoryIsBounded(t *testing.T) {
	manager, err := OpenManager(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	const bound = 5000
	for index := 0; index < bound+25; index++ {
		manager.RecordAudit(AuditRecord{ClientID: "reader@example.com", Operation: protocol.SessionList})
	}
	if count := len(manager.Audit()); count > bound {
		t.Fatalf("audit records=%d exceed bound %d", count, bound)
	}
}
