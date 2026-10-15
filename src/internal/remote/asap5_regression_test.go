package remote_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/remote"
)

// ASAP5-F2: a bridge redirect must never carry the registration body or the
// signed device headers to a different origin.
func TestHostedRequestsDoNotFollowBridgeRedirects(t *testing.T) {
	var reached atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
	}))
	defer other.Close()
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/captured", http.StatusTemporaryRedirect)
	}))
	defer bridge.Close()
	identity, err := remote.LoadOrCreateIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, client := range []*http.Client{nil, {}} {
		if _, err := remote.RegisterHosted(ctx, client, bridge.URL, "device", identity); err == nil {
			t.Fatal("registration accepted a redirect response")
		}
		config := remote.HostedConfig{URL: bridge.URL, DeviceID: identity.ID}
		if response, err := remote.SignedHostedRequest(ctx, client, config, identity, http.MethodPost, "/agent/v1/next", []byte(`{}`)); err == nil {
			response.Body.Close()
		}
	}
	if count := reached.Load(); count != 0 {
		t.Fatalf("second origin received %d redirected requests", count)
	}
}
