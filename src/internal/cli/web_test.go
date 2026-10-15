// Package cli tests the loopback-only dashboard browser command.
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebOpensOnlyReadyLocalDashboard(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		io.WriteString(w, `{"status":"ready"}`)
	}))
	defer server.Close()
	t.Setenv("SESSIONSIFT_ADDR", strings.TrimPrefix(server.URL, "http://"))
	previous := openDashboardBrowser
	t.Cleanup(func() { openDashboardBrowser = previous })
	opened := ""
	openDashboardBrowser = func(_ context.Context, url string) error { opened = url; return nil }
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"web"}, strings.NewReader(""), &out, &errOut); code != 0 || opened != server.URL || !strings.Contains(out.String(), server.URL) {
		t.Fatalf("code=%d opened=%s out=%s err=%s", code, opened, &out, &errOut)
	}
	opened = ""
	if code := Run(context.Background(), []string{"web", "--no-browser"}, strings.NewReader(""), &out, &errOut); code != 0 || opened != "" {
		t.Fatalf("headless code=%d opened=%s", code, opened)
	}
}

func TestWebRejectsOfflineRedirectMalformedAndUnhealthyWithoutOpening(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	previous := openDashboardBrowser
	t.Cleanup(func() { openDashboardBrowser = previous })
	openDashboardBrowser = func(context.Context, string) error { t.Fatal("unready browser launch"); return nil }
	for _, kind := range []string{"unhealthy", "redirect", "malformed", "wrong-status", "too-large"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "unhealthy":
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
				case "redirect":
					http.Redirect(w, r, "https://example.invalid/", http.StatusFound)
				case "malformed":
					io.WriteString(w, "ready")
				case "wrong-status":
					io.WriteString(w, `{"status":"ok"}`)
				case "too-large":
					io.WriteString(w, `{"status":"ready","padding":"`+strings.Repeat("a", 8192)+`"}`)
				}
			}))
			defer server.Close()
			t.Setenv("SESSIONSIFT_ADDR", strings.TrimPrefix(server.URL, "http://"))
			var out, errOut bytes.Buffer
			if code := Run(context.Background(), []string{"web", "--no-browser"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "sessionsift start") {
				t.Fatalf("code=%d err=%s", code, &errOut)
			}
		})
	}
}

func TestDashboardURLNeverContainsRemoteHostOrUnvalidatedPort(t *testing.T) {
	for _, address := range []string{"example.invalid:7777", "192.0.2.1:7777", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:7777/path", "127.0.0.1:abc", "user@localhost:7777"} {
		if _, err := dashboardURL(address); err == nil {
			t.Errorf("accepted %s", address)
		}
	}
	for address, want := range map[string]string{"127.0.0.1:7777": "http://127.0.0.1:7777", "localhost:7777": "http://127.0.0.1:7777", "[::1]:7777": "http://[::1]:7777", "0.0.0.0:7777": "http://127.0.0.1:7777"} {
		got, err := dashboardURL(address)
		if err != nil || got != want {
			t.Errorf("%s = %s, %v", address, got, err)
		}
	}
}

func TestWebArgumentsAndBrowserFailure(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"web", "https://example.invalid"}, {"web", "--bad"}, {"web", "--no-browser", "extra"}} {
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"status":"ready"}`) }))
	defer server.Close()
	t.Setenv("SESSIONSIFT_ADDR", strings.TrimPrefix(server.URL, "http://"))
	previous := openDashboardBrowser
	t.Cleanup(func() { openDashboardBrowser = previous })
	openDashboardBrowser = func(context.Context, string) error { return errors.New("no display") }
	if code := Run(context.Background(), []string{"web"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "--no-browser") {
		t.Fatalf("code=%d err=%s", code, &errOut)
	}
}
