// Package cli implements the SessionSift command surface over local services.
// @module CLI dispatches commands and runs the Local Agent, bridge, importers, and backups.
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
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	"github.com/LanceSandino/sessionsift/src/internal/api"
	"github.com/LanceSandino/sessionsift/src/internal/app"
	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
	claudediscover "github.com/LanceSandino/sessionsift/src/internal/claude/discover"
	"github.com/LanceSandino/sessionsift/src/internal/codex/discover"
	"github.com/LanceSandino/sessionsift/src/internal/codexconfig"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/hooks"
	"github.com/LanceSandino/sessionsift/src/internal/ingest"
	mcpserver "github.com/LanceSandino/sessionsift/src/internal/mcp"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

var hostedHTTPClient *http.Client

// Run executes a command and returns a process exit code.
func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		usage(errOut)
		return 2
	}
	paths, err := config.Resolve()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, app.Version())
		return 0
	case "start":
		return start(ctx, paths, out, errOut)
	case "bridge":
		return startBridge(ctx, out, errOut)
	case "agent":
		return agentCommand(ctx, paths, args[1:], out, errOut)
	case "support":
		return supportCommand(ctx, paths, args[1:], out, errOut)
	case "stop":
		return stop(paths, out, errOut)
	case "status":
		return status(out, errOut)
	case "backup":
		return backup(ctx, paths, out, errOut)
	case "import":
		return importSessions(ctx, paths, out, errOut)
	case "import-chatgpt":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "ChatGPT export path required")
			return 2
		}
		return importChatGPT(ctx, paths, args[1], out, errOut)
	case "import-claude":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "Claude.ai export path required")
			return 2
		}
		return importClaudeExport(ctx, paths, args[1], out, errOut)
	case "sessions":
		return list(ctx, paths, out, errOut)
	case "inspect":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "session id required")
			return 2
		}
		return inspect(ctx, paths, args[1], out, errOut)
	case "compare":
		if len(args) < 3 {
			fmt.Fprintln(errOut, "two session ids required")
			return 2
		}
		return compare(ctx, paths, args[1], args[2], out, errOut)
	case "mcp":
		return serveMCP(ctx, paths, in, out, errOut)
	case "doctor":
		return doctor(ctx, paths, out, errOut)
	case "hook":
		provider := "codex"
		if len(args) > 1 {
			provider = args[1]
		}
		return captureHook(paths, provider, in, errOut)
	case "setup":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "setup requires enable, hooks, otel, chatgpt, claude, mcp, or undo")
			return 2
		}
		if args[1] == "enable" {
			if len(args) > 2 && args[2] == "claude" {
				return enableClaudeSetup(out, errOut)
			}
			return enableSetup(out, errOut)
		}
		return setup(args[1], out, errOut)
	default:
		usage(errOut)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: sessionsift <start|bridge|agent|support|stop|status|backup|import|import-chatgpt|import-claude|sessions|inspect|compare|mcp|doctor|setup|hook|version>")
}

// openStore prepares product state, installs the per-installation path-hash
// secret before any import runs, and opens the local database.
func openStore(paths config.Paths) (*storage.Store, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	key, err := config.LoadOrCreateSecret(paths.PathKey)
	if err != nil {
		return nil, fmt.Errorf("path hash secret: %w", err)
	}
	redact.SetPathKey(key)
	return storage.Open(paths.Database)
}

