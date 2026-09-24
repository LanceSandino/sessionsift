// Package bridge tests explicit workspace isolation and MCP target selection.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
)

func enrollTenantDevice(t *testing.T, store *Store, owner, name string) Device {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device := testOwnedDevice(t, store, owner, name, public)
	if device.WorkspaceID != workspaceIDFor(owner) {
		t.Fatalf("missing personal workspace scope: device=%+v", device)
	}
	return device
}

func TestStoreIsolatesWorkspacesAndRequiresExplicitMCPSelection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alpha := enrollTenantDevice(t, store, "alpha@example.com", "Alpha laptop")
	beta := enrollTenantDevice(t, store, "beta@example.com", "Beta laptop")
	if err = store.RevokeDevice(beta.ID, "alpha@example.com"); err == nil {
		t.Fatal("one tenant revoked another tenant's device")
	}
	if _, _, err = store.CreateInvitation(beta.ID, "alpha@example.com", "reviewer@example.com", "session-b", "view", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("one tenant invited through another tenant's device")
	}
	alphaWorkspaces, err := store.WorkspacesFor("alpha@example.com")
	if err != nil || len(alphaWorkspaces) != 1 || alphaWorkspaces[0].ID != alpha.WorkspaceID {
		t.Fatalf("alpha workspaces=%+v err=%v", alphaWorkspaces, err)
	}
	operations := []protocol.Operation{protocol.SessionList}
	for _, device := range []Device{alpha, beta} {
		if _, err = store.RegisterGrant(device.ID, "reviewer@example.com", operations, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err = store.ResolveMCPTarget("reviewer@example.com"); err == nil {
		t.Fatal("ambiguous MCP target was selected implicitly")
	}
	if err = store.SetMCPTarget("reviewer@example.com", alpha.WorkspaceID, beta.ID); err == nil {
		t.Fatal("cross-workspace device target was accepted")
	}
	if err = store.SetMCPTarget("reviewer@example.com", alpha.WorkspaceID, alpha.ID); err != nil {
		t.Fatal(err)
	}
	target, err := store.ResolveMCPTarget("reviewer@example.com")
	if err != nil || target.DeviceID != alpha.ID || target.WorkspaceID != alpha.WorkspaceID {
		t.Fatalf("target=%+v err=%v", target, err)
	}

	request := protocol.Request{Version: 1, RequestID: "tenant", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	task, err := store.Submit("reviewer@example.com", alpha.ID, request)
	if err != nil || task.WorkspaceID != alpha.WorkspaceID {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	var stored []byte
	if err = store.db.QueryRow(`SELECT request_json FROM tasks WHERE id=?`, task.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if json.Valid(stored) {
		t.Fatalf("task request remained plaintext JSON: %s", stored)
	}
}

func TestStoreAutoSelectsOnlyEligibleDeviceAndInvalidatesRevokedTarget(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	device := enrollTenantDevice(t, store, "owner@example.com", "Laptop")
	_, _ = store.RegisterGrant(device.ID, "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	target, err := store.ResolveMCPTarget("owner@example.com")
	if err != nil || target.DeviceID != device.ID {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if err = store.RevokeDevice(device.ID, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveMCPTarget("owner@example.com"); err == nil {
		t.Fatal("revoked MCP target remained eligible")
	}
}

func TestWorkspaceOwnerControlsMembershipAndScopedEnrollment(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, err := store.CreateWorkspace("owner@example.com", "Review team")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Owner != "owner@example.com" || workspace.Role != "owner" {
		t.Fatalf("workspace=%+v", workspace)
	}
	member, err := store.AddMembership("owner@example.com", workspace.ID, "member@example.com", "member")
	if err != nil || member.Principal != "member@example.com" || member.Role != "member" {
		t.Fatalf("member=%+v err=%v", member, err)
	}
	if _, err = store.AddMembership("member@example.com", workspace.ID, "intruder@example.com", "member"); err == nil {
		t.Fatal("non-owner changed workspace membership")
	}
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	registered, err := store.RegisterDevice("Team laptop", public)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ClaimDeviceLink("member@example.com", testLinkCode(t, store, registered.ID), workspace.ID); err == nil {
		t.Fatal("ordinary member linked a device into the workspace")
	}
	linked, err := store.ClaimDeviceLink("owner@example.com", testLinkCode(t, store, registered.ID), workspace.ID)
	if err != nil || linked.WorkspaceID != workspace.ID || linked.Owner != "owner@example.com" {
		t.Fatalf("linked=%+v err=%v", linked, err)
	}
	members, err := store.MembersFor("owner@example.com", workspace.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("members=%+v err=%v", members, err)
	}
	if err = store.RemoveMembership("owner@example.com", workspace.ID, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	if err = store.RemoveMembership("owner@example.com", workspace.ID, "owner@example.com"); err == nil {
		t.Fatal("workspace owner membership was removed")
	}
}

func TestOpenEncryptsLegacyRelayPayloadsInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE devices (
		id TEXT PRIMARY KEY, owner TEXT NOT NULL, name TEXT NOT NULL,
		public_key BLOB NOT NULL, created_at TEXT NOT NULL, last_seen TEXT NOT NULL,
		revoked_at TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE tasks (
		id TEXT PRIMARY KEY, device_id TEXT NOT NULL, principal TEXT NOT NULL,
		request_json BLOB NOT NULL, response_json BLOB, status TEXT NOT NULL,
		created_at TEXT NOT NULL, claimed_at TEXT, completed_at TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT INTO devices(id,owner,name,public_key,created_at,last_seen) VALUES(?,?,?,?,?,?)`,
		"device-legacy", "owner@example.com", "Legacy laptop", make([]byte, ed25519.PublicKeySize), now, now)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON := []byte(`{"version":1,"request_id":"legacy","operation":"session.list","arguments":{}}`)
	responseJSON := []byte(`{"version":1,"request_id":"legacy","status":"ok","data":{"sessions":[]}}`)
	_, err = db.Exec(`INSERT INTO tasks(id,device_id,principal,request_json,response_json,status,created_at)
		VALUES(?,?,?,?,?,'complete',?)`, "legacy-task", "device-legacy", "owner@example.com", requestJSON, responseJSON, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var requestEnvelope, responseEnvelope []byte
	if err = store.db.QueryRowContext(context.Background(), `SELECT request_json,response_json FROM tasks WHERE id='legacy-task'`).Scan(&requestEnvelope, &responseEnvelope); err != nil {
		t.Fatal(err)
	}
	if json.Valid(requestEnvelope) || json.Valid(responseEnvelope) || !store.cipher.IsEnvelope(requestEnvelope) || !store.cipher.IsEnvelope(responseEnvelope) {
		t.Fatalf("legacy relay payloads were not encrypted: request=%x response=%x", requestEnvelope, responseEnvelope)
	}
	openedRequest, err := store.cipher.Open(requestEnvelope)
	if err != nil || string(openedRequest) != string(requestJSON) {
		t.Fatalf("request=%s err=%v", openedRequest, err)
	}
	openedResponse, err := store.cipher.Open(responseEnvelope)
	if err != nil || string(openedResponse) != string(responseJSON) {
		t.Fatalf("response=%s err=%v", openedResponse, err)
	}
}

func TestWorkspaceValidationFailsClosedAndDiscoveryIsStable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateWorkspace("not-an-email", "Team"); err == nil {
		t.Fatal("invalid owner email was accepted")
	}
	if _, err = store.CreateWorkspace("owner@example.com", " "); err == nil {
		t.Fatal("blank workspace name was accepted")
	}
	if _, err = store.CreateWorkspace("owner@example.com", string(make([]byte, 81))); err == nil {
		t.Fatal("oversized workspace name was accepted")
	}
	workspaces, err := store.WorkspacesFor("new-owner@example.com")
	if err != nil || len(workspaces) != 1 || workspaces[0].Role != "owner" {
		t.Fatalf("workspaces=%+v err=%v", workspaces, err)
	}
	stable, err := store.WorkspacesFor("NEW-OWNER@example.com")
	if err != nil || len(stable) != 1 || stable[0].ID != workspaces[0].ID {
		t.Fatalf("stable=%+v err=%v", stable, err)
	}
	workspace := workspaces[0]
	if _, err = store.AddMembership(workspace.Owner, workspace.ID, "member@example.com", "viewer"); err == nil {
		t.Fatal("invalid membership role was accepted")
	}
	if _, err = store.AddMembership(workspace.Owner, workspace.ID, workspace.Owner, "admin"); err == nil {
		t.Fatal("owner role was changed")
	}
	if _, err = store.MembersFor("outsider@example.com", workspace.ID); err == nil {
		t.Fatal("outsider listed workspace members")
	}
	if err = store.RemoveMembership(workspace.Owner, workspace.ID, "missing@example.com"); err == nil {
		t.Fatal("missing membership removal succeeded")
	}
	if _, err = store.ClaimDeviceLink("outsider@example.com", "0000-0000-0000", workspace.ID); err == nil {
		t.Fatal("outsider linked a device into the workspace")
	}
	if _, err = store.ResolveMCPTarget(workspace.Owner); err == nil {
		t.Fatal("identity without a grant resolved an MCP target")
	}
	if err = store.SetMCPTarget(workspace.Owner, workspace.ID, "missing-device"); err == nil {
		t.Fatal("missing device became an MCP target")
	}
	if _, err = store.DeviceKey("missing-device"); err == nil {
		t.Fatal("missing device returned a key")
	}
}

func TestStoreFailsClosedWhenBridgeDatabaseIsUnavailable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	request := protocol.Request{Version: protocol.Version, RequestID: "closed", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	checks := []struct {
		name string
		run  func() error
	}{
		{"health", func() error { return store.Health(context.Background()) }},
		{"create workspace", func() error { _, value := store.CreateWorkspace("owner@example.com", "Team"); return value }},
		{"add member", func() error {
			_, value := store.AddMembership("owner@example.com", "workspace", "member@example.com", "member")
			return value
		}},
		{"list members", func() error { _, value := store.MembersFor("owner@example.com", "workspace"); return value }},
		{"remove member", func() error { return store.RemoveMembership("owner@example.com", "workspace", "member@example.com") }},
		{"register device", func() error {
			_, value := store.RegisterDevice("Laptop", make(ed25519.PublicKey, ed25519.PublicKeySize))
			return value
		}},
		{"create link", func() error {
			return store.CreateDeviceLink("device", strings.Repeat("a", 64), time.Now().Add(time.Minute))
		}},
		{"claim link", func() error {
			_, value := store.ClaimDeviceLink("owner@example.com", "0000-0000-0000", "")
			return value
		}},
		{"register support", func() error {
			_, value := store.RegisterSupport("device", "support_0123456789abcdef", strings.Repeat("a", 64), "session", "view", time.Now().Add(time.Minute), 30)
			return value
		}},
		{"find support", func() error { _, value := store.FindWaitingSupport("0000-0000-0000"); return value }},
		{"list support", func() error { _, value := store.SupportForDevice("device"); return value }},
		{"create invitation", func() error {
			_, _, value := store.CreateInvitation("device", "owner@example.com", "reviewer@example.com", "session", "view", time.Now().Add(time.Hour))
			return value
		}},
		{"accept invitation", func() error { _, _, value := store.AcceptInvitation("code", "owner@example.com"); return value }},
		{"list invitations", func() error { _, value := store.InvitationsForDevice("device"); return value }},
		{"revoke invitation", func() error { return store.RevokeInvitation("device", "invite") }},
		{"register grant", func() error {
			_, value := store.RegisterGrant("device", "owner@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
			return value
		}},
		{"revoke grant", func() error { return store.RevokeGrant("device", "owner@example.com") }},
		{"list granted devices", func() error { _, value := store.DevicesFor("owner@example.com"); return value }},
		{"list owned devices", func() error { _, value := store.OwnedDevices("owner@example.com"); return value }},
		{"list workspaces", func() error { _, value := store.WorkspacesFor("owner@example.com"); return value }},
		{"set target", func() error { return store.SetMCPTarget("owner@example.com", "workspace", "device") }},
		{"resolve target", func() error { _, value := store.ResolveMCPTarget("owner@example.com"); return value }},
		{"device key", func() error { _, value := store.DeviceKey("device"); return value }},
		{"revoke device", func() error { return store.RevokeDevice("device", "owner@example.com") }},
		{"submit", func() error { _, value := store.Submit("owner@example.com", "device", request); return value }},
		{"submit invitation", func() error {
			_, value := store.SubmitInvitation("reviewer@example.com", "invite", "token", request)
			return value
		}},
		{"next task", func() error { _, _, value := store.Next("device"); return value }},
		{"complete task", func() error {
			return store.Complete("device", "task", []byte(`{"version":1,"request_id":"closed","status":"ok"}`), nil)
		}},
		{"wait", func() error { _, value := store.Wait(context.Background(), "task", time.Millisecond); return value }},
		{"ensure column", func() error {
			return store.ensureColumn(context.Background(), "tasks", "workspace_id", "workspace_id TEXT")
		}},
		{"backfill scopes", func() error { return store.backfillWorkspaceScopes(context.Background()) }},
		{"encrypt legacy payloads", func() error { return store.encryptLegacyTaskPayloads(context.Background()) }},
		{"migrate", func() error { return store.migrate(context.Background()) }},
	}
	for _, check := range checks {
		if err = check.run(); err == nil {
			t.Errorf("%s succeeded with a closed database", check.name)
		}
	}
}
