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

	"github.com/LanceSandino/sessionsift/src/internal/api"
	"github.com/LanceSandino/sessionsift/src/internal/app"
	"github.com/LanceSandino/sessionsift/src/internal/codex/discover"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/ingest"
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
			fmt.Fprintln(errOut, "setup requires hooks, otel, or undo")
			return 2
		}
		return setup(args[1], out, errOut)
	default:
		usage(errOut)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: sessionsift <start|stop|status|import|sessions|inspect|compare|doctor|setup|hook|version>")
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
func list(ctx context.Context, paths config.Paths, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return fail(errOut, err)
	}
	for _, session := range sessions {
		if session.Kind == domain.InternalReviewSession {
			continue
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", session.ID, session.StartedAt.Format(time.RFC3339), session.Model, session.Title)
	}
	return 0
}
func inspect(ctx context.Context, paths config.Paths, id string, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	session, err := store.GetSession(ctx, id)
	if err != nil {
		return fail(errOut, err)
	}
	return write(out, errOut, map[string]any{"session": session, "metrics": domain.CalculateMetrics(session)})
}
func compare(ctx context.Context, paths config.Paths, aID, bID string, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	a, err := store.GetSession(ctx, aID)
	if err != nil {
		return fail(errOut, err)
	}
	b, err := store.GetSession(ctx, bID)
	if err != nil {
		return fail(errOut, err)
	}
	return write(out, errOut, map[string]any{"a": domain.CalculateMetrics(a), "b": domain.CalculateMetrics(b)})
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
	if strings.Contains(configText, "sessionsift hook") {
		hooks = "configured\t✓"
	}
	otel := "not configured (optional)\t!"
	if strings.Contains(string(configBody), "/v1/logs") && strings.Contains(string(configBody), "otel") {
		otel = "configured\t✓"
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
	if store, openErr := openStore(paths); openErr == nil {
		database = paths.Database + "\t✓"
		if sessions, listErr := store.ListSessions(ctx); listErr == nil {
			for _, session := range sessions {
				warnings += len(session.Warnings)
			}
		}
		store.Close()
	}
	fmt.Fprintf(out, "Codex home       %s\t✓\nSessions         %d found\t✓\nArchived         %d found\t✓\nHistory          %s\nHook capture     %s\nOTel             %s\nCollector        %s\nDatabase         %s\nParser warnings  %d\n", home, current, archived, history, hooks, otel, collector, database, warnings)
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
	case "undo":
		fmt.Fprintln(out, "Remove only the SessionSift entries you added from ~/.codex/hooks.json and ~/.codex/config.toml. SessionSift does not edit these files automatically.")
	default:
		fmt.Fprintln(errOut, "setup requires hooks, otel, or undo")
		return 2
	}
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
exporter = { otlp-http = { endpoint = "http://127.0.0.1:7777/v1/logs", protocol = "binary" } }`

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