func start(ctx context.Context, paths config.Paths, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	if replayed, replayErr := ingest.ImportSpool(ctx, store, paths.RawEvents); replayErr != nil {
		fmt.Fprintf(errOut, "hook spool warning: %v\n", replayErr)
	} else if replayed > 0 {
		fmt.Fprintf(out, "Replayed %d spooled hook events.\n", replayed)
	}
	codexHome, _ := discover.Home()
	claudeHome, _ := claudediscover.Home()
	chatGPTExport := chatGPTExportPath(paths)
	claudeExport := rememberedPath(paths.ClaudeExport, "SESSIONSIFT_CLAUDE_EXPORT")
	if err := os.WriteFile(paths.PID, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fail(errOut, err)
	}
	defer os.Remove(paths.PID)
	address := listenAddress()
	listener, err := listenTCP("tcp", address)
	if err != nil {
		return fail(errOut, err)
	}
	identity, identityErr := remote.LoadOrCreateIdentity(paths.DeviceIdentity)
	if identityErr != nil {
		return fail(errOut, identityErr)
	}
	remoteManager, remoteErr := remote.OpenManager(paths.RemoteState)
	if remoteErr != nil {
		return fail(errOut, remoteErr)
	}
	stopCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	hosted, hostedErr := remote.NewHostedRuntime(stopCtx, paths.HostedBridge, identity, remoteManager, store, hostedHTTPClient, func(loopErr error) {
		fmt.Fprintf(errOut, "hosted bridge warning: %v\n", loopErr)
	})
	if hostedErr != nil {
		fmt.Fprintf(errOut, "hosted bridge configuration warning: %v\n", hostedErr)
	}
	defer hosted.Stop()
	handler := api.HandlerWithHostedRuntime(store, remoteManager, identity, hosted)
	if !loopbackListener(address) {
		handler = localProxyOnly(handler)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	if codexHome != "" {
		go importAndWatchCodex(stopCtx, store, codexHome, out, errOut)
	}
	if chatGPTExport != "" {
		go importAndWatchChatGPT(stopCtx, store, chatGPTExport, out, errOut)
	}
	if claudeHome != "" {
		go importAndWatchClaude(stopCtx, store, claudeHome, out, errOut)
	}
	if claudeExport != "" {
		go importAndWatchExport(stopCtx, store, claudeExport, "Claude", ingest.ImportClaudeExport, out, errOut)
	}
	go runBackups(stopCtx, store, paths.Backups, errOut)
	if status := hosted.Status(); status.Connected {
		fmt.Fprintf(out, "Outbound bridge connected to %s\n", status.URL)
	}
	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(out, "SessionSift is running at http://%s\n", localAddress(address))
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(errOut, err)
	}
	return 0
}

func startBridge(ctx context.Context, out, errOut io.Writer) int {
	home := strings.TrimSpace(os.Getenv("SESSIONSIFT_BRIDGE_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fail(errOut, err)
		}
		home = filepath.Join(userHome, ".sessionsift-bridge")
	}
	home = filepath.Clean(home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fail(errOut, err)
	}
	store, err := bridgeapp.Open(filepath.Join(home, "bridge.db"))
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	verifier, err := accessauth.NewFromEnv(nil)
	if err != nil {
		return fail(errOut, fmt.Errorf("bridge requires SESSIONSIFT_ACCESS_ISSUER and SESSIONSIFT_ACCESS_AUD: %w", err))
	}
	publicURL := strings.TrimSpace(os.Getenv("SESSIONSIFT_PUBLIC_URL"))
	if publicURL == "" {
		publicURL = "https://bridge.example.com"
	}
	address := bridgeAddress()
	listener, err := listenTCP("tcp", address)
	if err != nil {
		return fail(errOut, err)
	}
	server := &http.Server{Handler: localProxyOnly(bridgeapp.NewHandler(store, verifier, publicURL)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	stopCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go runBridgeMaintenance(stopCtx, store, filepath.Join(home, "backups"), errOut)
	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(out, "SessionSift bridge is running at http://%s\n", localAddress(address))
	if err = server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(errOut, err)
	}
	return 0
}

func bridgeAddress() string {
	value := strings.TrimSpace(os.Getenv("SESSIONSIFT_BRIDGE_ADDR"))
	if value == "" {
		return "127.0.0.1:7780"
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil || (host != "0.0.0.0" && host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return "127.0.0.1:7780"
	}
	return value
}

func importAndWatchCodex(ctx context.Context, store *storage.Store, home string, out, errOut io.Writer) {
	result, err := ingest.ImportAll(ctx, store, home)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(errOut, "initial import warning: %v\n", err)
		}
	} else {
		fmt.Fprintf(out, "Imported %d of %d discovered sessions (%d unchanged).\n", result.Imported, result.Discovered, result.Skipped)
	}
	watchCodex(ctx, store, home, errOut)
}

func importAndWatchChatGPT(ctx context.Context, store *storage.Store, path string, out, errOut io.Writer) {
	result, err := ingest.ImportChatGPT(ctx, store, path)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(errOut, "ChatGPT import warning: %v\n", err)
		}
	} else {
		fmt.Fprintf(out, "Imported %d of %d ChatGPT conversations (%d unchanged).\n", result.Imported, result.Discovered, result.Skipped)
	}
	watchChatGPT(ctx, store, path, errOut)
}

