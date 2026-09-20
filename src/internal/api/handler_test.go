package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Now().UTC()
	session := domain.Session{ID: "s1", ExternalSessionID: "s1", Title: "Test session", StartedAt: now, EndedAt: now.Add(time.Minute), ParserVersion: domain.ParserVersion, Turns: []domain.Turn{{ID: "t1", SessionID: "s1", Sequence: 1, InputTokens: 100, CachedInputTokens: 50}}, ToolCalls: []domain.ToolCall{{ID: "c1", SessionID: "s1", Success: false}}}
	if err := store.SaveImport(context.Background(), "path", "fingerprint", session); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestSessionAndCompareRoutes(t *testing.T) {
	handler := Handler(testStore(t))
	for _, target := range []string{"/api/v1/sessions", "/api/v1/sessions/s1", "/api/v1/sessions/s1/turns", "/api/v1/sessions/s1/events", "/api/v1/sessions/s1/findings", "/api/v1/sessions/s1/metrics", "/api/v1/compare?a=s1&b=s1", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Errorf("%s status=%d body=%s", target, res.Code, res.Body.String())
		}
	}
}

func TestSessionListHidesInternalReviewsByDefault(t *testing.T) {
	store := testStore(t)
	now := time.Now().UTC()
	review := domain.Session{ID: "review", ExternalSessionID: "review", Title: "Auto-review approval assessment", Kind: domain.InternalReviewSession, StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}
	if err := store.SaveImport(context.Background(), "review-path", "fingerprint", review); err != nil {
		t.Fatal(err)
	}
	handler := Handler(store)
	for _, item := range []struct {
		target string
		want   int
	}{{"/api/v1/sessions", 1}, {"/api/v1/sessions?include_internal=true", 2}} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, item.target, nil))
		var sessions []domain.Session
		if err := json.Unmarshal(res.Body.Bytes(), &sessions); err != nil || len(sessions) != item.want {
			t.Fatalf("target=%s sessions=%d err=%v body=%s", item.target, len(sessions), err, res.Body.String())
		}
	}
}

func TestHookReceiverRedactsAndValidates(t *testing.T) {
	store := testStore(t)
	handler := Handler(store)
	body := `{"session_id":"live","turn_id":"turn","hook_event_name":"PostToolUse","tool_response":"Bearer abcdefghijklmnopqrstuvwxyz"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/hooks/codex", strings.NewReader(body))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	bad := httptest.NewRequest(http.MethodPost, "/internal/hooks/codex", strings.NewReader(`{"x":1}`))
	badRes := httptest.NewRecorder()
	handler.ServeHTTP(badRes, bad)
	if badRes.Code != http.StatusBadRequest {
		t.Fatalf("bad status=%d", badRes.Code)
	}
}

func TestMissingAndStaticRoutes(t *testing.T) {
	handler := Handler(testStore(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/missing", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status=%d", res.Code)
	}
	home := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRes := httptest.NewRecorder()
	handler.ServeHTTP(homeRes, home)
	if homeRes.Code != http.StatusOK || !strings.Contains(homeRes.Body.String(), "SessionSift") {
		t.Fatalf("home status=%d", homeRes.Code)
	}
	var value map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &value)
	if value["error"] == nil {
		t.Fatal("expected JSON error")
	}
}

func TestWebShellRefreshCannotUseStaleBootAssets(t *testing.T) {
	handler := Handler(testStore(t))

	for _, target := range []string{"/", "/sessions/s1", "/remote-access"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
			t.Errorf("%s Cache-Control=%q", target, got)
		}
		if !strings.Contains(response.Body.String(), "SessionSift is loading") {
			t.Errorf("%s does not contain the visible boot fallback", target)
		}
	}

	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	start := strings.Index(home.Body.String(), `src="/assets/`)
	if start < 0 {
		t.Fatal("built index does not reference a hashed script")
	}
	start += len(`src="`)
	end := strings.Index(home.Body.String()[start:], `"`)
	if end < 0 {
		t.Fatal("built index script reference is malformed")
	}
	assetPath := home.Body.String()[start : start+end]
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, assetPath, nil))
	if asset.Code != http.StatusOK {
		t.Fatalf("asset %s status=%d", assetPath, asset.Code)
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control=%q", got)
	}

	stale := httptest.NewRecorder()
	handler.ServeHTTP(stale, httptest.NewRequest(http.MethodGet, "/assets/index-stale.js", nil))
	if stale.Code != http.StatusNotFound {
		t.Fatalf("stale asset status=%d", stale.Code)
	}
	if got := stale.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Errorf("stale asset Cache-Control=%q", got)
	}
}

