package bridge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

func TestHandlerSeparatesHumanAndDeviceTrustBoundaries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion != "valid" {
			return accessauth.Principal{}, context.Canceled
		}
		return accessauth.Principal{Subject: "subject", Email: "owner@example.com"}, nil
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("root status=%d", unauthorized.Code)
	}

	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := registerOverHTTP(t, handler, "Laptop", public, private)
	if device.Owner != "" {
		t.Fatalf("self-registered device already owned: %+v", device)
	}
	code, _ := supportcode.Generate()
	codeHash, _ := supportcode.Hash(supportcode.Link, code)
	linkBody, _ := json.Marshal(map[string]any{"code_hash": codeHash, "expires_at": time.Now().Add(5 * time.Minute).UTC()})
	link := httptest.NewRecorder()
	handler.ServeHTTP(link, signedBridgeRequest(t, private, device.ID, http.MethodPost, "/agent/v1/links", linkBody))
	if link.Code != http.StatusCreated {
		t.Fatalf("link status=%d body=%s", link.Code, link.Body.String())
	}
	claimRequest := httptest.NewRequest(http.MethodPost, "/api/v1/devices/link", strings.NewReader(`{"code":"`+code+`"}`))
	claimRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	claim := httptest.NewRecorder()
	handler.ServeHTTP(claim, claimRequest)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, signedBridgeRequest(t, private, device.ID, http.MethodGet, "/agent/v1/device", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"owner":"owner@example.com"`) {
		t.Fatalf("device status=%d body=%s", status.Code, status.Body.String())
	}

	grantBody, _ := json.Marshal(map[string]any{
		"principal": "owner@example.com", "operations": []protocol.Operation{protocol.SessionList},
		"expires_at": time.Now().Add(time.Hour).UTC(),
	})
	grantRequest := signedBridgeRequest(t, private, device.ID, http.MethodPost, "/agent/v1/grants", grantBody)
	grantRecorder := httptest.NewRecorder()
	handler.ServeHTTP(grantRecorder, grantRequest)
	if grantRecorder.Code != http.StatusCreated {
		t.Fatalf("grant status=%d body=%s", grantRecorder.Code, grantRecorder.Body.String())
	}

	protocolBody, _ := json.Marshal(map[string]any{
		"device_id": device.ID,
		"request":   protocol.Request{Version: 1, RequestID: "browser-1", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)},
	})
	protocolRequest := httptest.NewRequest(http.MethodPost, "/api/v1/protocol", bytes.NewReader(protocolBody))
	protocolRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, protocolRequest)
		done <- recorder
	}()

	var task Task
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		nextRequest := signedBridgeRequest(t, private, device.ID, http.MethodGet, "/agent/v1/tasks/next", nil)
		next := httptest.NewRecorder()
		handler.ServeHTTP(next, nextRequest)
		if next.Code == http.StatusOK {
			_ = json.Unmarshal(next.Body.Bytes(), &task)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.ID == "" || task.Principal != "owner@example.com" {
		t.Fatalf("task=%+v", task)
	}
	response := protocol.Response{Version: 1, RequestID: task.Request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"sessions": []any{}}}
	responseJSON, _ := json.Marshal(response)
	completion, _ := json.Marshal(map[string]string{
		"task_id": task.ID, "response": base64.RawURLEncoding.EncodeToString(responseJSON),
		"signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, responseJSON)),
	})
	complete := httptest.NewRecorder()
	handler.ServeHTTP(complete, signedBridgeRequest(t, private, device.ID, http.MethodPost, "/agent/v1/tasks/complete", completion))
	if complete.Code != http.StatusOK {
		t.Fatalf("complete=%d body=%s", complete.Code, complete.Body.String())
	}
	result := <-done
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"status":"ok"`) {
		t.Fatalf("protocol=%d body=%s", result.Code, result.Body.String())
	}
}

