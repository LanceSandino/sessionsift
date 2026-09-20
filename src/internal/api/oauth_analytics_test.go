package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOAuthMCPRequiresVerifiedAndLocallyApprovedIdentity(t *testing.T) {
	store := oauthTestStore(t)
	manager, err := remote.OpenManager(filepath.Join(t.TempDir(), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	grant, err := manager.ApproveOAuthPrincipal("owner@example.com", []protocol.Operation{protocol.SessionList}, 14*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion != "valid" {
			return accessauth.Principal{}, context.Canceled
		}
		return accessauth.Principal{Subject: "subject", Email: "owner@example.com"}, nil
	})
	handler := HandlerWithRemoteAccess(store, manager, remote.Identity{ID: "device"}, verifier, "https://sessionsift.example")

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "https://sessionsift.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Header().Get("WWW-Authenticate"), "oauth-protected-resource") {
		t.Fatalf("unauthorized=%d headers=%v body=%s", unauthorized.Code, unauthorized.Header(), unauthorized.Body.String())
	}

	httpClient := &http.Client{Transport: apiRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		request.Header.Set("Cf-Access-Jwt-Assertion", "valid")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "chatgpt-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: "https://sessionsift.example/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != "2026-07-28" {
		t.Fatalf("protocol=%s", session.InitializeResult().ProtocolVersion)
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "list_sessions" {
		t.Fatalf("listed=%v err=%v", listed, err)
	}
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "list_sessions", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("result=%v err=%v", result, err)
	}
	audit := manager.Audit()
	if len(audit) == 0 || audit[len(audit)-1].GrantID != grant.ID || audit[len(audit)-1].ClientID != "owner@example.com" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestAnalyticsAndProtectedResourceMetadataExcludeConversationText(t *testing.T) {
	store := oauthTestStore(t)
	manager, _ := remote.OpenManager(filepath.Join(t.TempDir(), "remote.json"))
	handler := HandlerWithRemoteAccess(store, manager, remote.Identity{}, nil, "https://sessionsift.example")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/analytics", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"sessions":1`) || strings.Contains(recorder.Body.String(), "sensitive prompt") {
		t.Fatalf("analytics=%d %s", recorder.Code, recorder.Body.String())
	}
	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"resource":"https://sessionsift.example"`) || !strings.Contains(metadata.Body.String(), "offline_access") {
		t.Fatalf("metadata=%d %s", metadata.Code, metadata.Body.String())
	}
}

func oauthTestStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	session := domain.Session{ID: "s1", ExternalSessionID: "s1", Title: "Session", Kind: domain.DeveloperSession, Source: domain.SourceCodex, StartedAt: now, EndedAt: now.Add(time.Minute), Status: "completed", ParserVersion: domain.ParserVersion, Turns: []domain.Turn{{ID: "t1", SessionID: "s1", Sequence: 1, UserPromptRedacted: "sensitive prompt", InputTokens: 10, CachedInputTokens: 5}}}
	if err = store.SaveImport(context.Background(), "path", "fingerprint", session); err != nil {
		t.Fatal(err)
	}
	return store
}

type apiRoundTripFunc func(*http.Request) (*http.Response, error)

func (function apiRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
