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

func TestProtocolStatusMapping(t *testing.T) {
	if statusFor("invalid_request") != http.StatusBadRequest || statusFor("denied") != http.StatusForbidden || statusFor("not_found") != http.StatusNotFound || statusFor("error") != http.StatusInternalServerError {
		t.Fatal("protocol status mapping changed")
	}
}