func TestPrivateRoutesFailClosedWithoutVerifiedUserIdentity(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion == "empty-principal" {
			return accessauth.Principal{}, nil
		}
		return accessauth.Principal{}, context.Canceled
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")

	privateRoutes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/", ""},
		{http.MethodGet, "/api/v1/me", ""},
		{http.MethodGet, "/api/v1/devices", ""},
		{http.MethodGet, "/api/v1/workspaces", ""},
		{http.MethodPost, "/api/v1/workspaces", `{"name":"Team"}`},
		{http.MethodGet, "/api/v1/workspaces/workspace_private/members", ""},
		{http.MethodPost, "/api/v1/workspaces/workspace_private/members", `{"principal":"member@example.com","role":"member"}`},
		{http.MethodDelete, "/api/v1/workspaces/workspace_private/members/member%40example.com", ""},
		{http.MethodGet, "/api/v1/mcp-devices", ""},
		{http.MethodGet, "/api/v1/mcp-target", ""},
		{http.MethodPut, "/api/v1/mcp-target", `{"workspace_id":"workspace","device_id":"device"}`},
		{http.MethodPost, "/api/v1/devices/link", `{"code":"0000-0000-0000"}`},
		{http.MethodGet, "/support", ""},
		{http.MethodPost, "/support/connect", `{"code":"0000-0000-0000"}`},
		{http.MethodGet, "/support/support_0123456789abcdef", ""},
		{http.MethodPost, "/api/v1/protocol", `{"request":{"version":1,"request_id":"guard","operation":"session.list","arguments":{}}}`},
		{http.MethodPost, "/mcp", `{}`},
		{http.MethodGet, "/invite/invite_private", ""},
	}
	for _, route := range privateRoutes {
		for _, assertion := range []string{"", "empty-principal"} {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			request.Header.Set("Cf-Access-Jwt-Assertion", assertion)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("%s %s assertion=%q status=%d body=%s", route.method, route.path, assertion, recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "sessions") || strings.Contains(recorder.Body.String(), "devices") {
				t.Errorf("%s %s leaked private route data: %s", route.method, route.path, recorder.Body.String())
			}
		}
	}
}

