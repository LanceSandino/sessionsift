// @module LocalReleaseAssurance rechecks authorization after data access (A05/A06).
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

type revocableAuthorizer struct{ allowed bool }

func (a *revocableAuthorizer) Authorize(context.Context, protocol.Request) error {
	if !a.allowed {
		return errors.New("revoked")
	}
	return nil
}

type revokingRepository struct{ authorizer *revocableAuthorizer }

func (r revokingRepository) GetSession(context.Context, string) (domain.Session, error) {
	r.authorizer.allowed = false
	return domain.Session{ID: "synthetic", Title: "PRIVATE-RESPONSE"}, nil
}
func (r revokingRepository) ListSessions(context.Context) ([]domain.Session, error) { return nil, nil }

func TestA06LocalAgentDoesNotReleaseAfterInFlightRevocation(t *testing.T) {
	authorizer := &revocableAuthorizer{allowed: true}
	result := New(revokingRepository{authorizer}, authorizer).Execute(context.Background(), protocol.Request{Version: 1, RequestID: "inflight", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"synthetic"}`)})
	if result.Status != protocol.StatusDenied || result.Data != nil {
		t.Fatalf("local agent released data after revocation: %+v", result)
	}
}
