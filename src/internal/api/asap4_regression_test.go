// Package api anchors ASAP pass 4 findings for the localhost trust boundary.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/remote"
)

// ASAP4-F2a: a DNS-rebinding page reaches 127.0.0.1 with its own Host header.
// Session data must never be served to a non-loopback Host.
func TestLocalAPIRejectsForeignHostHeaders(t *testing.T) {
	handler := Handler(testStore(t))
	for _, host := range []string{"attacker.example", "attacker.example:7777", "127.0.0.1.attacker.example:7777", "sessionsift.local:7777"} {
		request := localRequest(http.MethodGet, "/api/v1/sessions", nil)
		request.Host = host
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "Test session") {
			t.Fatalf("host %q status=%d body=%s", host, response.Code, response.Body.String())
		}
	}
}

// ASAP4-F2b: a cross-site page can POST text/plain JSON to localhost without a
// CORS preflight. Owner mutations must reject foreign browser origins.
func TestLocalOwnerAPIRejectsCrossSiteMutations(t *testing.T) {
	directory := t.TempDir()
	manager, err := remote.OpenManager(filepath.Join(directory, "remote-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remote.LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := HandlerWithRemote(testStore(t), manager, identity)
	attacks := []struct{ origin, site, contentType string }{
		{"https://attacker.example", "cross-site", "text/plain"},
		{"http://attacker.example:7777", "same-origin", "text/plain"},
		{"null", "", "text/plain"},
		{"", "cross-site", "application/x-www-form-urlencoded"},
	}
	for _, attack := range attacks {
		body := `{"principal":"attacker@example.com","operations":["session.list","session.get"],"duration_days":30}`
		request := localRequest(http.MethodPost, "/api/v1/remote/oauth/grants", strings.NewReader(body))
		request.Host = "127.0.0.1:7777"
		request.Header.Set("Content-Type", attack.contentType)
		if attack.origin != "" {
			request.Header.Set("Origin", attack.origin)
		}
		if attack.site != "" {
			request.Header.Set("Sec-Fetch-Site", attack.site)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("attack %+v status=%d body=%s", attack, response.Code, response.Body.String())
		}
	}
	if grants := manager.OAuthGrants(); len(grants) != 0 {
		t.Fatalf("cross-site request created grants: %+v", grants)
	}
}

// ASAP4-F5: hook payloads carry assistant messages, instructions and absolute
// paths in keys the receiver previously stored verbatim.
func TestHookReceiverRedactsEveryUnknownPayloadKey(t *testing.T) {
	store := testStore(t)
	handler := Handler(store)
	body := `{"session_id":"hook-leak","hook_event_name":"Stop","last_assistant_message":"use password=hunter2secret now","custom_instructions":"api_key=abc123456789xyz","agent_transcript_path":"/Users/alice/.claude/projects/p/x.jsonl","nested":{"token":"sk-abcdefghijklmnopqrstuvwx"}}` // gitleaks:allow -- synthetic redaction fixture
	request := localRequest(http.MethodPost, "/internal/hooks/codex", strings.NewReader(body))
	request.Host = "127.0.0.1:7777"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	session, err := store.GetSession(context.Background(), "hook-leak")
	if err != nil || len(session.Events) != 1 {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	stored := strings.ToLower(stringify(session.Events[0].PayloadRedacted))
	for _, secret := range []string{"hunter2secret", "abc123456789xyz", "/users/alice", "sk-" + "abcdefghijklmnopqrstuvwx"} { // gitleaks:allow -- synthetic redaction fixture
		if strings.Contains(stored, secret) {
			t.Fatalf("hook event retained %q: %s", secret, stored)
		}
	}
}

func stringify(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// ASAP4-F7: the owner UI must not be frameable by another origin.
func TestLocalUIForbidsFraming(t *testing.T) {
	handler := Handler(testStore(t))
	request := localRequest(http.MethodGet, "/remote-access", nil)
	request.Host = "127.0.0.1:7777"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers=%v", response.Header())
	}
}

// The public tunnel hostname is accepted only in explicit Docker tunnel mode.
func TestTunnelHostRequiresExplicitTunnelMode(t *testing.T) {
	t.Setenv("SESSIONSIFT_PUBLIC_URL", "https://sessionsift.example")
	for _, enabled := range []string{"false", "true"} {
		t.Setenv("SESSIONSIFT_ALLOW_DOCKER_TUNNEL", enabled)
		handler := Handler(testStore(t))
		request := localRequest(http.MethodGet, "/healthz", nil)
		request.Host = "sessionsift.example"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if want := map[string]int{"false": http.StatusForbidden, "true": http.StatusOK}[enabled]; response.Code != want {
			t.Fatalf("tunnel=%s status=%d", enabled, response.Code)
		}
	}
}

// Same-origin owner requests and non-browser clients keep working.
func TestLocalGuardAllowsSameOriginAndNonBrowserClients(t *testing.T) {
	directory := t.TempDir()
	manager, _ := remote.OpenManager(filepath.Join(directory, "remote-state.json"))
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	handler := HandlerWithRemote(testStore(t), manager, identity)
	for index, origin := range []string{"http://127.0.0.1:7777", "http://localhost:7777", ""} {
		host := "127.0.0.1:7777"
		if strings.Contains(origin, "localhost") {
			host = "localhost:7777"
		}
		request := localRequest(http.MethodPost, "/api/v1/remote/pairings", strings.NewReader(`{}`))
		request.Host = host
		if origin != "" {
			request.Header.Set("Origin", origin)
			request.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("case %d origin=%q status=%d body=%s", index, origin, response.Code, response.Body.String())
		}
	}
	mismatched := localRequest(http.MethodPost, "/api/v1/remote/pairings", strings.NewReader(`{}`))
	mismatched.Header.Set("Origin", "http://localhost:7777")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, mismatched)
	if response.Code != http.StatusForbidden {
		t.Fatalf("origin/host mismatch status=%d", response.Code)
	}
	wrongType := localRequest(http.MethodPost, "/api/v1/remote/pairings", strings.NewReader(`{}`))
	wrongType.Header.Set("Content-Type", "text/plain")
	typed := httptest.NewRecorder()
	handler.ServeHTTP(typed, wrongType)
	if typed.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON owner mutation status=%d", typed.Code)
	}
}