func TestWorkspaceLifecycleAPIsRemainOwnerScoped(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		return accessauth.Principal{Subject: assertion, Email: assertion + "@example.com"}, nil
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")
	request := func(identity, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Cf-Access-Jwt-Assertion", identity)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	created := request("owner", http.MethodPost, "/api/v1/workspaces", `{"name":"Review team"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var workspace Workspace
	if err = json.Unmarshal(created.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	memberPath := "/api/v1/workspaces/" + workspace.ID + "/members"
	added := request("owner", http.MethodPost, memberPath, `{"principal":"member@example.com","role":"member"}`)
	if added.Code != http.StatusCreated {
		t.Fatalf("add=%d body=%s", added.Code, added.Body.String())
	}
	denied := request("member", http.MethodPost, memberPath, `{"principal":"intruder@example.com","role":"member"}`)
	if denied.Code < http.StatusBadRequest {
		t.Fatalf("non-owner add=%d body=%s", denied.Code, denied.Body.String())
	}
	listed := request("owner", http.MethodGet, memberPath, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "member@example.com") {
		t.Fatalf("list=%d body=%s", listed.Code, listed.Body.String())
	}
	removed := request("owner", http.MethodDelete, memberPath+"/member%40example.com", "")
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"removed":true`) {
		t.Fatalf("remove=%d body=%s", removed.Code, removed.Body.String())
	}
	missing := request("owner", http.MethodDelete, memberPath+"/member%40example.com", "")
	if missing.Code < http.StatusBadRequest {
		t.Fatalf("second remove=%d body=%s", missing.Code, missing.Body.String())
	}
	teamKey, _, _ := ed25519.GenerateKey(rand.Reader)
	teamDevice, err := store.RegisterDevice("Team laptop", teamKey)
	if err != nil {
		t.Fatal(err)
	}
	linked := request("owner", http.MethodPost, "/api/v1/devices/link", `{"code":"`+testLinkCode(t, store, teamDevice.ID)+`","workspace_id":"`+workspace.ID+`"}`)
	if linked.Code != http.StatusOK || !strings.Contains(linked.Body.String(), workspace.ID) {
		t.Fatalf("link=%d body=%s", linked.Code, linked.Body.String())
	}
}

func TestHostedPagesUseLayeredSiftAndBoundedControlCenters(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(context.Context, string) (accessauth.Principal, error) {
		return accessauth.Principal{Subject: "owner", Email: "owner@example.com"}, nil
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	root := httptest.NewRecorder()
	handler.ServeHTTP(root, request)
	for _, marker := range []string{`data-mark="layered-sift"`, "Connection control center", "Authorized sessions", "overflow:hidden", "/api/v1/mcp-target", "session.list", "session.get"} {
		if !strings.Contains(root.Body.String(), marker) {
			t.Fatalf("hosted control center missing %q", marker)
		}
	}
	if !strings.Contains(invitePage, `data-mark="layered-sift"`) || !strings.Contains(invitePage, "html,body{height:100%;overflow:hidden}") {
		t.Fatal("invitation page is missing bounded Layered Sift shell")
	}
}

func TestHandlerOperationalRoutesAndRegistrationRateLimit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := newHandler(store, nil, "https://sessionsift.example/", nil)
	for _, path := range []string{"/healthz", "/readyz", "/.well-known/oauth-protected-resource"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
	private := httptest.NewRecorder()
	handler.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/", nil))
	if private.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured verifier status=%d", private.Code)
	}
	for attempt := 0; attempt < 9; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/agent/v1/register", strings.NewReader(`{}`))
		request.RemoteAddr = "192.0.2.10:2345"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if attempt < 8 && recorder.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
		}
		if attempt == 8 && recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("rate limit status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	forwarded := httptest.NewRequest(http.MethodGet, "/", nil)
	forwarded.Header.Set("CF-Connecting-IP", "198.51.100.7")
	if got := remoteIP(forwarded); got != "198.51.100.7" {
		t.Fatalf("forwarded remote IP=%q", got)
	}
	if _, _, ok := workspaceMemberPath("/api/v1/workspaces//members"); ok {
		t.Fatal("malformed member path was accepted")
	}
}

func TestHandlerRequiresExplicitTenantTargetWhenIdentityHasMultipleDevices(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	alpha := enrollTenantDevice(t, store, "alpha@example.com", "Alpha")
	beta := enrollTenantDevice(t, store, "beta@example.com", "Beta")
	for _, device := range []Device{alpha, beta} {
		_, _ = store.RegisterGrant(device.ID, "reviewer@example.com", []protocol.Operation{protocol.SessionList}, time.Now().Add(time.Hour))
	}
	verifier := accessauth.VerifierFunc(func(context.Context, string) (accessauth.Principal, error) {
		return accessauth.Principal{Subject: "reviewer", Email: "reviewer@example.com"}, nil
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")
	authenticated := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Cf-Access-Jwt-Assertion", "valid")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	workspaces := authenticated(http.MethodGet, "/api/v1/workspaces", "")
	if workspaces.Code != http.StatusOK || !strings.Contains(workspaces.Body.String(), alpha.WorkspaceID) || !strings.Contains(workspaces.Body.String(), beta.WorkspaceID) {
		t.Fatalf("workspaces=%d body=%s", workspaces.Code, workspaces.Body.String())
	}
	ambiguous := authenticated(http.MethodGet, "/api/v1/mcp-target", "")
	if ambiguous.Code < http.StatusBadRequest || !strings.Contains(ambiguous.Body.String(), "choose an MCP target") {
		t.Fatalf("ambiguous=%d body=%s", ambiguous.Code, ambiguous.Body.String())
	}
	cross := authenticated(http.MethodPut, "/api/v1/mcp-target", `{"workspace_id":"`+alpha.WorkspaceID+`","device_id":"`+beta.ID+`"}`)
	if cross.Code < http.StatusBadRequest {
		t.Fatalf("cross-tenant target=%d body=%s", cross.Code, cross.Body.String())
	}
	selected := authenticated(http.MethodPut, "/api/v1/mcp-target", `{"workspace_id":"`+alpha.WorkspaceID+`","device_id":"`+alpha.ID+`"}`)
	if selected.Code != http.StatusOK {
		t.Fatalf("selected=%d body=%s", selected.Code, selected.Body.String())
	}
	resolved := authenticated(http.MethodGet, "/api/v1/mcp-target", "")
	if resolved.Code != http.StatusOK || !strings.Contains(resolved.Body.String(), alpha.ID) || strings.Contains(resolved.Body.String(), beta.ID) {
		t.Fatalf("resolved=%d body=%s", resolved.Code, resolved.Body.String())
	}
}

func TestInvitationAcceptanceSetsSecureSessionAndServesOnlyInvitee(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	invitation, code, err := store.CreateInvitation(device.ID, "owner@example.com", "reviewer@example.com", "session-a", "view", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion == "reviewer" {
			return accessauth.Principal{Subject: "reviewer-sub", Email: "reviewer@example.com"}, nil
		}
		if assertion == "other" {
			return accessauth.Principal{Subject: "other-sub", Email: "other@example.com"}, nil
		}
		return accessauth.Principal{}, context.Canceled
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")
	acceptRequest := httptest.NewRequest(http.MethodGet, "/invite/accept/"+code, nil)
	acceptRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, acceptRequest)
	if accepted.Code != http.StatusSeeOther || accepted.Header().Get("Location") != "/invite/"+invitation.ID {
		t.Fatalf("accepted=%d headers=%v body=%s", accepted.Code, accepted.Header(), accepted.Body.String())
	}
	cookies := accepted.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookies=%+v", cookies)
	}
	pageRequest := httptest.NewRequest(http.MethodGet, "/invite/"+invitation.ID, nil)
	pageRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	pageRequest.AddCookie(cookies[0])
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, pageRequest)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Shared session") {
		t.Fatalf("page=%d body=%s", page.Code, page.Body.String())
	}
	otherRequest := httptest.NewRequest(http.MethodGet, "/invite/"+invitation.ID, nil)
	otherRequest.Header.Set("Cf-Access-Jwt-Assertion", "other")
	otherRequest.AddCookie(cookies[0])
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, otherRequest)
	if other.Code != http.StatusForbidden {
		t.Fatalf("other identity status=%d", other.Code)
	}
}

func TestInvitationAgentLifecycleAndSessionProtocol(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion == "reviewer" {
			return accessauth.Principal{Subject: "reviewer-sub", Email: "reviewer@example.com"}, nil
		}
		return accessauth.Principal{}, context.Canceled
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")

	createBody, _ := json.Marshal(map[string]any{
		"owner": "owner@example.com", "invitee": "reviewer@example.com", "session_id": "session-a", "role": "view",
		"expires_at": time.Now().Add(time.Hour).UTC(),
	})
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, signedBridgeRequest(t, private, device.ID, http.MethodPost, "/agent/v1/invitations", createBody))
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var envelope struct {
		Invitation Invitation `json:"invitation"`
		AcceptURL  string     `json:"accept_url"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil || envelope.Invitation.ID == "" || envelope.AcceptURL == "" {
		t.Fatalf("envelope=%+v err=%v", envelope, err)
	}
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, signedBridgeRequest(t, private, device.ID, http.MethodGet, "/agent/v1/invitations", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), envelope.Invitation.ID) {
		t.Fatalf("list=%d body=%s", listed.Code, listed.Body.String())
	}

	code := envelope.AcceptURL[strings.LastIndex(envelope.AcceptURL, "/")+1:]
	acceptRequest := httptest.NewRequest(http.MethodGet, "/invite/accept/"+code, nil)
	acceptRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, acceptRequest)
	cookies := accepted.Result().Cookies()
	if accepted.Code != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatalf("accept=%d cookies=%+v body=%s", accepted.Code, cookies, accepted.Body.String())
	}

	metadataRequest := httptest.NewRequest(http.MethodGet, "/invite/"+envelope.Invitation.ID+"/metadata", nil)
	metadataRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	metadataRequest.AddCookie(cookies[0])
	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, metadataRequest)
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"session_id":"session-a"`) {
		t.Fatalf("metadata=%d body=%s", metadata.Code, metadata.Body.String())
	}

	request := protocol.Request{Version: protocol.Version, RequestID: "invite-protocol", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"session-a"}`)}
	requestBody, _ := json.Marshal(request)
	crossSiteRequest := httptest.NewRequest(http.MethodPost, "/invite/"+envelope.Invitation.ID+"/protocol", bytes.NewReader(requestBody))
	crossSiteRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	crossSiteRequest.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSiteRequest.AddCookie(cookies[0])
	crossSite := httptest.NewRecorder()
	handler.ServeHTTP(crossSite, crossSiteRequest)
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d body=%s", crossSite.Code, crossSite.Body.String())
	}

	protocolRequest := httptest.NewRequest(http.MethodPost, "/invite/"+envelope.Invitation.ID+"/protocol", bytes.NewReader(requestBody))
	protocolRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	protocolRequest.Header.Set("Origin", "https://sessionsift.example")
	protocolRequest.AddCookie(cookies[0])
	protocolDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, protocolRequest)
		protocolDone <- recorder
	}()
	var task Task
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		next := httptest.NewRecorder()
		handler.ServeHTTP(next, signedBridgeRequest(t, private, device.ID, http.MethodGet, "/agent/v1/tasks/next", nil))
		if next.Code == http.StatusOK {
			_ = json.Unmarshal(next.Body.Bytes(), &task)
			break
		}
		time.Sleep(time.Millisecond)
	}
	if task.ID == "" || task.Request.Operation != protocol.SessionGet {
		t.Fatalf("task=%+v", task)
	}
	responseJSON, _ := json.Marshal(protocol.Response{Version: protocol.Version, RequestID: request.RequestID, Status: protocol.StatusOK, Data: map[string]any{"id": "session-a"}})
	completion, _ := json.Marshal(map[string]string{
		"task_id": task.ID, "response": base64.RawURLEncoding.EncodeToString(responseJSON),
		"signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, responseJSON)),
	})
	completed := httptest.NewRecorder()
	handler.ServeHTTP(completed, signedBridgeRequest(t, private, device.ID, http.MethodPost, "/agent/v1/tasks/complete", completion))
	if completed.Code != http.StatusOK {
		t.Fatalf("complete=%d body=%s", completed.Code, completed.Body.String())
	}
	result := <-protocolDone
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"status":"ok"`) {
		t.Fatalf("protocol=%d body=%s", result.Code, result.Body.String())
	}

	revoked := httptest.NewRecorder()
	handler.ServeHTTP(revoked, signedBridgeRequest(t, private, device.ID, http.MethodDelete, "/agent/v1/invitations/"+envelope.Invitation.ID, nil))
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke=%d body=%s", revoked.Code, revoked.Body.String())
	}
	afterRequest := httptest.NewRequest(http.MethodGet, "/invite/"+envelope.Invitation.ID+"/metadata", nil)
	afterRequest.Header.Set("Cf-Access-Jwt-Assertion", "reviewer")
	afterRequest.AddCookie(cookies[0])
	after := httptest.NewRecorder()
	handler.ServeHTTP(after, afterRequest)
	if after.Code < http.StatusBadRequest {
		t.Fatalf("revoked session status=%d body=%s", after.Code, after.Body.String())
	}
}

func TestHandlerPublishesOAuthDiscoveryAndRejectsForgedDeviceRequest(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(context.Context, string) (accessauth.Principal, error) {
		return accessauth.Principal{}, context.Canceled
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")

	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"resource":"https://sessionsift.example/mcp"`) {
		t.Fatalf("metadata=%d body=%s", metadata.Code, metadata.Body.String())
	}
	forged := httptest.NewRecorder()
	handler.ServeHTTP(forged, httptest.NewRequest(http.MethodGet, "/agent/v1/tasks/next", nil))
	if forged.Code != http.StatusUnauthorized {
		t.Fatalf("forged status=%d", forged.Code)
	}
	mcp := httptest.NewRecorder()
	handler.ServeHTTP(mcp, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))
	if mcp.Code != http.StatusUnauthorized || !strings.Contains(mcp.Header().Get("WWW-Authenticate"), "oauth-protected-resource") {
		t.Fatalf("mcp=%d headers=%v", mcp.Code, mcp.Header())
	}
}

