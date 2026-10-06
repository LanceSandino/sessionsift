// Package api serves the localhost JSON API, hook receiver, and embedded UI.
// @module LocalAPI routes the loopback JSON API, hook and OTLP receivers, and the embedded UI.
package api

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/accessauth"
	"github.com/LanceSandino/sessionsift/src/internal/agent/service"
	"github.com/LanceSandino/sessionsift/src/internal/codex/otel"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/hooks"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/release"
	"github.com/LanceSandino/sessionsift/src/internal/remote"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	"github.com/LanceSandino/sessionsift/src/webui"
)

// Handler returns the complete localhost application handler.
func Handler(store *storage.Store) http.Handler {
	return HandlerWithRemote(store, nil, remote.Identity{})
}

// HandlerWithRemote adds opt-in owner and scoped remote routes while retaining localhost authority.
func HandlerWithRemote(store *storage.Store, remoteManager *remote.Manager, identity remote.Identity) http.Handler {
	verifier, _ := accessauth.NewFromEnv(nil)
	return handlerWithRemoteAccess(store, remoteManager, identity, verifier, strings.TrimRight(os.Getenv("SESSIONSIFT_PUBLIC_URL"), "/"), nil)
}

// HandlerWithHostedRuntime adds owner control of the bridge connection: device
// registration and linking, support codes, and email invitations.
func HandlerWithHostedRuntime(store *storage.Store, remoteManager *remote.Manager, identity remote.Identity, hosted *remote.HostedRuntime) http.Handler {
	verifier, _ := accessauth.NewFromEnv(nil)
	return handlerWithRemoteAccess(store, remoteManager, identity, verifier, strings.TrimRight(os.Getenv("SESSIONSIFT_PUBLIC_URL"), "/"), hosted)
}

// HandlerWithRemoteAccess allows production Access verification and deterministic integration tests.
func HandlerWithRemoteAccess(store *storage.Store, remoteManager *remote.Manager, identity remote.Identity, verifier accessauth.Verifier, publicURL string) http.Handler {
	return handlerWithRemoteAccess(store, remoteManager, identity, verifier, publicURL, nil)
}

func handlerWithRemoteAccess(store *storage.Store, remoteManager *remote.Manager, identity remote.Identity, verifier accessauth.Verifier, publicURL string, hosted *remote.HostedRuntime) http.Handler {
	agent := service.NewLocal(store)
	assets, _ := fs.Sub(webui.Assets, "dist")
	web := webHandler(assets)
	return localGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'")
		switch {
		case remoteManager != nil && (strings.HasPrefix(r.URL.Path, "/api/v1/remote/") || strings.HasPrefix(r.URL.Path, "/remote/v1/") || r.URL.Path == "/mcp"):
			remoteRoute(store, remoteManager, identity, verifier, publicURL, hosted, w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource"):
			protectedResourceMetadata(publicURL, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			listSessions(agent, w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
			sessionRoute(agent, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/compare":
			compare(agent, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/analytics":
			analyticsReport(agent, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/changelog":
			changelogPage(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/v1/optimization"):
			optimizationRoute(store, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/diagnostics":
			diagnosticsReport(store, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/protocol":
			receiveProtocol(agent, w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/internal/hooks/"):
			receiveHook(store, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/logs":
			receiveOTel(store, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/metrics":
			receiveOTelMetrics(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			if remoteManager != nil && !remoteManager.AuditHealthy() {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "audit_unavailable"})
				return
			}
			readyCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := store.Health(readyCtx); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		case r.Method == http.MethodGet:
			web.ServeHTTP(w, r)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	}))
}

func changelogPage(w http.ResponseWriter, r *http.Request) {
	page, pageSize := 1, 5
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := r.URL.Query().Get("page_size"); raw != "" {
			pageSize, err = strconv.Atoi(raw)
		}
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "page and page_size must be integers"})
		return
	}
	result, err := release.Page(page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type diagnosticsResponse struct {
	storage.CaptureStats
	CollectorRunning bool   `json:"collector_running"`
	OTelActive       bool   `json:"otel_active"`
	HookActive       bool   `json:"hook_active"`
	ClaudeHookActive bool   `json:"claude_hook_active"`
	MetricsReceiver  string `json:"metrics_receiver"`
}

func diagnosticsReport(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	stats, err := store.CaptureStats(r.Context())
	if err != nil {
		apiError(w, err)
		return
	}
	cutoff := time.Now().UTC().Add(-15 * time.Minute)
	writeJSON(w, http.StatusOK, diagnosticsResponse{
		CaptureStats: stats, CollectorRunning: true, MetricsReceiver: "acknowledge_only",
		OTelActive:       stats.LatestOTelEventAt != nil && stats.LatestOTelEventAt.After(cutoff),
		HookActive:       stats.LatestHookEventAt != nil && stats.LatestHookEventAt.After(cutoff),
		ClaudeHookActive: stats.LatestClaudeHookEventAt != nil && stats.LatestClaudeHookEventAt.After(cutoff),
	})
}

func analyticsReport(agent *service.Service, w http.ResponseWriter, r *http.Request) {
	data, ok := execute(agent, w, r, protocol.AnalysisPatterns, struct{}{})
	if ok {
		writeJSON(w, http.StatusOK, data)
	}
}

func receiveProtocol(agent *service.Service, w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, protocol.MaximumArgumentsBytes+(8<<10)))
	decoder.DisallowUnknownFields()
	var request protocol.Request
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Response{Version: protocol.Version, Status: protocol.StatusInvalidRequest, Error: &protocol.Error{Code: "invalid_request", Message: "invalid protocol request"}})
		return
	}
	response := agent.Execute(r.Context(), request)
	status := http.StatusOK
	if response.Status != protocol.StatusOK {
		status = statusFor(response.Status)
	}
	writeJSON(w, status, response)
}

func receiveOTelMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "OTLP batch is too large"})
		return
	}
	count, err := otel.DecodeMetrics(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid OTLP metrics batch"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": count})
}