func watchChatGPT(ctx context.Context, store *storage.Store, path string, errOut io.Writer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := fileIdentity(path)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := fileIdentity(path)
			if current == "" || current == last {
				continue
			}
			if _, err := ingest.ImportChatGPT(ctx, store, path); err != nil && ctx.Err() == nil {
				fmt.Fprintf(errOut, "ChatGPT import warning: %v\n", err)
				continue
			}
			last = current
		}
	}
}

func fileIdentity(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

func chatGPTExportPath(paths config.Paths) string {
	if configured := strings.TrimSpace(os.Getenv("SESSIONSIFT_CHATGPT_EXPORT")); configured != "" {
		return filepath.Clean(configured)
	}
	value, err := os.ReadFile(paths.ChatGPTExport)
	if err != nil {
		return ""
	}
	stored := strings.TrimSpace(string(value))
	if stored == "" {
		return ""
	}
	return filepath.Clean(stored)
}

func listenAddress() string {
	const fallback = "127.0.0.1:7777"
	value := strings.TrimSpace(os.Getenv("SESSIONSIFT_ADDR"))
	if value == "" {
		return fallback
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return fallback
	}
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return value
	}
	if host == "0.0.0.0" && strings.EqualFold(strings.TrimSpace(os.Getenv("SESSIONSIFT_ALLOW_DOCKER_TUNNEL")), "true") {
		return value
	}
	return fallback
}

func loopbackListener(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && (host == "localhost" || net.ParseIP(host).IsLoopback())
}

func localAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err == nil && host == "0.0.0.0" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return address
}

// localProxyOnly permits Docker Desktop's loopback-forwarded tunnel traffic
// without exposing session data to direct clients on the local network.
func localProxyOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func watchCodex(ctx context.Context, store *storage.Store, codexHome string, errOut io.Writer) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := ingest.ImportAll(ctx, store, codexHome); err != nil && ctx.Err() == nil {
				fmt.Fprintf(errOut, "live import warning: %v\n", err)
			}
		}
	}
}

func stop(paths config.Paths, out, errOut io.Writer) int {
	data, err := os.ReadFile(paths.PID)
	if err != nil {
		fmt.Fprintln(errOut, "SessionSift is not running")
		return 1
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fail(errOut, err)
	}
	if err = signalPID(pid); err != nil {
		return fail(errOut, err)
	}
	fmt.Fprintln(out, "Stop signal sent.")
	return 0
}

func status(out, errOut io.Writer) int {
	response, err := getHealth()
	if err != nil {
		fmt.Fprintln(errOut, "SessionSift is not running")
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintln(errOut, "SessionSift is unhealthy")
		return 1
	}
	fmt.Fprintf(out, "SessionSift is running at http://%s\n", localAddress(listenAddress()))
	return 0
}

const backupRetention = 7

func backup(ctx context.Context, paths config.Paths, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	path, err := createBackup(ctx, store, paths.Backups, time.Now().UTC())
	if err != nil {
		return fail(errOut, err)
	}
	if err := pruneBackups(paths.Backups, backupRetention); err != nil {
		return fail(errOut, err)
	}
	fmt.Fprintf(out, "Backup created: %s\n", path)
	return 0
}

func createBackup(ctx context.Context, store *storage.Store, directory string, now time.Time) (string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	name := "sessionsift-" + now.UTC().Format("20060102T150405.000000000Z") + ".db"
	path := filepath.Join(directory, name)
	if err := store.Backup(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

func pruneBackups(directory string, retain int) error {
	if retain < 1 {
		return errors.New("backup retention must be positive")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "sessionsift-") && strings.HasSuffix(entry.Name(), ".db") {
			files = append(files, entry.Name())
		}
	}
	removeCount := len(files) - retain
	if removeCount < 0 {
		removeCount = 0
	}
	for _, name := range files[:removeCount] {
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	return nil
}

func runBackups(ctx context.Context, store *storage.Store, directory string, errOut io.Writer) {
	backupOnce := func() {
		if _, err := createBackup(ctx, store, directory, time.Now().UTC()); err != nil {
			if ctx.Err() == nil {
				fmt.Fprintf(errOut, "backup warning: %v\n", err)
			}
			return
		}
		if err := pruneBackups(directory, backupRetention); err != nil && ctx.Err() == nil {
			fmt.Fprintf(errOut, "backup retention warning: %v\n", err)
		}
	}
	backupOnce()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			backupOnce()
		}
	}
}

