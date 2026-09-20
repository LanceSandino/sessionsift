package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
)

func TestMCPAccessLogCorrelatesRequestsWithoutRecordingSecrets(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	verifier := accessauth.VerifierFunc(func(_ context.Context, assertion string) (accessauth.Principal, error) {
		if assertion != "valid-secret-assertion" {
			return accessauth.Principal{}, context.Canceled
		}
		return accessauth.Principal{Subject: "owner", Email: "Owner@Example.com"}, nil
	})
	var logs bytes.Buffer
	handler := newHandler(store, verifier, "https://sessionsift.example", slog.New(slog.NewJSONHandler(&logs, nil)))

	unauthorized := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"prompt":"body-secret"}`))
	unauthorized.Header.Set("Authorization", "Bearer bearer-secret")
	unauthorized.Header.Set("Cf-Access-Jwt-Assertion", "jwt-secret")
	unauthorized.Header.Set("Cf-Ray", "safe-ray-123")
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorizedRecorder.Code)
	}

	authorized := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","private":"another-secret"}`))
	authorized.Header.Set("Cf-Access-Jwt-Assertion", "valid-secret-assertion")
	authorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(authorizedRecorder, authorized)
	if authorizedRecorder.Code != http.StatusForbidden {
		t.Fatalf("authorized status=%d body=%s", authorizedRecorder.Code, authorizedRecorder.Body.String())
	}

	output := logs.String()
	for _, expected := range []string{
		`"event":"mcp_access"`, `"request_id":"safe-ray-123"`, `"method":"POST"`,
		`"status":401`, `"status":403`, `"identity_verified":false`, `"identity_verified":true`,
		`"principal_hash":"`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("log does not contain %q: %s", expected, output)
		}
	}
	for _, secret := range []string{
		"body-secret", "another-secret", "bearer-secret", "jwt-secret", "valid-secret-assertion", "owner@example.com",
	} {
		if strings.Contains(strings.ToLower(output), strings.ToLower(secret)) {
			t.Errorf("log leaked %q: %s", secret, output)
		}
	}
}

func TestMCPAccessLogSanitizesCorrelationIdentifiersAndPreservesStreaming(t *testing.T) {
	for _, unsafe := range []string{"ray with spaces", "ray\nforged", strings.Repeat("x", 129)} {
		identifier := requestID(unsafe)
		if identifier == unsafe || len(identifier) != 32 {
			t.Errorf("requestID(%q)=%q", unsafe, identifier)
		}
	}
	if identifier := requestID("  abc-123-IAD  "); identifier != "abc-123-IAD" {
		t.Fatalf("safe request ID=%q", identifier)
	}
	if left, right := principalHash("Owner@Example.com"), principalHash(" owner@example.com "); left != right || len(left) != 24 {
		t.Fatalf("principal hashes left=%q right=%q", left, right)
	}

	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	recorder.Flush()
	recorder.WriteHeader(http.StatusTeapot)
	if recorder.status != http.StatusOK || underlying.Code != http.StatusOK || !underlying.Flushed {
		t.Fatalf("stream status=%d underlying=%d flushed=%v", recorder.status, underlying.Code, underlying.Flushed)
	}
	if unwrapped := recorder.Unwrap(); unwrapped != underlying {
		t.Fatalf("unwrapped response writer=%T", unwrapped)
	}
}
