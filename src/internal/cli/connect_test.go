package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// syncBuffer lets a test read output while a command is still running.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(value)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func connectedCLI(t *testing.T) *bridgeapp.Store {
	t.Helper()
	state := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", filepath.Join(state, "local"))
	bridgeStore, err := bridgeapp.Open(filepath.Join(state, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridgeStore.Close() })
	handler := bridgeapp.NewHandler(bridgeStore, nil, "https://bridge.example")
	previous := hostedHTTPClient
	hostedHTTPClient = &http.Client{Transport: cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	t.Cleanup(func() { hostedHTTPClient = previous })
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"agent", "connect", "http://localhost"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("connect code=%d err=%s", code, errOut.String())
	}
	return bridgeStore
}

func TestSupportCommandIssuesListsAndEndsCodes(t *testing.T) {
	connectedCLI(t)
	paths, _ := config.Resolve()
	store, err := openStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = store.SaveImport(context.Background(), "p", "f", domain.Session{ID: "s1", ExternalSessionID: "s1", Title: "Checkout bug", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	previousHealth := getHealth
	getHealth = func() (*http.Response, error) { return nil, context.DeadlineExceeded }
	t.Cleanup(func() { getHealth = previousHealth })

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"support", "start", "s1", "view", "90"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("start code=%d err=%s", code, errOut.String())
	}
	text := out.String()
	if !strings.Contains(text, "Support code: ") || !strings.Contains(text, "https://bridge.example/support") || !strings.Contains(text, `"Checkout bug" only`) || !strings.Contains(errOut.String(), "not running") {
		t.Fatalf("start out=%s err=%s", text, errOut.String())
	}
	id := strings.TrimSpace(text[strings.LastIndex(text, "support end ")+len("support end "):])
	out.Reset()
	if code := Run(context.Background(), []string{"support", "list"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), id+"\twaiting\tview") {
		t.Fatalf("list code=%d out=%s", code, out.String())
	}
	if code := Run(context.Background(), []string{"support", "start", "s1", "view", "soon"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("bad minutes code=%d", code)
	}
	if code := Run(context.Background(), []string{"support", "start", "s1", "admin"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("bad role code=%d", code)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"support", "end", id}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Ended "+id) {
		t.Fatalf("end code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"support", "list"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), id+"\tended") {
		t.Fatalf("list after end=%s", out.String())
	}
}

func TestAgentLinkWaitsUntilTheOwnerEntersTheCode(t *testing.T) {
	bridgeStore := connectedCLI(t)
	previous := linkPollInterval
	linkPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { linkPollInterval = previous })
	out, errOut := &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- Run(context.Background(), []string{"agent", "link"}, strings.NewReader(""), out, errOut)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "Waiting for the owner") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	code := strings.Fields(strings.SplitN(out.String(), "Link code: ", 2)[1])[0]
	if _, err := bridgeStore.ClaimDeviceLink("owner@example.com", code, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case exit := <-done:
		if exit != 0 || !strings.Contains(out.String(), "Linked to owner@example.com.") {
			t.Fatalf("exit=%d out=%s err=%s", exit, out.String(), errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent link did not observe the claim")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var cancelOut, cancelErr bytes.Buffer
	if exit := Run(cancelled, []string{"agent", "link"}, strings.NewReader(""), &cancelOut, &cancelErr); exit != 1 {
		t.Fatalf("cancelled link exit=%d", exit)
	}
}
