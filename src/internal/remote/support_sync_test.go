package remote

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// UI polling must not spend the device's signed-request budget: bridge syncs
// happen only while a support session is live and at most every interval.
func TestSupportSyncIsRateLimitedAndOnlyWhileLive(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	directory := t.TempDir()
	identity, _ := LoadOrCreateIdentity(filepath.Join(directory, "identity.json"))
	manager, _ := OpenManager(filepath.Join(directory, "state.json"))
	runtime := &HostedRuntime{identity: identity, manager: manager, client: client, config: &HostedConfig{URL: "http://localhost", DeviceID: identity.ID}}
	runtime.SupportSessions(context.Background())
	if calls.Load() != 0 {
		t.Fatalf("synced with no live support session: %d", calls.Load())
	}
	if _, _, err := manager.CreateSupportSession("s1", "t", "view", time.Hour); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		runtime.SupportSessions(context.Background())
	}
	if calls.Load() != 1 {
		t.Fatalf("polling synced %d times within one interval", calls.Load())
	}
	runtime.SyncSupport(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("forced sync did not reach the bridge: %d", calls.Load())
	}
	runtime.lastSupportSync = time.Now().Add(-supportSyncInterval)
	runtime.SupportSessions(context.Background())
	if calls.Load() != 3 {
		t.Fatalf("sync did not resume after the interval: %d", calls.Load())
	}
}