func importSessions(ctx context.Context, paths config.Paths, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	home, err := discover.Home()
	if err != nil {
		return fail(errOut, err)
	}
	result, err := ingest.ImportAll(ctx, store, home)
	if err != nil {
		return fail(errOut, err)
	}
	claude := ingest.Result{}
	if claudeHome, homeErr := claudediscover.Home(); homeErr == nil {
		if claude, err = ingest.ImportClaude(ctx, store, claudeHome); err != nil {
			return fail(errOut, err)
		}
	}
	return write(out, errOut, map[string]ingest.Result{"codex": result, "claude_code": claude})
}

func importChatGPT(ctx context.Context, paths config.Paths, path string, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	result, err := ingest.ImportChatGPT(ctx, store, path)
	if err != nil {
		return fail(errOut, err)
	}
	absolute, absoluteErr := filepath.Abs(path)
	if absoluteErr != nil {
		return fail(errOut, absoluteErr)
	}
	if err := os.WriteFile(paths.ChatGPTExport, []byte(filepath.Clean(absolute)+"\n"), 0o600); err != nil {
		return fail(errOut, err)
	}
	return write(out, errOut, result)
}
func list(ctx context.Context, paths config.Paths, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	agent := service.NewLocal(store)
	cursor := 0
	for {
		response := agent.Execute(ctx, protocolRequest("cli-list-"+strconv.Itoa(cursor), protocol.SessionList, service.ListArguments{Cursor: cursor, Limit: 100}))
		if response.Status != protocol.StatusOK {
			return fail(errOut, protocolError(response))
		}
		page := response.Data.(service.SessionListResponse)
		for _, item := range page.Sessions {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", item.ID, item.StartedAt.Format(time.RFC3339), item.Model, item.Title)
		}
		if !page.HasMore || page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	return 0
}
func inspect(ctx context.Context, paths config.Paths, id string, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	agent := service.NewLocal(store)
	sessionResponse := agent.Execute(ctx, protocolRequest("cli-inspect", protocol.SessionGet, service.SessionArguments{SessionID: id}))
	metricsResponse := agent.Execute(ctx, protocolRequest("cli-inspect-metrics", protocol.SessionMetrics, service.SessionArguments{SessionID: id}))
	if sessionResponse.Status != protocol.StatusOK {
		return fail(errOut, protocolError(sessionResponse))
	}
	if metricsResponse.Status != protocol.StatusOK {
		return fail(errOut, protocolError(metricsResponse))
	}
	return write(out, errOut, map[string]any{"session": sessionResponse.Data, "metrics": metricsResponse.Data})
}
func compare(ctx context.Context, paths config.Paths, aID, bID string, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	agent := service.NewLocal(store)
	response := agent.Execute(ctx, protocolRequest("cli-compare", protocol.SessionCompare, service.CompareArguments{A: aID, B: bID}))
	if response.Status != protocol.StatusOK {
		return fail(errOut, protocolError(response))
	}
	comparison := response.Data.(service.SessionComparison)
	return write(out, errOut, map[string]any{"a": comparison.MetricsA, "b": comparison.MetricsB})
}

func protocolRequest(id string, operation protocol.Operation, arguments any) protocol.Request {
	body, _ := json.Marshal(arguments)
	return protocol.Request{Version: protocol.Version, RequestID: id, Operation: operation, Arguments: body}
}

func protocolError(response protocol.Response) error {
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	return errors.New("local agent operation failed")
}

func serveMCP(ctx context.Context, paths config.Paths, in io.Reader, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	server := service.NewLocal(store)
	var serveErr error
	if inputFile, inputOK := in.(*os.File); inputOK && inputFile == os.Stdin {
		if outputFile, outputOK := out.(*os.File); outputOK && outputFile == os.Stdout {
			serveErr = mcpserver.ServeSDK(ctx, server, in, out)
		}
	}
	if serveErr == nil && (in != os.Stdin || out != os.Stdout) {
		// Deterministic finite streams keep the legacy harness used by tests and embedders;
		// the real CLI process uses the current official SDK above.
		serveErr = mcpserver.New(server).Serve(ctx, in, out)
	}
	if serveErr != nil {
		return fail(errOut, serveErr)
	}
	return 0
}

func doctor(ctx context.Context, paths config.Paths, out, errOut io.Writer) int {
	home, err := discover.Home()
	if err != nil {
		return fail(errOut, err)
	}
	sources, scanErr := discover.Sessions(home)
	if scanErr != nil {
		return fail(errOut, scanErr)
	}
	current, archived := 0, 0
	for _, source := range sources {
		if source.Archived {
			archived++
		} else {
			current++
		}
	}
	configBody, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	hooksBody, _ := os.ReadFile(filepath.Join(home, "hooks.json"))
	configText := string(configBody) + string(hooksBody)
	hooks := "not configured\t!"
	if strings.Contains(configText, "\"command\"") && strings.Contains(configText, " hook") {
		hooks = "configured\t✓"
	}
	otelLogs := "not configured (optional)\t!"
	if strings.Contains(string(configBody), "/v1/logs") && strings.Contains(string(configBody), "otel") {
		otelLogs = "configured\t✓"
	}
	otelMetrics := "not configured (optional)\t!"
	if strings.Contains(string(configBody), "/v1/metrics") && strings.Contains(string(configBody), "metrics_exporter") {
		otelMetrics = "configured\t✓"
	}
	history := "not found\t!"
	if _, statErr := os.Stat(filepath.Join(home, "history.jsonl")); statErr == nil {
		history = "available\t✓"
	}
	collector := "not running\t!"
	if response, healthErr := getHealth(); healthErr == nil {
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			collector = "running\t✓"
		}
	}
	database := "not initialized\t!"
	warnings := 0
	chatCount := 0
	claudeCodeCount, claudeCount := 0, 0
	if store, openErr := openStore(paths); openErr == nil {
		database = paths.Database + "\t✓"
		if sessions, listErr := store.ListSessions(ctx); listErr == nil {
			for _, session := range sessions {
				warnings += len(session.Warnings)
				switch session.Source {
				case domain.SourceChatGPT:
					chatCount++
				case domain.SourceClaudeCode:
					claudeCodeCount++
				case domain.SourceClaude:
					claudeCount++
				}
			}
		}
		store.Close()
	}
	chatSource := "not configured\t!"
	if path := chatGPTExportPath(paths); path != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			chatSource = "watching " + filepath.Base(path) + "\t✓"
		} else {
			chatSource = "configured path missing\t!"
		}
	}
	backupState := "none yet\t!"
	if entries, readErr := os.ReadDir(paths.Backups); readErr == nil {
		for index := len(entries) - 1; index >= 0; index-- {
			if entries[index].Type().IsRegular() && strings.HasPrefix(entries[index].Name(), "sessionsift-") && strings.HasSuffix(entries[index].Name(), ".db") {
				backupState = entries[index].Name() + "\t✓"
				break
			}
		}
	}
	fmt.Fprintf(out, "Codex home       %s\t✓\nSessions         %d found\t✓\nArchived         %d found\t✓\nChatGPT source   %s\nChatGPT chats    %d imported\nHistory          %s\nHook capture     %s\nOTel logs       %s\nOTel metrics    %s\nCollector        %s\nDatabase         %s\nLatest backup    %s\nParser warnings  %d\n", home, current, archived, chatSource, chatCount, history, hooks, otelLogs, otelMetrics, collector, database, backupState, warnings)
	fmt.Fprint(out, claudeDoctor(paths, claudeCodeCount, claudeCount))
	return 0
}