func receiveOTel(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "OTLP batch is too large"})
		return
	}
	events, err := otel.Decode(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid OTLP log batch"})
		return
	}
	for _, event := range events {
		if err := store.AppendHookEvent(r.Context(), event); err != nil {
			apiError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": len(events)})
}

func listSessions(agent *service.Service, w http.ResponseWriter, r *http.Request) {
	arguments := service.ListArguments{IncludeInternal: r.URL.Query().Get("include_internal") == "true"}
	for key, target := range map[string]*int{"cursor": &arguments.Cursor, "limit": &arguments.Limit} {
		if value := r.URL.Query().Get(key); value != "" {
			number, err := strconv.Atoi(value)
			if err != nil || number < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid pagination"})
				return
			}
			*target = number
		}
	}
	data, ok := execute(agent, w, r, protocol.SessionList, arguments)
	if ok {
		writeJSON(w, http.StatusOK, data.(service.SessionListResponse).Sessions)
	}
}

func sessionRoute(agent *service.Service, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session id required"})
		return
	}
	if len(parts) == 1 {
		data, ok := execute(agent, w, r, protocol.SessionGet, service.SessionArguments{SessionID: parts[0]})
		if ok {
			session := data.(domain.Session)
			writeJSON(w, http.StatusOK, service.SessionSummary{Session: session, Metrics: domain.CalculateMetrics(session)})
		}
		return
	}
	switch parts[1] {
	case "turns":
		data, ok := execute(agent, w, r, protocol.SessionGet, service.SessionArguments{SessionID: parts[0]})
		if ok {
			writeJSON(w, http.StatusOK, data.(domain.Session).Turns)
		}
	case "events":
		data, ok := execute(agent, w, r, protocol.SessionTimeline, service.TimelineArguments{SessionID: parts[0], Limit: 1000})
		if ok {
			writeJSON(w, http.StatusOK, data.(service.SessionTimelineResponse).Events)
		}
	case "findings":
		data, ok := execute(agent, w, r, protocol.SessionFindings, service.SessionArguments{SessionID: parts[0]})
		if ok {
			writeJSON(w, http.StatusOK, data)
		}
	case "metrics":
		data, ok := execute(agent, w, r, protocol.SessionMetrics, service.SessionArguments{SessionID: parts[0]})
		if ok {
			writeJSON(w, http.StatusOK, data)
		}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func compare(agent *service.Service, w http.ResponseWriter, r *http.Request) {
	body, _ := json.Marshal(service.CompareArguments{A: r.URL.Query().Get("a"), B: r.URL.Query().Get("b")})
	response := agent.Execute(r.Context(), protocol.Request{Version: protocol.Version, RequestID: "http", Operation: protocol.SessionCompare, Arguments: body})
	if response.Status != protocol.StatusOK {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "two valid session ids are required"})
		return
	}
	writeJSON(w, http.StatusOK, response.Data)
}

func execute(agent *service.Service, w http.ResponseWriter, r *http.Request, operation protocol.Operation, arguments any) (any, bool) {
	body, err := json.Marshal(arguments)
	if err != nil {
		apiError(w, err)
		return nil, false
	}
	response := agent.Execute(r.Context(), protocol.Request{Version: protocol.Version, RequestID: "http", Operation: operation, Arguments: body})
	if response.Status == protocol.StatusOK {
		return response.Data, true
	}
	message := "operation failed"
	if response.Error != nil {
		message = response.Error.Message
	}
	writeJSON(w, statusFor(response.Status), map[string]string{"error": message})
	return nil, false
}

func statusFor(status protocol.Status) int {
	switch status {
	case protocol.StatusInvalidRequest:
		return http.StatusBadRequest
	case protocol.StatusDenied:
		return http.StatusForbidden
	case protocol.StatusNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func receiveHook(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	provider, ok := hooks.ParseProvider(strings.TrimPrefix(r.URL.Path, "/internal/hooks/"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown hook provider"})
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid hook payload"})
		return
	}
	now := time.Now().UTC()
	event, err := hooks.Normalize(provider, payload, now, strconv.FormatInt(now.UnixNano(), 10), false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := store.AppendHookEvent(r.Context(), event); err != nil {
		apiError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func apiError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