func TestBadRoutesAndOTelAcceptance(t *testing.T) {
	handler := Handler(testStore(t))
	cases := []struct {
		method, path string
		status       int
		body         string
	}{{http.MethodGet, "/api/v1/compare?a=missing&b=s1", 400, ""}, {http.MethodGet, "/api/v1/sessions/s1/nope", 404, ""}, {http.MethodPost, "/internal/hooks/codex", 400, "not-json"}, {http.MethodPost, "/v1/logs", 202, ""}, {http.MethodPost, "/v1/metrics", 202, ""}, {http.MethodDelete, "/nope", 404, ""}}
	for _, item := range cases {
		req := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != item.status {
			t.Errorf("%s %s status=%d body=%s", item.method, item.path, res.Code, res.Body.String())
		}
	}
}

func TestTypedSessionProtocolEndpoint(t *testing.T) {
	handler := Handler(testStore(t))
	request := `{"version":1,"request_id":"req_http","operation":"session.list","arguments":{"include_internal":false}}`
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/v1/protocol", strings.NewReader(request)))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"status":"ok"`) || !strings.Contains(res.Body.String(), `"title":"Test session"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	unsafe := httptest.NewRecorder()
	handler.ServeHTTP(unsafe, httptest.NewRequest(http.MethodPost, "/api/v1/protocol", strings.NewReader(`{"version":1,"request_id":"req_unsafe","operation":"shell.exec"}`)))
	if unsafe.Code != http.StatusBadRequest || !strings.Contains(unsafe.Body.String(), `"status":"invalid_request"`) {
		t.Fatalf("unsafe status=%d body=%s", unsafe.Code, unsafe.Body.String())
	}
}

func TestClosedStoreReturnsInternalError(t *testing.T) {
	store := testStore(t)
	_ = store.Close()
	handler := Handler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", res.Code)
	}
}

