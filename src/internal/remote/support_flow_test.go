package remote_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	"github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

// TestSupportCodeFlowEndToEnd drives the TeamViewer-style flow through the real
// bridge handler and the real Local Agent polling loop: the source machine
// generates a code, a signed-in supporter enters it and lands in exactly one
// session, and the owner's End action cuts access immediately.
func TestSupportCodeFlowEndToEnd(t *testing.T) {
	state := t.TempDir()
	bridgeStore, err := bridge.Open(filepath.Join(state, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeStore.Close()
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		return accessauth.Principal{Subject: assertion, Email: assertion}, nil
	})
	handler := bridge.NewHandler(bridgeStore, verifier, "http://localhost")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	repository, err := storage.Open(filepath.Join(state, "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	now := time.Now().UTC()
	for _, session := range []domain.Session{
		{ID: "shared", ExternalSessionID: "shared", Title: "Shared session", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion},
		{ID: "private", ExternalSessionID: "private", Title: "Private session", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion},
	} {
		if err = repository.SaveImport(context.Background(), session.ID, "fingerprint", session); err != nil {
			t.Fatal(err)
		}
	}
	identity, _ := remote.LoadOrCreateIdentity(filepath.Join(state, "identity.json"))
	manager, _ := remote.OpenManager(filepath.Join(state, "remote.json"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime, err := remote.NewHostedRuntime(ctx, filepath.Join(state, "hosted.json"), identity, manager, repository, client, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()
	if _, err = runtime.StartSupport(ctx, "shared", "Shared session", "view", time.Hour); err == nil {
		t.Fatal("support code issued before connecting to a bridge")
	}
	status, err := runtime.Connect(ctx, "http://localhost", "Customer laptop")
	if err != nil || !status.Connected || status.Owner != "" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	offer, err := runtime.StartSupport(ctx, "shared", "Shared session", "view", 30*time.Minute)
	if err != nil || len(offer.Code) != 14 || offer.ConnectURL != "http://localhost/support" || offer.Support.Status != "waiting" {
		t.Fatalf("offer=%+v err=%v", offer, err)
	}

	supporter := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Cf-Access-Jwt-Assertion", "helper@example.com")
		request.Header.Set("Origin", "http://localhost")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	connect := supporter(http.MethodPost, "/support/connect", `{"code":"`+offer.Code+`"}`, nil)
	if connect.Code != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", connect.Code, connect.Body.String())
	}
	cookie := connect.Result().Cookies()[0]
	base := "/support/" + offer.Support.ID
	read := supporter(http.MethodPost, base+"/protocol", `{"version":1,"request_id":"read1","operation":"session.get","arguments":{"session_id":"shared"}}`, cookie)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"title":"Shared session"`) {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}
	escape := supporter(http.MethodPost, base+"/protocol", `{"version":1,"request_id":"read2","operation":"session.get","arguments":{"session_id":"private"}}`, cookie)
	if strings.Contains(escape.Body.String(), "Private session") || !strings.Contains(escape.Body.String(), "denied") {
		t.Fatalf("escape status=%d body=%s", escape.Code, escape.Body.String())
	}

	sessions := runtime.SupportSessions(ctx)
	if len(sessions) != 1 || sessions[0].Status != "connected" || sessions[0].Supporter != "helper@example.com" || sessions[0].LastActivityAt == nil {
		t.Fatalf("owner view=%+v", sessions)
	}
	encoded, _ := json.Marshal(sessions)
	if strings.Contains(string(encoded), offer.Code) || strings.Contains(string(encoded), "code_hash") {
		t.Fatalf("owner view leaked code material: %s", encoded)
	}
	if _, err = runtime.EndSupport(ctx, offer.Support.ID); err != nil {
		t.Fatal(err)
	}
	after := supporter(http.MethodPost, base+"/protocol", `{"version":1,"request_id":"read3","operation":"session.get","arguments":{"session_id":"shared"}}`, cookie)
	if strings.Contains(after.Body.String(), "Shared session") {
		t.Fatalf("ended support still read the session: %s", after.Body.String())
	}

	second, err := runtime.StartSupport(ctx, "shared", "Shared session", "comment", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	joined := supporter(http.MethodPost, "/support/connect", `{"code":"`+second.Code+`"}`, nil)
	if joined.Code != http.StatusOK {
		t.Fatalf("second connect status=%d body=%s", joined.Code, joined.Body.String())
	}
	secondCookie := joined.Result().Cookies()[0]
	if leave := supporter(http.MethodPost, "/support/"+second.Support.ID+"/disconnect", `{}`, secondCookie); leave.Code != http.StatusOK {
		t.Fatalf("leave status=%d body=%s", leave.Code, leave.Body.String())
	}
	for _, value := range runtime.SyncSupport(ctx) {
		if value.ID == second.Support.ID && (value.Status != "ended" || value.EndReason != "supporter_disconnected") {
			t.Fatalf("supporter disconnect not adopted locally: %+v", value)
		}
	}
	request := `{"version":1,"request_id":"r","operation":"session.get","arguments":{"session_id":"shared"}}`
	if err = manager.AuthorizePrincipalRequest(ctx, "helper@example.com", mustRequest(t, request)); err == nil {
		t.Fatal("local grants survived both ended support sessions")
	}
}

func mustRequest(t *testing.T, body string) protocol.Request {
	t.Helper()
	var request protocol.Request
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatal(err)
	}
	return request
}
