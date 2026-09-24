// @module BridgeSupportTests verifies code-based support connections end to end at the bridge.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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

type supportHarness struct {
	t       *testing.T
	store   *Store
	handler http.Handler
	device  Device
	private ed25519.PrivateKey
}

func newSupportHarness(t *testing.T) *supportHarness {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion == "" {
			return accessauth.Principal{}, context.Canceled
		}
		return accessauth.Principal{Subject: assertion, Email: assertion}, nil
	})
	handler := NewHandler(store, verifier, "https://sessionsift.example")
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	device := registerOverHTTP(t, handler, "Customer laptop", public, private)
	return &supportHarness{t: t, store: store, handler: handler, device: device, private: private}
}

// issue registers a device-generated support code the way the Local Agent does.
func (h *supportHarness) issue(sessionID, role string, minutes int) (string, string) {
	h.t.Helper()
	code, _ := supportcode.Generate()
	hash, _ := supportcode.Hash(supportcode.Support, code)
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	id := "support_" + hex.EncodeToString(buffer)
	body, _ := json.Marshal(map[string]any{"id": id, "code_hash": hash, "session_id": sessionID, "role": role, "code_expires_at": time.Now().Add(10 * time.Minute).UTC(), "access_minutes": minutes})
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, signedBridgeRequest(h.t, h.private, h.device.ID, http.MethodPost, "/agent/v1/support", body))
	if recorder.Code != http.StatusCreated || !strings.Contains(recorder.Body.String(), `"connect_url":"https://sessionsift.example/support"`) {
		h.t.Fatalf("issue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	return id, code
}

func (h *supportHarness) human(principal, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Cf-Access-Jwt-Assertion", principal)
	request.Header.Set("Origin", "https://sessionsift.example")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder
}

// serveDevice answers the next relayed task like a Local Agent would.
func (h *supportHarness) serveDevice(answer func(Task) protocol.Response) {
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			next := httptest.NewRecorder()
			h.handler.ServeHTTP(next, signedBridgeRequest(h.t, h.private, h.device.ID, http.MethodGet, "/agent/v1/tasks/next", nil))
			if next.Code != http.StatusOK {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			var task Task
			_ = json.Unmarshal(next.Body.Bytes(), &task)
			response := answer(task)
			encoded, _ := json.Marshal(response)
			completion, _ := json.Marshal(map[string]string{"task_id": task.ID, "response": base64.RawURLEncoding.EncodeToString(encoded), "signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(h.private, encoded))})
			done := httptest.NewRecorder()
			h.handler.ServeHTTP(done, signedBridgeRequest(h.t, h.private, h.device.ID, http.MethodPost, "/agent/v1/tasks/complete", completion))
			return
		}
	}()
}

func confirmRedeem(task Task) protocol.Response {
	var arguments struct {
		SupportID string `json:"support_id"`
		Code      string `json:"code"`
	}
	_ = json.Unmarshal(task.Request.Arguments, &arguments)
	return protocol.Response{Version: 1, RequestID: task.Request.RequestID, Status: protocol.StatusOK, Data: map[string]any{
		"id": arguments.SupportID, "session_id": "session-a", "role": "view", "supporter": task.Principal, "expires_at": time.Now().Add(30 * time.Minute).UTC(),
	}}
}

// The code-entry page must not load the shared hosted bundle: its layout CSS
// forces every <main> to full width, which stretched the entry card.
func TestSupportEntryPageKeepsItsOwnLayout(t *testing.T) {
	h := newSupportHarness(t)
	page := h.human("helper@example.com", http.MethodGet, "/support", "")
	if page.Code != http.StatusOK || page.Body.String() != supportPage || strings.Contains(page.Body.String(), "/support/assets/") {
		t.Fatalf("support entry page should be served without the hosted bundle: %d", page.Code)
	}
}

func TestSupportCodeConnectsExactlyOneSupporterToOneSession(t *testing.T) {
	h := newSupportHarness(t)
	id, code := h.issue("session-a", "view", 30)

	if miss := h.human("helper@example.com", http.MethodPost, "/support/connect", `{"code":"0000-0000-0000"}`); miss.Code != http.StatusNotFound {
		t.Fatalf("unknown code status=%d body=%s", miss.Code, miss.Body.String())
	}
	var redeemed Task
	h.serveDevice(func(task Task) protocol.Response {
		redeemed = task
		return confirmRedeem(task)
	})
	connect := h.human("helper@example.com", http.MethodPost, "/support/connect", `{"code":"`+strings.ToLower(code)+`"}`)
	if connect.Code != http.StatusOK || !strings.Contains(connect.Body.String(), `"redirect":"/support/`+id+`"`) {
		t.Fatalf("connect status=%d body=%s", connect.Code, connect.Body.String())
	}
	if redeemed.Request.Operation != protocol.SupportRedeem || redeemed.Principal != "helper@example.com" {
		t.Fatalf("device received %+v", redeemed)
	}
	cookies := connect.Result().Cookies()
	if len(cookies) != 1 || !strings.HasPrefix(cookies[0].Name, supportCookiePrefix) || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("cookies=%+v", cookies)
	}
	cookie := cookies[0]

	if again := h.human("other@example.com", http.MethodPost, "/support/connect", `{"code":"`+code+`"}`); again.Code != http.StatusNotFound {
		t.Fatalf("used code status=%d body=%s", again.Code, again.Body.String())
	}
	if page := h.human("helper@example.com", http.MethodGet, "/support/"+id, "", cookie); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "/support/assets/") && !strings.Contains(page.Body.String(), "Shared session") {
		t.Fatalf("viewer status=%d", page.Code)
	}
	metadata := h.human("helper@example.com", http.MethodGet, "/support/"+id+"/metadata", "", cookie)
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"kind":"support"`) || !strings.Contains(metadata.Body.String(), `"session_id":"session-a"`) || strings.Contains(metadata.Body.String(), "Customer laptop") {
		t.Fatalf("metadata status=%d body=%s", metadata.Code, metadata.Body.String())
	}
	if stolen := h.human("other@example.com", http.MethodGet, "/support/"+id+"/metadata", "", cookie); stolen.Code != http.StatusForbidden {
		t.Fatalf("cookie replayed by another identity status=%d", stolen.Code)
	}
	if missing := h.human("helper@example.com", http.MethodGet, "/support/"+id+"/metadata", ""); missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing cookie status=%d", missing.Code)
	}

	for name, body := range map[string]string{
		"other session":      `{"version":1,"request_id":"x1","operation":"session.get","arguments":{"session_id":"session-b"}}`,
		"ambiguous keys":     `{"version":1,"request_id":"x2","operation":"session.get","arguments":{"session_id":"session-a","SESSION_ID":"session-b"}}`,
		"list sessions":      `{"version":1,"request_id":"x3","operation":"session.list","arguments":{}}`,
		"role escalation":    `{"version":1,"request_id":"x4","operation":"annotation.create","arguments":{"session_id":"session-a","body":"x"}}`,
		"internal operation": `{"version":1,"request_id":"x5","operation":"support.redeem","arguments":{"session_id":"session-a"}}`,
	} {
		denied := h.human("helper@example.com", http.MethodPost, "/support/"+id+"/protocol", body, cookie)
		if denied.Code != http.StatusOK || !strings.Contains(denied.Body.String(), "support_denied") && !strings.Contains(denied.Body.String(), "invalid") {
			t.Fatalf("%s status=%d body=%s", name, denied.Code, denied.Body.String())
		}
	}
	h.serveDevice(func(task Task) protocol.Response {
		return protocol.Response{Version: 1, RequestID: task.Request.RequestID, Status: protocol.StatusOK, Data: map[string]string{"id": "session-a"}}
	})
	allowed := h.human("helper@example.com", http.MethodPost, "/support/"+id+"/protocol", `{"version":1,"request_id":"ok1","operation":"session.get","arguments":{"session_id":"session-a"}}`, cookie)
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), `"status":"ok"`) {
		t.Fatalf("allowed status=%d body=%s", allowed.Code, allowed.Body.String())
	}
	crossSite := httptest.NewRequest(http.MethodPost, "/support/"+id+"/protocol", strings.NewReader(`{}`))
	crossSite.Header.Set("Cf-Access-Jwt-Assertion", "helper@example.com")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSite.AddCookie(cookie)
	crossRecorder := httptest.NewRecorder()
	h.handler.ServeHTTP(crossRecorder, crossSite)
	if crossRecorder.Code != http.StatusForbidden {
		t.Fatalf("cross-site support request status=%d", crossRecorder.Code)
	}

	list := httptest.NewRecorder()
	h.handler.ServeHTTP(list, signedBridgeRequest(t, h.private, h.device.ID, http.MethodGet, "/agent/v1/support", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"supporter":"helper@example.com"`) || strings.Contains(list.Body.String(), code) {
		t.Fatalf("device list status=%d body=%s", list.Code, list.Body.String())
	}
	leave := h.human("helper@example.com", http.MethodPost, "/support/"+id+"/disconnect", `{}`, cookie)
	if leave.Code != http.StatusOK {
		t.Fatalf("disconnect status=%d body=%s", leave.Code, leave.Body.String())
	}
	if after := h.human("helper@example.com", http.MethodGet, "/support/"+id+"/metadata", "", cookie); after.Code != http.StatusForbidden {
		t.Fatalf("ended support still readable status=%d", after.Code)
	}
}

