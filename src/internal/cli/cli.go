// Package cli implements the SessionSift command surface over local services.
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

	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	"github.com/LanceSandino/sessionsift/src/internal/api"
	"github.com/LanceSandino/sessionsift/src/internal/app"
	"github.com/LanceSandino/sessionsift/src/internal/codex/discover"
	"github.com/LanceSandino/sessionsift/src/internal/codexconfig"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/ingest"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

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
	case "stop":
		return stop(paths, out, errOut)
	case "status":
		return status(out, errOut)
	case "import":
		return importSessions(ctx, paths, out, errOut)
	case "import-chatgpt":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "ChatGPT export path required")
			return 2
		}
		return importChatGPT(ctx, paths, args[1], out, errOut)
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
	case "doctor":
		return doctor(ctx, paths, out, errOut)
	case "hook":
		return captureHook(paths, in, errOut)
	case "setup":
		if len(args) < 2 {
			fmt.Fprintln(errOut, "setup requires enable, hooks, otel, chatgpt, or undo")
			return 2
		}
		if args[1] == "enable" {
			return enableSetup(out, errOut)
		}
		return setup(args[1], out, errOut)
	default:
		usage(errOut)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: sessionsift <start|stop|status|import|import-chatgpt|sessions|inspect|compare|doctor|setup|hook|version>")
}

func openStore(paths config.Paths) (*storage.Store, error) {
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
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
	codexHome, err := discover.Home()
	if err == nil {
		result, importErr := ingest.ImportAll(ctx, store, codexHome)
		if importErr != nil {
			fmt.Fprintf(errOut, "initial import warning: %v\n", importErr)
		} else {
			fmt.Fprintf(out, "Imported %d of %d discovered sessions (%d unchanged).\n", result.Imported, result.Discovered, result.Skipped)
		}
	}
	chatGPTExport := chatGPTExportPath(paths)
	if chatGPTExport != "" {
		result, importErr := ingest.ImportChatGPT(ctx, store, chatGPTExport)
		if importErr != nil {
			fmt.Fprintf(errOut, "ChatGPT import warning: %v\n", importErr)
		} else {
			fmt.Fprintf(out, "Imported %d of %d ChatGPT conversations (%d unchanged).\n", result.Imported, result.Discovered, result.Skipped)
		}
	}
	if err := os.WriteFile(paths.PID, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fail(errOut, err)
	}
	defer os.Remove(paths.PID)
	address := listenAddress()
	listener, err := listenTCP("tcp", address)
	if err != nil {
		return fail(errOut, err)
	}
	server := &http.Server{Handler: api.Handler(store), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	stopCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if codexHome != "" {
		go watchCodex(stopCtx, store, codexHome, errOut)
	}
	if chatGPTExport != "" {
		go watchChatGPT(stopCtx, store, chatGPTExport, errOut)
	}
	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(out, "SessionSift is running at http://%s\n", address)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail(errOut, err)
	}
	return 0
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
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return fallback
	}
	return value
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
	fmt.Fprintf(out, "SessionSift is running at http://%s\n", listenAddress())
	return 0
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
	return write(out, errOut, result)
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
	response := agent.Execute(ctx, protocolRequest("cli-list", protocol.SessionList, service.ListArguments{}))
	if response.Status != protocol.StatusOK {
		return fail(errOut, protocolError(response))
	}
	for _, item := range response.Data.([]service.SessionSummary) {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", item.ID, item.StartedAt.Format(time.RFC3339), item.Model, item.Title)
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
	if store, openErr := openStore(paths); openErr == nil {
		database = paths.Database + "\t✓"
		if sessions, listErr := store.ListSessions(ctx); listErr == nil {
			for _, session := range sessions {
				warnings += len(session.Warnings)
				if session.Source == domain.SourceChatGPT {
					chatCount++
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
	fmt.Fprintf(out, "Codex home       %s\t✓\nSessions         %d found\t✓\nArchived         %d found\t✓\nChatGPT source   %s\nChatGPT chats    %d imported\nHistory          %s\nHook capture     %s\nOTel logs       %s\nOTel metrics    %s\nCollector        %s\nDatabase         %s\nParser warnings  %d\n", home, current, archived, chatSource, chatCount, history, hooks, otelLogs, otelMetrics, collector, database, warnings)
	return 0
}

func captureHook(paths config.Paths, in io.Reader, errOut io.Writer) int {
	body, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return 0
	}
	client := http.Client{Timeout: 40 * time.Millisecond}
	response, postErr := client.Post("http://"+listenAddress()+"/internal/hooks/codex", "application/json", bytes.NewReader(body))
	if postErr == nil {
		response.Body.Close()
		if response.StatusCode < 300 {
			return 0
		}
	}
	if err := paths.Ensure(); err != nil {
		return 0
	}
	name := fmt.Sprintf("%d-%d.json", time.Now().UnixNano(), os.Getpid())
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
	case "undo":
		fmt.Fprintln(out, "Remove only the SessionSift entries you added from ~/.codex/hooks.json and ~/.codex/config.toml. Delete ~/.sessionsift/chatgpt-export.path to stop watching a remembered ChatGPT export. SessionSift does not edit Codex configuration automatically.")
	default:
		fmt.Fprintln(errOut, "setup requires enable, hooks, otel, chatgpt, or undo")
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
	return client.Get("http://" + listenAddress() + "/healthz")
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
