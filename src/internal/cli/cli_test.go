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
	"sync"
	"testing"
	"time"

	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/ingest"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
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
	var hookConfig struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(snippet[start:end+1]), &hookConfig); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "PreCompact", "PostCompact", "Stop"} {
		handler := hookConfig.Hooks[event][0].Hooks[0]
		if handler["timeout"] != float64(10) || handler["async"] != true {
			t.Fatalf("%s policy=%v", event, handler)
		}
	}
	sessionEnd := hookConfig.Hooks["SessionEnd"][0].Hooks[0]
	if sessionEnd["timeout"] != float64(3) {
		t.Fatalf("SessionEnd timeout=%v", sessionEnd["timeout"])
	}
	if _, exists := sessionEnd["async"]; exists {
		t.Fatalf("SessionEnd must be synchronous: %v", sessionEnd)
	}
	interrupt := hookConfig.Hooks["Interrupt"][0].Hooks[0]
	if interrupt["timeout"] != float64(3) || interrupt["async"] != true {
		t.Fatalf("Interrupt policy=%v", interrupt)
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
	oldCollectorHealth := getCollectorHealth
	getCollectorHealth = func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"ok"}`))}, nil
	}
	t.Cleanup(func() { getCollectorHealth = oldCollectorHealth })
	if code := Run(context.Background(), []string{"doctor"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Codex\n") || !strings.Contains(out.String(), "  Hooks            configured\t✓") || !strings.Contains(out.String(), "  OTel logs        configured\t✓") || !strings.Contains(out.String(), "  OTel metrics     configured\t✓") {
		t.Fatalf("doctor code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "  OTel collector   running\t✓") {
		t.Fatalf("doctor did not use liveness probe: %s", out.String())
	}
	for _, ambiguous := range []string{"\nSessions        ", "\nHistory         ", "\nHook capture    ", "\nCollector       "} {
		if strings.Contains(out.String(), ambiguous) {
			t.Fatalf("doctor retained ambiguous row %q: %s", ambiguous, out.String())
		}
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

func TestHostedAgentEnrollmentShareStatusAndRevoke(t *testing.T) {
	state := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", filepath.Join(state, "local"))
	bridgeStore, err := bridgeapp.Open(filepath.Join(state, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeStore.Close()
	handler := bridgeapp.NewHandler(bridgeStore, nil, "")
	client := &http.Client{Transport: cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	previousClient := hostedHTTPClient
	hostedHTTPClient = client
	t.Cleanup(func() { hostedHTTPClient = previousClient })

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"agent", "status"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "not connected") {
		t.Fatalf("status before connect code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"agent", "enroll", "http://localhost", "CODE"}, strings.NewReader(""), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "agent connect") {
		t.Fatalf("retired enroll code=%d err=%s", code, errOut.String())
	}
	errOut.Reset()
	if code := Run(context.Background(), []string{"agent", "connect", "http://localhost", "Test", "Mac"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Connected Test Mac (device_") {
		t.Fatalf("connect code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"agent", "status"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "not linked") {
		t.Fatalf("unlinked status code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"agent", "link", "--no-wait"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Link code: ") {
		t.Fatalf("link code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	linkCode := strings.Fields(strings.SplitN(out.String(), "Link code: ", 2)[1])[0]
	if _, err = bridgeStore.ClaimDeviceLink("owner@example.com", linkCode, ""); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"agent", "status"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Owner: owner@example.com") {
		t.Fatalf("status code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"agent", "share", "reviewer@example.com", "7"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "reviewer@example.com") {
		t.Fatalf("share code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"agent", "revoke", "reviewer@example.com"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Revoked") {
		t.Fatalf("revoke code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	if code := Run(context.Background(), []string{"agent", "share", "reviewer@example.com", "31"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("invalid duration code=%d", code)
	}
	if code := Run(context.Background(), []string{"agent"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("missing subcommand code=%d", code)
	}
}

func TestBridgeCommandRequiresAccessConfigurationAndValidatesAddress(t *testing.T) {
	t.Setenv("SESSIONSIFT_BRIDGE_HOME", t.TempDir())
	t.Setenv("SESSIONSIFT_ACCESS_ISSUER", "")
	t.Setenv("SESSIONSIFT_ACCESS_AUD", "")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"bridge"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "requires SESSIONSIFT_ACCESS_ISSUER") {
		t.Fatalf("bridge code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	t.Setenv("SESSIONSIFT_ACCESS_ISSUER", "https://access.example.com")
	t.Setenv("SESSIONSIFT_ACCESS_AUD", "audience")
	t.Setenv("SESSIONSIFT_PUBLIC_URL", "")
	oldListen := listenTCP
	listenTCP = func(string, string) (net.Listener, error) { return nil, errors.New("bridge bind failed") }
	t.Cleanup(func() { listenTCP = oldListen })
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"bridge"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "bridge bind failed") {
		t.Fatalf("bridge bind code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	t.Setenv("SESSIONSIFT_BRIDGE_ADDR", "10.0.0.1:9999")
	if bridgeAddress() != "127.0.0.1:7780" {
		t.Fatal("unsafe bridge bind was accepted")
	}
	t.Setenv("SESSIONSIFT_BRIDGE_ADDR", "127.0.0.1:7780")
	if bridgeAddress() != "127.0.0.1:7780" {
		t.Fatal("loopback bridge bind was rejected")
	}
}

func TestBridgeCommandStartsAndStopsOnCancelledContext(t *testing.T) {
	t.Setenv("SESSIONSIFT_BRIDGE_HOME", t.TempDir())
	t.Setenv("SESSIONSIFT_ACCESS_ISSUER", "https://access.example.com")
	t.Setenv("SESSIONSIFT_ACCESS_AUD", "audience")
	t.Setenv("SESSIONSIFT_PUBLIC_URL", "https://sessionsift.example")
	oldListen := listenTCP
	listener := &blockingListener{closed: make(chan struct{})}
	listenTCP = func(string, string) (net.Listener, error) { return listener, nil }
	t.Cleanup(func() { listenTCP = oldListen })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	if code := Run(ctx, []string{"bridge"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "bridge is running") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}

func TestLocalAgentStartsLoopbackWithOutboundBridgeConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "empty-codex"))
	t.Setenv("SESSIONSIFT_ADDR", "127.0.0.1:7777")
	paths, _ := config.Resolve()
	if err := remote.SaveHostedConfig(paths.HostedBridge, remote.HostedConfig{URL: "https://sessionsift.example", DeviceID: "device_test", Owner: "owner@example.com"}); err != nil {
		t.Fatal(err)
	}
	oldListen := listenTCP
	listener := &blockingListener{closed: make(chan struct{})}
	listenTCP = func(string, string) (net.Listener, error) { return listener, nil }
	t.Cleanup(func() { listenTCP = oldListen })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut lockedBuffer
	if code := Run(ctx, []string{"start"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Outbound bridge connected") || !strings.Contains(out.String(), "127.0.0.1:7777") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}

func TestHostedAgentCommandUsageAndUnenrolledStatus(t *testing.T) {
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"agent", "status"}, 0},
		{[]string{"agent", "enroll"}, 2},
		{[]string{"agent", "connect"}, 2},
		{[]string{"agent", "link"}, 1},
		{[]string{"support"}, 2},
		{[]string{"support", "start"}, 2},
		{[]string{"support", "start", "missing-session"}, 1},
		{[]string{"support", "end"}, 2},
		{[]string{"support", "end", "support_missing"}, 1},
		{[]string{"support", "list"}, 0},
		{[]string{"support", "unknown"}, 2},
		{[]string{"agent", "share"}, 2},
		{[]string{"agent", "share", "person@example.com"}, 1},
		{[]string{"agent", "revoke"}, 2},
		{[]string{"agent", "revoke", "person@example.com"}, 1},
		{[]string{"agent", "unknown"}, 2},
	}
	for _, test := range cases {
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), test.args, strings.NewReader(""), &out, &errOut); code != test.code {
			t.Fatalf("args=%v code=%d want=%d out=%s err=%s", test.args, code, test.code, out.String(), errOut.String())
		}
	}
}

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func (listener *blockingListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, net.ErrClosed
}
func (listener *blockingListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}
func (listener *blockingListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4zero, Port: 7780} }

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(value)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

type cliRoundTripFunc func(*http.Request) (*http.Response, error)

func (function cliRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
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

// Regression anchor for CU-20260924-005: per-session import failures no longer
// abort the pass, so startup reports them instead, bounded.
func TestReportImportFailuresIsBoundedAndSilentWithoutFailures(t *testing.T) {
	var quiet strings.Builder
	reportImportFailures(&quiet, "Codex", ingest.Result{Imported: 3, Warnings: []string{"Codex thread metadata unavailable"}})
	if quiet.Len() != 0 {
		t.Fatalf("reported without failures: %q", quiet.String())
	}
	warnings := make([]string, maxReportedImportWarnings+2)
	for index := range warnings {
		warnings[index] = fmt.Sprintf("2026/%d.jsonl: rejected", index)
	}
	var report strings.Builder
	reportImportFailures(&report, "Codex", ingest.Result{Failed: len(warnings), Warnings: warnings})
	lines := strings.Split(strings.TrimSpace(report.String()), "\n")
	if len(lines) != maxReportedImportWarnings+1 || !strings.Contains(lines[0], "Codex import: 7 could not be imported") || !strings.Contains(lines[1], "2026/0.jsonl") || strings.Contains(report.String(), "2026/6.jsonl") {
		t.Fatalf("report=%q", report.String())
	}
}
