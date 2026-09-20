package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/config"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestVersionSetupAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "0.1.0") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "otel"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "127.0.0.1:7777/v1/logs") || !strings.Contains(out.String(), "127.0.0.1:7777/v1/metrics") {
		t.Fatalf("setup code=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "chatgpt"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "import-chatgpt") {
		t.Fatalf("chatgpt setup code=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "mcp"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "mcp_servers.sessionsift") {
		t.Fatalf("mcp setup code=%d out=%s", code, out.String())
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

func TestSetupEnableInstallsCurrentBinary(t *testing.T) {
	codex := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"setup", "enable"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	configBody, configErr := os.ReadFile(filepath.Join(codex, "config.toml"))
	hooksBody, hooksErr := os.ReadFile(filepath.Join(codex, "hooks.json"))
	if configErr != nil || hooksErr != nil || !strings.Contains(string(configBody), "/v1/metrics") || !strings.Contains(string(hooksBody), " hook") {
		t.Fatalf("config=%s hooks=%s errors=%v,%v", configBody, hooksBody, configErr, hooksErr)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "enable"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "already enabled") {
		t.Fatalf("second code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"doctor"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "OTel logs       configured\t✓") || !strings.Contains(out.String(), "OTel metrics    configured\t✓") {
		t.Fatalf("doctor code=%d out=%s err=%s", code, out.String(), errOut.String())
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
	t.Setenv("SESSIONSIFT_ALLOW_DOCKER_TUNNEL", "true")
	if listenAddress() != "0.0.0.0:7788" {
		t.Fatal("explicit Docker tunnel listener was rejected")
	}
	if localAddress(listenAddress()) != "127.0.0.1:7788" {
		t.Fatal("wildcard listener was not converted to a loopback client address")
	}
}

func TestDockerTunnelListenerRejectsNonLocalNetworkClients(t *testing.T) {
	called := false
	handler := localProxyOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	local := httptest.NewRecorder()
	localRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	localRequest.RemoteAddr = "127.0.0.1:41234"
	handler.ServeHTTP(local, localRequest)
	if local.Code != http.StatusNoContent || !called {
		t.Fatalf("local status=%d called=%v", local.Code, called)
	}

	called = false
	remote := httptest.NewRecorder()
	remoteRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	remoteRequest.RemoteAddr = "192.168.4.20:41234"
	handler.ServeHTTP(remote, remoteRequest)
	if remote.Code != http.StatusForbidden || called {
		t.Fatalf("remote status=%d called=%v", remote.Code, called)
	}
}

func TestChatGPTSourceHelpers(t *testing.T) {
	state := t.TempDir()
	paths := config.Paths{ChatGPTExport: filepath.Join(state, "chatgpt-export.path")}
	if chatGPTExportPath(paths) != "" || fileIdentity(filepath.Join(state, "missing")) != "" {
		t.Fatal("missing paths should be empty")
	}
	if err := os.WriteFile(paths.ChatGPTExport, []byte("\n"), 0o600); err != nil || chatGPTExportPath(paths) != "" {
		t.Fatal("empty remembered path should be ignored")
	}
	export := filepath.Join(state, "export.json")
	if err := os.WriteFile(export, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fileIdentity(export) == "" {
		t.Fatal("existing export should have an identity")
	}
	if err := os.WriteFile(paths.ChatGPTExport, []byte(export+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if chatGPTExportPath(paths) != export {
		t.Fatal("remembered export path not loaded")
	}
	t.Setenv("SESSIONSIFT_CHATGPT_EXPORT", filepath.Join(state, "override.json"))
	if !strings.HasSuffix(chatGPTExportPath(paths), "override.json") {
		t.Fatal("environment override not applied")
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

func TestImportChatGPTCommand(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("SESSIONSIFT_HOME", state)
	path := filepath.Join(t.TempDir(), "conversations.json")
	data := `[{"id":"chat","title":"Imported chat","create_time":1,"current_node":"u","mapping":{"u":{"message":{"id":"u","author":{"role":"user"},"create_time":1,"content":{"parts":["Hello"]}}}}}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"import-chatgpt", path}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), `"imported": 1`) {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	remembered, err := os.ReadFile(filepath.Join(state, "chatgpt-export.path"))
	if err != nil || strings.TrimSpace(string(remembered)) != path {
		t.Fatalf("remembered=%q err=%v", remembered, err)
	}
	if code := Run(context.Background(), []string{"import-chatgpt"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("missing path code=%d", code)
	}
}

func TestBackupCommandCreatesRestorableSnapshotAndRetentionIsBounded(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("SESSIONSIFT_HOME", state)
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"backup"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("backup code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	paths, err := config.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(paths.Backups)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".db") {
		t.Fatalf("backup entries=%v err=%v", entries, err)
	}
	for index := 0; index < 10; index++ {
		name := filepath.Join(paths.Backups, fmt.Sprintf("sessionsift-20000101T0000%02d.000000000Z.db", index))
		if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneBackups(paths.Backups, 7); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(paths.Backups)
	if len(entries) != 7 {
		t.Fatalf("retained=%d", len(entries))
	}
}

func TestBackgroundImportersAndBackupLoopStopCleanly(t *testing.T) {
	state := t.TempDir()
	paths := config.Paths{Backups: filepath.Join(state, "backups")}
	store, err := storage.Open(filepath.Join(state, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	codexHome := filepath.Join(state, "codex")
	transcript := filepath.Join(codexHome, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"background-codex"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Inspect"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	chatExport := filepath.Join(state, "conversations.json")
	if err := os.WriteFile(chatExport, []byte(`[{"id":"background-chat","title":"Chat","create_time":1,"current_node":"u","mapping":{"u":{"message":{"id":"u","author":{"role":"user"},"create_time":1,"content":{"parts":["Hello"]}}}}}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	doneCodex, doneChat := make(chan struct{}), make(chan struct{})
	go func() { defer close(doneCodex); importAndWatchCodex(ctx, store, codexHome, io.Discard, io.Discard) }()
	go func() { defer close(doneChat); importAndWatchChatGPT(ctx, store, chatExport, io.Discard, io.Discard) }()
	waitUntil(t, func() bool {
		sessions, listErr := store.ListSessions(context.Background())
		return listErr == nil && len(sessions) >= 2
	})
	cancel()
	<-doneCodex
	<-doneChat

	backupCtx, stopBackups := context.WithCancel(context.Background())
	doneBackup := make(chan struct{})
	go func() { defer close(doneBackup); runBackups(backupCtx, store, paths.Backups, io.Discard) }()
	waitUntil(t, func() bool {
		entries, readErr := os.ReadDir(paths.Backups)
		return readErr == nil && len(entries) == 1
	})
	stopBackups()
	<-doneBackup

	if !loopbackListener("127.0.0.1:7777") || loopbackListener("0.0.0.0:7777") {
		t.Fatal("listener classification changed")
	}
	if err := pruneBackups(paths.Backups, 0); err == nil {
		t.Fatal("zero retention should fail")
	}
	if message := protocolError(protocol.Response{}).Error(); !strings.Contains(message, "operation failed") {
		t.Fatalf("fallback protocol error=%q", message)
	}
	if message := protocolError(protocol.Response{Error: &protocol.Error{Message: "specific"}}).Error(); message != "specific" {
		t.Fatalf("specific protocol error=%q", message)
	}
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for background work")
		case <-ticker.C:
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

func TestMCPCommandListsOnlySafeSessionTools(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"mcp"}, input, &out, &errOut); code != 0 {
		t.Fatalf("code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"list_sessions"`) || strings.Contains(out.String(), "shell") || strings.Contains(out.String(), "filesystem") {
		t.Fatalf("unsafe or incomplete MCP tools: %s", out.String())
	}
}

func TestStartStatusAndStopErrors(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	codex := filepath.Join(t.TempDir(), "codex")
	chatExport := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(chatExport, []byte(`[{"id":"start-chat","title":"Startup chat"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SESSIONSIFT_HOME", state)
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("SESSIONSIFT_CHATGPT_EXPORT", chatExport)
	var out, errOut bytes.Buffer
	oldListen := listenTCP
	listenTCP = func(string, string) (net.Listener, error) { return nil, errors.New("bind failed") }
	t.Cleanup(func() { listenTCP = oldListen })
	if code := Run(context.Background(), []string{"start"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("start code=%d", code)
	}
	store, openErr := storage.Open(filepath.Join(state, "sessionsift.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	if imported, listErr := store.ListSessions(context.Background()); listErr != nil || len(imported) != 0 {
		t.Fatalf("startup imported %d sources before proving it could bind the listener: %v", len(imported), listErr)
	}
	_ = store.Close()
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
