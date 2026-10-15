// Package remote verifies the signed runtime primitives reused by encrypted sync.
package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedHostedRequestBindsMethodPathQueryNonceAndBody(t *testing.T) {
	identity, _ := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	body := []byte(`{"opaque":true}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.RawQuery != "z=2&a=1" || r.Header.Get("X-SessionSift-Device") != identity.ID {
			t.Errorf("request method=%s query=%s device=%s", r.Method, r.URL.RawQuery, r.Header.Get("X-SessionSift-Device"))
			return
		}
		nonce := r.Header.Get("X-SessionSift-Nonce")
		timestamp := r.Header.Get("X-SessionSift-Timestamp")
		signature, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-SessionSift-Signature"))
		if err != nil || nonce == "" || timestamp == "" {
			t.Errorf("signed headers nonce=%q timestamp=%q err=%v", nonce, timestamp, err)
			return
		}
		digest := sha256.Sum256(body)
		message := []byte("sessionsift.device-request.v2\n" + r.Method + "\n/sync?a=1&z=2\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(digest[:]))
		if !ed25519.Verify(identity.PublicKey, message, signature) {
			t.Error("request signature did not bind canonical request")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	config := HostedConfig{URL: server.URL, DeviceID: identity.ID}
	response, err := SignedHostedRequest(context.Background(), server.Client(), config, identity, http.MethodPut, "/sync?z=2&a=1", body)
	if err != nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%v err=%v", response, err)
	}
	response.Body.Close()
	config.DeviceID = "different"
	if _, err = SignedHostedRequest(context.Background(), server.Client(), config, identity, http.MethodGet, "/sync", nil); err == nil {
		t.Fatal("mismatched device identity signed a bridge request")
	}
}

func TestOpenHostedClientExposesOnlySavedBinding(t *testing.T) {
	home := t.TempDir()
	identity, _ := LoadOrCreateIdentity(filepath.Join(home, "identity.json"))
	manager, _ := OpenManager(filepath.Join(home, "remote.json"))
	path := filepath.Join(home, "hosted.json")
	runtime, err := OpenHostedClient(path, identity, manager, nil)
	if err != nil || runtime.Status().Connected {
		t.Fatalf("empty runtime=%+v err=%v", runtime.Status(), err)
	}
	if _, err = runtime.CurrentConfig(); err == nil || runtime.Inviter() != nil {
		t.Fatal("unlinked runtime exposed hosted operations")
	}
	if _, _, err = runtime.StartLink(context.Background()); err == nil {
		t.Fatal("unlinked runtime started an owner link")
	}
	if _, err = runtime.RefreshOwner(context.Background()); err == nil {
		t.Fatal("unlinked runtime refreshed an owner")
	}
	if _, err = runtime.StartSupport(context.Background(), "session", "title", "view", 0); err == nil {
		t.Fatal("unlinked runtime started support")
	}
	if got := (*HostedRuntime)(nil).SupportSessions(context.Background()); len(got) != 0 {
		t.Fatalf("nil runtime sessions=%+v", got)
	}
	config := HostedConfig{URL: "https://bridge.example.test", DeviceID: identity.ID, WorkspaceID: "workspace", Owner: "owner@example.com", Name: "Laptop"}
	if err = SaveHostedConfig(path, config); err != nil {
		t.Fatal(err)
	}
	runtime, err = OpenHostedClient(path, identity, manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := runtime.CurrentConfig()
	if err != nil || loaded.WorkspaceID != config.WorkspaceID || !runtime.Status().Connected || runtime.Inviter() == nil {
		t.Fatalf("loaded=%+v status=%+v err=%v", loaded, runtime.Status(), err)
	}
	runtime.Stop()
	if strings.TrimSpace(DefaultDeviceName()) == "" {
		t.Fatal("default device name is empty")
	}
	if err = os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenHostedClient(path, identity, manager, nil); err == nil {
		t.Fatal("invalid saved bridge binding accepted")
	}
}

func TestHostedResponseErrorUsesStructuredPlainAndStatusFallbacks(t *testing.T) {
	for _, test := range []struct {
		body, status, want string
	}{
		{`{"error":"structured"}`, "Bad Request", "structured"},
		{"plain refusal", "Bad Request", "plain refusal"},
		{"", "Service Unavailable", "Service Unavailable"},
	} {
		response := &http.Response{Status: test.status, Body: ioNopCloser{bytes.NewBufferString(test.body)}}
		if err := hostedResponseError(response); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("error=%v want=%q", err, test.want)
		}
	}
}

type ioNopCloser struct{ *bytes.Buffer }

func (ioNopCloser) Close() error { return nil }