func TestHandlerHealthConsoleDevicesMCPAndRevocation(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion != "valid" {
			return accessauth.Principal{}, context.Canceled
		}
		return accessauth.Principal{Subject: "owner", Email: "owner@example.com"}, nil
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")

	for _, path := range []string{"/healthz", "/readyz"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
	rootRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	root := httptest.NewRecorder()
	handler.ServeHTTP(root, rootRequest)
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), "Connection control center") {
		t.Fatalf("root=%d body=%s", root.Code, root.Body.String())
	}
	meRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	me := httptest.NewRecorder()
	handler.ServeHTTP(me, meRequest)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "owner@example.com") {
		t.Fatalf("me=%d body=%s", me.Code, me.Body.String())
	}
	noDeviceProtocol := httptest.NewRequest(http.MethodPost, "/api/v1/protocol", strings.NewReader(`{"request":{"version":1,"request_id":"none","operation":"session.list","arguments":{}}}`))
	noDeviceProtocol.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	noDevice := httptest.NewRecorder()
	handler.ServeHTTP(noDevice, noDeviceProtocol)
	if noDevice.Code != http.StatusForbidden {
		t.Fatalf("no-device protocol=%d body=%s", noDevice.Code, noDevice.Body.String())
	}
	noDeviceMCP := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	noDeviceMCP.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	noMCP := httptest.NewRecorder()
	handler.ServeHTTP(noMCP, noDeviceMCP)
	if noMCP.Code != http.StatusForbidden {
		t.Fatalf("no-device mcp=%d body=%s", noMCP.Code, noMCP.Body.String())
	}
	emptyDevicesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	emptyDevicesRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	emptyDevices := httptest.NewRecorder()
	handler.ServeHTTP(emptyDevices, emptyDevicesRequest)
	if emptyDevices.Code != http.StatusOK || strings.TrimSpace(emptyDevices.Body.String()) != "[]" {
		t.Fatalf("empty devices=%d body=%s", emptyDevices.Code, emptyDevices.Body.String())
	}

	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	_, _ = store.RegisterGrant(device.ID, "owner@example.com", protocol.ReadOperations(), time.Now().Add(time.Hour))
	devicesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	devicesRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	devices := httptest.NewRecorder()
	handler.ServeHTTP(devices, devicesRequest)
	if devices.Code != http.StatusOK || !strings.Contains(devices.Body.String(), device.ID) {
		t.Fatalf("devices=%d body=%s", devices.Code, devices.Body.String())
	}

	mcpRequest := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	mcpRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	mcpRequest.Header.Set("Content-Type", "application/json")
	mcpRequest.Header.Set("Accept", "application/json, text/event-stream")
	mcp := httptest.NewRecorder()
	handler.ServeHTTP(mcp, mcpRequest)
	if mcp.Code != http.StatusOK || !strings.Contains(mcp.Body.String(), "sessionsift") {
		t.Fatalf("mcp=%d body=%s", mcp.Code, mcp.Body.String())
	}

	revokeBody := []byte(`{"principal":"owner@example.com"}`)
	revokeRequest := signedBridgeRequest(t, private, device.ID, http.MethodDelete, "/agent/v1/grants", revokeBody)
	revoked := httptest.NewRecorder()
	handler.ServeHTTP(revoked, revokeRequest)
	if revoked.Code != http.StatusOK {
		t.Fatalf("grant revoke=%d body=%s", revoked.Code, revoked.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/"+device.ID, nil)
	deleteRequest.Header.Set("Cf-Access-Jwt-Assertion", "valid")
	deleted := httptest.NewRecorder()
	handler.ServeHTTP(deleted, deleteRequest)
	if deleted.Code != http.StatusOK {
		t.Fatalf("device delete=%d body=%s", deleted.Code, deleted.Body.String())
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing=%d", missing.Code)
	}
	uppercaseMCP := httptest.NewRecorder()
	handler.ServeHTTP(uppercaseMCP, httptest.NewRequest(http.MethodPost, "/Mcp", strings.NewReader(`{}`)))
	if uppercaseMCP.Code != http.StatusNotFound {
		t.Fatalf("uppercase MCP status=%d", uppercaseMCP.Code)
	}
}

func TestHandlerBoundsRegistrationAndBadRequests(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	handler := NewHandler(store, nil, "")
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/agent/v1/register", strings.NewReader(`{"unexpected":true}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad registration=%d", bad.Code)
	}
	for attempt := 0; attempt < 8; attempt++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/agent/v1/register", strings.NewReader(`{"name":"Laptop","public_key":"bad"}`))
		handler.ServeHTTP(recorder, request)
		if attempt == 7 && recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d status=%d", attempt+1, recorder.Code)
		}
	}
}