func TestReadinessChecksDatabaseAndLivenessDoesNot(t *testing.T) {
	store := testStore(t)
	handler := Handler(store)
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"status":"ready"`) {
		t.Fatalf("ready=%d %s", ready.Code, ready.Body.String())
	}
	_ = store.Close()
	unready := httptest.NewRecorder()
	handler.ServeHTTP(unready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if unready.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready=%d %s", unready.Code, unready.Body.String())
	}
	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("live=%d", live.Code)
	}
}

func TestProtocolStatusMapping(t *testing.T) {
	if statusFor("invalid_request") != http.StatusBadRequest || statusFor("denied") != http.StatusForbidden || statusFor("not_found") != http.StatusNotFound || statusFor("error") != http.StatusInternalServerError {
		t.Fatal("protocol status mapping changed")
	}
}

func TestRemotePairingApprovalProtocolAndCollaborationRoutes(t *testing.T) {
	store := testStore(t)
	directory := t.TempDir()
	manager, err := remote.OpenManager(filepath.Join(directory, "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remote.LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := HandlerWithRemote(store, manager, identity)
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	created := call(http.MethodPost, "/api/v1/remote/pairings", `{}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var pairing remote.Pairing
	_ = json.Unmarshal(created.Body.Bytes(), &pairing)
	claimed := call(http.MethodPost, "/api/v1/remote/pairings/claim", `{"code":"`+pairing.Code+`","reviewer":"reviewer","operations":["session.list","session.get"]}`, "")
	if claimed.Code != http.StatusOK {
		t.Fatalf("claim=%d %s", claimed.Code, claimed.Body.String())
	}
	approved := call(http.MethodPost, "/api/v1/remote/pairings/approve", `{"code":"`+pairing.Code+`","duration_minutes":30}`, "")
	if approved.Code != http.StatusOK {
		t.Fatalf("approve=%d %s", approved.Code, approved.Body.String())
	}
	var grant struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(approved.Body.Bytes(), &grant)
	read := call(http.MethodPost, "/remote/v1/protocol", `{"version":1,"request_id":"remote-http","operation":"session.list","arguments":{}}`, grant.Token)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"Test session"`) {
		t.Fatalf("read=%d %s", read.Code, read.Body.String())
	}
	mcpRead := call(http.MethodPost, "/remote/v1/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}`, grant.Token)
	if mcpRead.Code != http.StatusOK || !strings.Contains(mcpRead.Body.String(), `"Test session"`) {
		t.Fatalf("mcp=%d %s", mcpRead.Code, mcpRead.Body.String())
	}
	denied := call(http.MethodPost, "/remote/v1/protocol", `{"version":1,"request_id":"remote-denied","operation":"session.metrics","arguments":{"session_id":"s1"}}`, grant.Token)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied=%d %s", denied.Code, denied.Body.String())
	}
	annotation := call(http.MethodPost, "/api/v1/remote/annotations", `{"session_id":"s1","turn_id":"t1","author":"reviewer","body":"Check this"}`, "")
	if annotation.Code != http.StatusCreated {
		t.Fatalf("annotation=%d", annotation.Code)
	}
	review := call(http.MethodPost, "/api/v1/remote/reviews", `{"session_id":"s1","reviewer":"reviewer","status":"approved","summary":"Looks good"}`, "")
	if review.Code != http.StatusCreated {
		t.Fatalf("review=%d", review.Code)
	}
	status := call(http.MethodGet, "/api/v1/remote/status", "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), identity.ID) || !strings.Contains(status.Body.String(), "reviewer") {
		t.Fatalf("status=%d %s", status.Code, status.Body.String())
	}
}

func TestRemoteRouteErrorsListsAndKillSwitches(t *testing.T) {
	store := testStore(t)
	directory := t.TempDir()
	manager, _ := remote.OpenManager(filepath.Join(directory, "remote.json"))
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	handler := HandlerWithRemote(store, manager, identity)
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, item := range []struct {
		method, path, body string
		status             int
	}{{http.MethodPost, "/remote/v1/protocol", `{}`, http.StatusUnauthorized}, {http.MethodPost, "/remote/v1/mcp", `{}`, http.StatusUnauthorized}, {http.MethodPost, "/api/v1/remote/pairings/claim", `bad`, http.StatusBadRequest}, {http.MethodPost, "/api/v1/remote/grants/revoke", `{"grant_id":"missing"}`, http.StatusBadRequest}, {http.MethodPost, "/api/v1/remote/connections/disconnect", `{"connection_id":"missing"}`, http.StatusBadRequest}, {http.MethodGet, "/api/v1/remote/missing", "", http.StatusNotFound}} {
		response := call(item.method, item.path, item.body, "")
		if response.Code != item.status {
			t.Fatalf("%s status=%d body=%s", item.path, response.Code, response.Body.String())
		}
	}
	pairing, _ := manager.CreatePairing(identity.ID)
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", []protocol.Operation{protocol.SessionList})
	grant, token, _ := manager.ApprovePairing(pairing.Code, time.Hour)
	connections := manager.Connections()
	if response := call(http.MethodPost, "/remote/v1/protocol", `bad`, token); response.Code != http.StatusBadRequest {
		t.Fatalf("bad protocol=%d", response.Code)
	}
	if response := call(http.MethodPost, "/remote/v1/mcp", `not-json`, token); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "parse error") {
		t.Fatalf("bad mcp=%d %s", response.Code, response.Body.String())
	}
	if response := call(http.MethodPost, "/api/v1/remote/connections/disconnect", `{"connection_id":"`+connections[0].ID+`"}`, ""); response.Code != http.StatusOK {
		t.Fatalf("disconnect=%d", response.Code)
	}
	if response := call(http.MethodPost, "/api/v1/remote/grants/revoke", `{"grant_id":"`+grant.ID+`"}`, ""); response.Code != http.StatusOK {
		t.Fatalf("revoke=%d", response.Code)
	}
	for _, path := range []string{"/api/v1/remote/annotations?session_id=s1", "/api/v1/remote/reviews?session_id=s1"} {
		if response := call(http.MethodGet, path, "", ""); response.Code != http.StatusOK {
			t.Fatalf("list=%d", response.Code)
		}
	}
}