// captureHook forwards one lifecycle-hook payload from Codex or Claude Code.
// It never blocks the agent: on any delivery failure the payload is spooled
// under a provider-tagged name and replayed on the next start.
func captureHook(paths config.Paths, provider string, in io.Reader, errOut io.Writer) int {
	body, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return 0
	}
	route, prefix := "codex", ""
	if parsed, ok := hooks.ParseProvider(provider); ok && parsed == hooks.ClaudeCode {
		route, prefix = "claude", "claude-"
	}
	client := http.Client{Timeout: 40 * time.Millisecond}
	response, postErr := client.Post("http://"+localAddress(listenAddress())+"/internal/hooks/"+route, "application/json", bytes.NewReader(body))
	if postErr == nil {
		response.Body.Close()
		if response.StatusCode < 300 {
			return 0
		}
	}
	if err := paths.Ensure(); err != nil {
		return 0
	}
	name := fmt.Sprintf("%s%d-%d.json", prefix, time.Now().UnixNano(), os.Getpid())
	if err := os.WriteFile(filepath.Join(paths.RawEvents, name), body, 0o600); err != nil {
		fmt.Fprintln(errOut, "hook spool warning:", err)
	}
	return 0
}

func setup(kind string, out, errOut io.Writer) int {
	switch kind {
	case "hooks":
		fmt.Fprintln(out, hookSnippet)
	case "otel":
		fmt.Fprintln(out, otelSnippet)
	case "chatgpt":
		fmt.Fprintln(out, chatGPTSnippet)
	case "claude":
		fmt.Fprintln(out, claudeSnippet)
	case "mcp":
		fmt.Fprintln(out, mcpSnippet)
	case "undo":
		fmt.Fprintln(out, "Remove only the SessionSift entries you added from ~/.codex/hooks.json, ~/.codex/config.toml, and the \"hooks\" object of ~/.claude/settings.json (entries running \"sessionsift hook claude\"). Delete ~/.sessionsift/chatgpt-export.path or ~/.sessionsift/claude-export.path to stop watching a remembered export. SessionSift edits agent configuration only when you run setup enable.")
	default:
		fmt.Fprintln(errOut, "setup requires enable, hooks, otel, chatgpt, claude, mcp, or undo")
		return 2
	}
	return 0
}