func TestHandlerRejectsMalformedSignedDeviceActionsAndRelayDenial(t *testing.T) {
	store, _ := Open(filepath.Join(t.TempDir(), "bridge.db"))
	defer store.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := testOwnedDevice(t, store, "owner@example.com", "Laptop", public)
	handler := NewHandler(store, nil, "")

	for _, test := range []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/agent/v1/tasks/complete", []byte(`{"task_id":"x","response":"!","signature":"!"}`)},
		{http.MethodPost, "/agent/v1/grants", []byte(`{"principal":`)},
		{http.MethodGet, "/agent/v1/unknown", nil},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, signedBridgeRequest(t, private, device.ID, test.method, test.path, test.body))
		if recorder.Code < 400 {
			t.Fatalf("%s status=%d body=%s", test.path, recorder.Code, recorder.Body.String())
		}
	}
	request := protocol.Request{Version: 1, RequestID: "denied", Operation: protocol.SessionList, Arguments: json.RawMessage(`{}`)}
	response := (relayExecutor{store: store, principal: "owner@example.com", deviceID: device.ID}).Execute(context.Background(), request)
	if response.Status != protocol.StatusError || response.Error == nil || response.Error.Code != "relay_denied" {
		t.Fatalf("relay response=%+v", response)
	}
}

// registerOverHTTP self-registers a device with key-possession proof.
func registerOverHTTP(t *testing.T, handler http.Handler, name string, public ed25519.PublicKey, private ed25519.PrivateKey) Device {
	t.Helper()
	encoded := base64.RawURLEncoding.EncodeToString(public)
	proofMessage, _ := json.Marshal([]string{"sessionsift.register.v1", name, encoded})
	body, _ := json.Marshal(map[string]string{"name": name, "public_key": encoded, "proof": base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, proofMessage))})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agent/v1/register", bytes.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var device Device
	if err := json.Unmarshal(recorder.Body.Bytes(), &device); err != nil || device.ID == "" {
		t.Fatalf("device=%+v err=%v", device, err)
	}
	return device
}

func signedBridgeRequest(t *testing.T, private ed25519.PrivateKey, deviceID, method, path string, body []byte) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	message := deviceSignatureMessage(method, path, timestamp, body)
	request.Header.Set("X-SessionSift-Device", deviceID)
	request.Header.Set("X-SessionSift-Timestamp", timestamp)
	request.Header.Set("X-SessionSift-Signature", base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, message)))
	return request
}