func TestRemoteMCPTransportConformanceAndCollaboration(t *testing.T) {
	t.Setenv("SESSIONSIFT_ALLOWED_ORIGINS", "https://review.example")
	store := testStore(t)
	directory := t.TempDir()
	manager, _ := remote.OpenManager(filepath.Join(directory, "remote.json"))
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	pairing, _ := manager.CreatePairing(identity.ID)
	operations := []protocol.Operation{protocol.SessionList, protocol.AnnotationList, protocol.AnnotationCreate, protocol.ReviewList, protocol.ReviewCreate}
	_, _ = manager.ClaimPairing(pairing.Code, "reviewer", operations)
	_, token, _ := manager.ApprovePairing(pairing.Code, time.Hour)
	handler := HandlerWithRemote(store, manager, identity)
	call := func(method, path, body, bearer, origin, version string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		if version != "" {
			request.Header.Set("MCP-Protocol-Version", version)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	for _, item := range []struct {
		method, body, bearer, origin, version string
		status                                int
	}{
		{http.MethodGet, "", "", "", "", http.StatusMethodNotAllowed},
		{http.MethodOptions, "", "", "https://review.example", "", http.StatusNoContent},
		{http.MethodPost, `{}`, token, "https://attacker.example", "", http.StatusForbidden},
		{http.MethodPost, `{}`, token, "", "1900-01-01", http.StatusBadRequest},
		{http.MethodPost, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, token, "", "", http.StatusBadRequest},
		{http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, token, "", "", http.StatusAccepted},
		{http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "bad", "", "", http.StatusUnauthorized},
	} {
		response := call(item.method, "/remote/v1/mcp", item.body, item.bearer, item.origin, item.version)
		if response.Code != item.status {
			t.Fatalf("method=%s status=%d want=%d body=%s", item.method, response.Code, item.status, response.Body.String())
		}
		if item.status >= 400 && !strings.Contains(response.Body.String(), `"jsonrpc":"2.0"`) {
			t.Fatalf("non JSON-RPC refusal: %s", response.Body.String())
		}
	}
	annotation := call(http.MethodPost, "/remote/v1/annotations", `{"session_id":"s1","author":"reviewer","body":"note"}`, token, "", "")
	if annotation.Code != http.StatusCreated {
		t.Fatalf("annotation=%d %s", annotation.Code, annotation.Body.String())
	}
	if response := call(http.MethodGet, "/remote/v1/annotations?session_id=s1", "", token, "", ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "note") {
		t.Fatalf("annotations=%d %s", response.Code, response.Body.String())
	}
	review := call(http.MethodPost, "/remote/v1/reviews", `{"session_id":"s1","reviewer":"reviewer","status":"approved","summary":"good"}`, token, "", "")
	if review.Code != http.StatusCreated {
		t.Fatalf("review=%d %s", review.Code, review.Body.String())
	}
	if response := call(http.MethodGet, "/remote/v1/reviews?session_id=s1", "", token, "", ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "good") {
		t.Fatalf("reviews=%d %s", response.Code, response.Body.String())
	}
	if response := call(http.MethodPut, "/remote/v1/reviews", "", token, "", ""); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d", response.Code)
	}
}
