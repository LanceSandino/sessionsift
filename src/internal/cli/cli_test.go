package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionSetupAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "0.1.0") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "otel"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "127.0.0.1:7777/v1/logs") {
		t.Fatalf("setup code=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "hooks"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("hooks setup code=%d out=%s", code, out.String())
	}
	snippet := out.String()
	start, end := strings.Index(snippet, "{"), strings.LastIndex(snippet, "}")
	if start < 0 || end < start || !json.Valid([]byte(snippet[start:end+1])) {
		t.Fatalf("hook setup did not contain valid JSON: %s", snippet)
	}
	if code := Run(context.Background(), nil, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("usage code=%d", code)
	}
}

func TestListenAddressRemainsLocalhostOnly(t *testing.T) {
	t.Setenv("SESSIONSIFT_ADDR", "127.0.0.1:7788")
	if listenAddress() != "127.0.0.1:7788" {
		t.Fatal("valid localhost address was rejected")
	}
	t.Setenv("SESSIONSIFT_ADDR", "0.0.0.0:7788")
	if listenAddress() != "127.0.0.1:7777" {
		t.Fatal("non-loopback address was accepted")
	}
}

func TestHookFallsBackToPrivateSpool(t *testing.T) {
	home := filepath.Join(t.TempDir(), "state")
	t.Setenv("SESSIONSIFT_HOME", home)
	t.Setenv("SESSIONSIFT_ADDR", "127.0.0.1:1")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"hook"}, strings.NewReader(`{"session_id":"s"}`), &out, &errOut); code != 0 {
		t.Fatalf("code=%d err=%s", code, errOut.String())
	}
	entries, err := os.ReadDir(filepath.Join(home, "raw", "events"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
}

func TestImportListInspectCompareDoctorAndSetup(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	codex := filepath.Join(t.TempDir(), "codex")
	t.Setenv("SESSIONSIFT_HOME", state)
	t.Setenv("CODEX_HOME", codex)
	transcript := filepath.Join(codex, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"session_id":"s1","cwd":"/tmp/repo"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"t1","model":"gpt"}}
{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Inspect auth"}]}}`
	if err := os.WriteFile(transcript, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := [][]string{{"import"}, {"sessions"}, {"inspect", "s1"}, {"compare", "s1", "s1"}, {"doctor"}, {"setup", "hooks"}, {"setup", "undo"}}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("args=%v code=%d err=%s", args, code, errOut.String())
		}
		if out.Len() == 0 {
			t.Fatalf("args=%v produced no output", args)
		}
	}
}

func TestInvalidCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"inspect"}, {"compare", "a"}, {"setup"}, {"setup", "bad"}, {"unknown"}} {
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &errOut); code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

func TestStartStatusAndStopErrors(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	codex := filepath.Join(t.TempDir(), "codex")
	t.Setenv("SESSIONSIFT_HOME", state)
	t.Setenv("CODEX_HOME", codex)
	var out, errOut bytes.Buffer
	oldListen := listenTCP
	listenTCP = func(string, string) (net.Listener, error) { return nil, errors.New("bind failed") }
	t.Cleanup(func() { listenTCP = oldListen })
	if code := Run(context.Background(), []string{"start"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("start code=%d", code)
	}
	oldHealth := getHealth
	getHealth = func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}
	t.Cleanup(func() { getHealth = oldHealth })
	if code := Run(context.Background(), []string{"status"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("status code=%d", code)
	}
	getHealth = func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	if code := Run(context.Background(), []string{"status"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("unhealthy status code=%d", code)
	}
	getHealth = func() (*http.Response, error) { return nil, errors.New("offline") }
	if code := Run(context.Background(), []string{"status"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("offline status code=%d", code)
	}
	if code := Run(context.Background(), []string{"stop"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("missing stop code=%d", code)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "sessionsift.pid"), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"stop"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("bad pid code=%d", code)
	}
	if err := os.WriteFile(filepath.Join(state, "sessionsift.pid"), []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldSignal := signalPID
	signalPID = func(pid int) error {
		if pid != 123 {
			t.Fatalf("pid=%d", pid)
		}
		return nil
	}
	t.Cleanup(func() { signalPID = oldSignal })
	if code := Run(context.Background(), []string{"stop"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("stop code=%d", code)
	}
}