func TestSupportRedemptionRequiresDeviceConfirmation(t *testing.T) {
	h := newSupportHarness(t)
	_, refusedCode := h.issue("session-a", "view", 30)
	h.serveDevice(func(task Task) protocol.Response {
		return protocol.Response{Version: 1, RequestID: task.Request.RequestID, Status: protocol.StatusDenied, Error: &protocol.Error{Code: "denied", Message: "support code is not valid"}}
	})
	if refused := h.human("helper@example.com", http.MethodPost, "/support/connect", `{"code":"`+refusedCode+`"}`); refused.Code != http.StatusForbidden || len(refused.Result().Cookies()) != 0 {
		t.Fatalf("device refusal status=%d body=%s", refused.Code, refused.Body.String())
	}
	_, mismatchCode := h.issue("session-a", "view", 30)
	h.serveDevice(func(task Task) protocol.Response {
		response := confirmRedeem(task)
		response.Data.(map[string]any)["supporter"] = "someone-else@example.com"
		return response
	})
	if mismatch := h.human("helper@example.com", http.MethodPost, "/support/connect", `{"code":"`+mismatchCode+`"}`); mismatch.Code != http.StatusForbidden {
		t.Fatalf("mismatched confirmation status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}
	_, longCode := h.issue("session-a", "view", 5)
	h.serveDevice(func(task Task) protocol.Response {
		response := confirmRedeem(task)
		response.Data.(map[string]any)["expires_at"] = time.Now().Add(24 * time.Hour).UTC()
		return response
	})
	if long := h.human("helper@example.com", http.MethodPost, "/support/connect", `{"code":"`+longCode+`"}`); long.Code != http.StatusForbidden {
		t.Fatalf("over-long access accepted status=%d", long.Code)
	}
}

func TestSupportCodesAreBoundedRevocableAndRateLimited(t *testing.T) {
	h := newSupportHarness(t)
	id, code := h.issue("session-a", "view", 30)
	revoke := httptest.NewRecorder()
	h.handler.ServeHTTP(revoke, signedBridgeRequest(t, h.private, h.device.ID, http.MethodDelete, "/agent/v1/support/"+id, nil))
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	if revoked := h.human("helper@example.com", http.MethodPost, "/support/connect", `{"code":"`+code+`"}`); revoked.Code != http.StatusNotFound {
		t.Fatalf("revoked code status=%d", revoked.Code)
	}
	for _, invalid := range []struct {
		id, hash, session, role string
		minutes                 int
		expires                 time.Time
	}{
		{"bad-id", strings.Repeat("a", 64), "s", "view", 30, time.Now().Add(time.Minute)},
		{"support_0123456789abcdef", "short", "s", "view", 30, time.Now().Add(time.Minute)},
		{"support_0123456789abcdef", strings.Repeat("a", 64), "", "view", 30, time.Now().Add(time.Minute)},
		{"support_0123456789abcdef", strings.Repeat("a", 64), "s", "admin", 30, time.Now().Add(time.Minute)},
		{"support_0123456789abcdef", strings.Repeat("a", 64), "s", "view", 1, time.Now().Add(time.Minute)},
		{"support_0123456789abcdef", strings.Repeat("a", 64), "s", "view", 30, time.Now().Add(time.Hour)},
		{"support_0123456789abcdef", strings.Repeat("a", 64), "s", "view", 30, time.Now().Add(-time.Minute)},
	} {
		if _, err := h.store.RegisterSupport(h.device.ID, invalid.id, invalid.hash, invalid.session, invalid.role, invalid.expires, invalid.minutes); err == nil {
			t.Fatalf("accepted invalid support registration %+v", invalid)
		}
	}
	limited := 0
	for attempt := 0; attempt < 10; attempt++ {
		response := h.human("guesser@example.com", http.MethodPost, "/support/connect", `{"code":"0000-0000-0000"}`)
		if response.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("support code guessing was not rate limited")
	}
}

func TestUnlinkedDeviceCannotSendEmailInvitations(t *testing.T) {
	h := newSupportHarness(t)
	if _, _, err := h.store.CreateInvitation(h.device.ID, "", "reviewer@example.com", "session-a", "view", time.Now().Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "support code") {
		t.Fatalf("unlinked invitation err=%v", err)
	}
}
