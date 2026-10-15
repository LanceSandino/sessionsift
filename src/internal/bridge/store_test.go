// Package bridge tests the durable hosted relay registry and queue.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

func TestStoreEnrollsDeviceAndRoutesOnlyGrantedOperations(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	registered, err := store.RegisterDevice("Developer laptop", publicKey)
	if err != nil || registered.Owner != "" {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}
	code := testLinkCode(t, store, registered.ID)
	device, err := store.ClaimDeviceLink("Owner@Example.com", code, "")
	if err != nil || device.Owner != "owner@example.com" || device.ID != registered.ID {
		t.Fatalf("device=%+v err=%v", device, err)
	}
	if _, err = store.ClaimDeviceLink("owner@example.com", code, ""); err == nil {
		t.Fatal("single-use link code was replayed")
	}
	grant, err := store.RegisterGrant(device.ID, "reviewer@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	if err != nil || grant.DeviceID != device.ID {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	granted, err := store.GrantOperations("reviewer@example.com", device.ID)
	if err != nil || len(granted) != 1 || granted[0] != protocol.SessionList {
		t.Fatalf("grant discovery=%v err=%v", granted, err)
	}
	request := protocol.Request{Version: 1, RequestID: "req-1", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	task, err := store.Submit("reviewer@example.com", device.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Submit("reviewer@example.com", device.ID, protocol.Request{Version: 1, RequestID: "req-2", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"s1"}`)}); err == nil {
		t.Fatal("ungranted operation was queued")
	}
	claimed, found, err := store.Next(device.ID)
	if err != nil || !found || claimed.ID != task.ID || claimed.Principal != "reviewer@example.com" {
		t.Fatalf("claimed=%+v found=%v err=%v", claimed, found, err)
	}
	response := protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"sessions": []any{}}}
	encoded, _ := json.Marshal(response)
	if err = store.Complete(device.ID, task.ID, encoded, ed25519.Sign(privateKey, encoded)); err != nil {
		t.Fatal(err)
	}
	got, err := store.Wait(context.Background(), task.ID, time.Second)
	if err != nil || got.Status != protocol.StatusOK {
		t.Fatalf("response=%+v err=%v", got, err)
	}
	if err = store.Complete(device.ID, task.ID, encoded, []byte("forged")); err == nil {
		t.Fatal("forged completion was accepted")
	}
}

func TestInvitationIsEmailBoundSingleUseAndSessionScoped(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)

	invitation, code, err := store.CreateInvitation(device.ID, "owner@example.com", "Reviewer@Example.com", "session-a", "comment", time.Now().Add(time.Hour))
	if err != nil || code == "" || invitation.Role != "comment" {
		t.Fatalf("invitation=%+v code=%q err=%v", invitation, code, err)
	}
	if _, _, err = store.AcceptInvitation(code, "wrong@example.com"); err == nil {
		t.Fatal("invitation accepted by the wrong identity")
	}
	accepted, token, err := store.AcceptInvitation(code, "reviewer@example.com")
	if err != nil || token == "" || accepted.AcceptedAt == nil {
		t.Fatalf("accepted=%+v token=%q err=%v", accepted, token, err)
	}
	if _, _, err = store.AcceptInvitation(code, "reviewer@example.com"); err == nil {
		t.Fatal("single-use invitation was replayed")
	}
	listed, err := store.InvitationsForDevice(device.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != invitation.ID || listed[0].AcceptedAt == nil {
		t.Fatalf("listed invitations=%+v err=%v", listed, err)
	}

	allowed := protocol.Request{Version: 1, RequestID: "invite-ok", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"session-a"}`)}
	if _, err = store.SubmitInvitation("reviewer@example.com", invitation.ID, token, allowed); err != nil {
		t.Fatalf("allowed invitation request: %v", err)
	}
	wrongSession := protocol.Request{Version: 1, RequestID: "invite-wrong", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"session-b"}`)}
	if _, err = store.SubmitInvitation("reviewer@example.com", invitation.ID, token, wrongSession); err == nil {
		t.Fatal("invitation accessed a different session")
	}
	listing := protocol.Request{Version: 1, RequestID: "invite-list", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	if _, err = store.SubmitInvitation("reviewer@example.com", invitation.ID, token, listing); err == nil {
		t.Fatal("invitation listed sessions")
	}
	if err = store.RevokeInvitation(device.ID, invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SubmitInvitation("reviewer@example.com", invitation.ID, token, allowed); err == nil {
		t.Fatal("revoked invitation remained active")
	}
	if err = store.RevokeInvitation(device.ID, invitation.ID); err == nil {
		t.Fatal("revoked invitation was revoked twice")
	}
}

func TestInvitationValidationRejectsUnsafeScope(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	for _, test := range []struct {
		owner, invitee, session, role string
		expires                       time.Time
	}{
		{"bad", "reviewer@example.com", "s1", "view", time.Now().Add(time.Hour)},
		{"owner@example.com", "bad", "s1", "view", time.Now().Add(time.Hour)},
		{"owner@example.com", "reviewer@example.com", "", "view", time.Now().Add(time.Hour)},
		{"owner@example.com", "reviewer@example.com", "s1", "admin", time.Now().Add(time.Hour)},
		{"owner@example.com", "reviewer@example.com", "s1", "view", time.Now().Add(31 * 24 * time.Hour)},
		{"owner@example.com", "reviewer@example.com", "s1", "view", time.Now().Add(-time.Hour)},
	} {
		if _, _, err := store.CreateInvitation(device.ID, test.owner, test.invitee, test.session, test.role, test.expires); err == nil {
			t.Fatalf("accepted invalid invitation: %+v", test)
		}
	}
	if _, _, err := store.CreateInvitation("missing", "owner@example.com", "reviewer@example.com", "s1", "view", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("invitation accepted a missing device")
	}
	if values, err := store.InvitationsForDevice("missing"); err != nil || len(values) != 0 {
		t.Fatalf("missing device invitations=%v err=%v", values, err)
	}
}

func TestStorePersistsDevicesGrantsAndHealth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", publicKey)
	_, _ = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	_ = store.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	devices, err := reopened.DevicesFor("owner@example.com")
	if err != nil || len(devices) != 1 || devices[0].ID != device.ID {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
	if err = reopened.RevokeGrant(device.ID, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Submit("owner@example.com", device.ID, protocol.Request{Version: 1, RequestID: "denied", Operation: protocol.SessionList}); err == nil {
		t.Fatal("revoked grant was accepted")
	}
}

func TestStoreRejectsInvalidEnrollmentGrantAndTaskInputs(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.RegisterDevice("Laptop", []byte("short")); err == nil {
		t.Fatal("invalid public key was accepted")
	}
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = store.RegisterDevice("", public); err == nil {
		t.Fatal("empty device name was accepted")
	}
	registered, err := store.RegisterDevice("Laptop", public)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ClaimDeviceLink("not-an-email", testLinkCode(t, store, registered.ID), ""); err == nil {
		t.Fatal("invalid link owner was accepted")
	}
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	if _, err = store.RegisterGrant(device.ID, "owner@example.com", nil, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("empty operation grant was accepted")
	}
	if _, err = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{"unknown"}, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("unknown operation grant was accepted")
	}
	if _, err = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(-time.Hour)); err == nil {
		t.Fatal("expired grant was accepted")
	}
	if _, err = store.RegisterGrant("missing", "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("missing device grant was accepted")
	}
	if _, found, err := store.Next(device.ID); err != nil || found {
		t.Fatalf("empty queue found=%v err=%v", found, err)
	}
	if _, err = store.Submit("owner@example.com", device.ID, protocol.Request{}); err == nil {
		t.Fatal("invalid protocol request was accepted")
	}
	if _, err = store.Wait(context.Background(), "missing", time.Second); err == nil {
		t.Fatal("missing task wait succeeded")
	}
}

func TestStoreListsAndRevokesOwnedDeviceAndTimesOutTasks(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	_, _ = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	owned, err := store.OwnedDevices("owner@example.com")
	if err != nil || len(owned) != 1 || owned[0].ID != device.ID {
		t.Fatalf("owned=%+v err=%v", owned, err)
	}
	request := protocol.Request{Version: 1, RequestID: "timeout", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	task, _ := store.Submit("owner@example.com", device.ID, request)
	if _, err = store.Wait(context.Background(), task.ID, time.Millisecond); err == nil {
		t.Fatal("task wait did not time out")
	}
	if err = store.RevokeDevice(device.ID, "someone@example.com"); err == nil {
		t.Fatal("non-owner revoked device")
	}
	if err = store.RevokeDevice(device.ID, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DeviceKey(device.ID); err == nil {
		t.Fatal("revoked device key remained active")
	}
	if devices, err := store.OwnedDevices("owner@example.com"); err != nil || len(devices) != 0 {
		t.Fatalf("revoked devices=%+v err=%v", devices, err)
	}
}

func TestStoreRejectsMismatchedAndUnclaimedCompletions(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	_, _ = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	request := protocol.Request{Version: 1, RequestID: "completion", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	task, _ := store.Submit("owner@example.com", device.ID, request)
	response := protocol.Response{Version: 1, RequestID: request.RequestID, Status: protocol.StatusOK}
	encoded, _ := json.Marshal(response)
	if err := store.Complete(device.ID, task.ID, encoded, ed25519.Sign(private, encoded)); err == nil {
		t.Fatal("unclaimed task was completed")
	}
	_, _, _ = store.Next(device.ID)
	if err := store.Complete(device.ID, task.ID, []byte("not-json"), nil); err == nil {
		t.Fatal("invalid JSON completion was accepted")
	}
	wrong := protocol.Response{Version: 1, RequestID: "wrong", Status: protocol.StatusOK}
	wrongJSON, _ := json.Marshal(wrong)
	if err := store.Complete(device.ID, task.ID, wrongJSON, ed25519.Sign(private, wrongJSON)); err == nil {
		t.Fatal("mismatched completion was accepted")
	}
	if err := store.Complete(device.ID, "missing", encoded, ed25519.Sign(private, encoded)); err == nil {
		t.Fatal("missing task completion was accepted")
	}
}
