// Package bridge anchors ASAP pass 4 findings for hosted session scope.
package bridge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

// ASAP4-F1 (bridge half): the hosted scope check must agree with the Local
// Agent's case-insensitive argument decoding and reject ambiguous names.
func TestInvitationRelayRejectsCaseVariantDuplicateSessionKeys(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	invitation, code, err := store.CreateInvitation(device.ID, "owner@example.com", "reviewer@example.com", "session-a", "view", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.AcceptInvitation(code, "reviewer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []string{
		`{"session_id":"session-a","SESSION_ID":"session-b"}`,
		`{"session_id":"session-a","Session_Id":"session-b"}`,
	} {
		request := protocol.Request{Version: 1, RequestID: "ambiguous", Operation: protocol.SessionGet, Arguments: json.RawMessage(arguments)}
		if _, err = store.SubmitInvitation("reviewer@example.com", invitation.ID, token, request); err == nil {
			t.Fatalf("bridge relayed ambiguous invitation arguments %s", arguments)
		}
	}
}