func enableSetup(out, errOut io.Writer) int {
	home, err := discover.Home()
	if err != nil {
		return fail(errOut, err)
	}
	binary, err := os.Executable()
	if err != nil {
		return fail(errOut, err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return fail(errOut, err)
	}
	result, err := codexconfig.Enable(home, binary)
	if err != nil {
		return fail(errOut, err)
	}
	if !result.HooksChanged && !result.OTelChanged {
		fmt.Fprintln(out, "SessionSift hooks and telemetry are already enabled.")
		return 0
	}
	fmt.Fprintf(out, "Enabled SessionSift Codex integration in %s. Review and trust the new hooks with /hooks before they can run.\n", home)
	return 0
}

const hookSnippet = `Review this configuration, then merge it into ~/.codex/hooks.json:
{
  "hooks": {
    "SessionStart": [{"hooks":[{"type":"command","command":"sessionsift hook","async":true}]}],
    "UserPromptSubmit": [{"hooks":[{"type":"command","command":"sessionsift hook","async":true}]}],
    "PostToolUse": [{"matcher":"*","hooks":[{"type":"command","command":"sessionsift hook","async":true}]}],
    "PreCompact": [{"matcher":"*","hooks":[{"type":"command","command":"sessionsift hook","async":true}]}],
    "PostCompact": [{"matcher":"*","hooks":[{"type":"command","command":"sessionsift hook","async":true}]}],
    "Stop": [{"hooks":[{"type":"command","command":"sessionsift hook","async":true}]}],
    "SessionEnd": [{"matcher":"*","hooks":[{"type":"command","command":"sessionsift hook"}]}],
    "Interrupt": [{"hooks":[{"type":"command","command":"sessionsift hook","async":true}]}]
  }
}
Codex requires you to review and trust unmanaged hooks before they run.`
const otelSnippet = `[otel]
environment = "local"
log_user_prompt = false
exporter = { otlp-http = { endpoint = "http://127.0.0.1:7777/v1/logs", protocol = "binary" } }
metrics_exporter = { otlp-http = { endpoint = "http://127.0.0.1:7777/v1/metrics", protocol = "binary" } }`

const chatGPTSnippet = `Export ChatGPT data from Settings > Data Controls, then run:
  sessionsift import-chatgpt /path/to/chatgpt-export.zip

SessionSift remembers that private local path and checks it for replacements while running.
You can also set SESSIONSIFT_CHATGPT_EXPORT to override the remembered path.`

const mcpSnippet = `Codex: add SessionSift's read-only local MCP server to ~/.codex/config.toml:
[mcp_servers.sessionsift]
command = "sessionsift"
args = ["mcp"]

Claude Code:
  claude mcp add --scope user sessionsift -- sessionsift mcp

Claude Desktop: add to claude_desktop_config.json under "mcpServers":
  "sessionsift": {"command": "sessionsift", "args": ["mcp"]}

The stdio process exposes ten bounded, redacted tools and no shell, process, filesystem, or SQL primitive.`

var listenTCP = net.Listen
var signalPID = func(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}
var getHealth = func() (*http.Response, error) {
	client := http.Client{Timeout: 500 * time.Millisecond}
	return client.Get("http://" + localAddress(listenAddress()) + "/readyz")
}

func write(out, errOut io.Writer, value any) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fail(errOut, err)
	}
	return 0
}
func fail(w io.Writer, err error) int { fmt.Fprintln(w, err); return 1 }
