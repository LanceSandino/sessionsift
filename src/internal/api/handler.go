// Package api serves the localhost JSON API, hook receiver, and embedded UI.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/codex/otel"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
	"github.com/LanceSandino/sessionsift/src/webui"
)

// Handler returns the complete localhost application handler.
func Handler(store *storage.Store) http.Handler {
	assets, _ := fs.Sub(webui.Assets, "dist")
	static := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			listSessions(store, w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
			sessionRoute(store, w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/compare":
			compare(store, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/internal/hooks/codex":
			receiveHook(store, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/logs":
			receiveOTel(store, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/metrics":
			receiveOTelMetrics(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case r.Method == http.MethodGet:
			if strings.Contains(filepathBase(r.URL.Path), ".") {
				static.ServeHTTP(w, r)
				return
			}
			r.URL.Path = "/"
			static.ServeHTTP(w, r)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		}
	})
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

func listSessions(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	sessions, err := store.ListSessions(r.Context())
	if err != nil {
		apiError(w, err)
		return
	}
	type item struct {
		domain.Session
		Metrics domain.Metrics `json:"metrics"`
	}
	items := make([]item, 0, len(sessions))
	for _, session := range sessions {
		if session.Kind == domain.InternalReviewSession && r.URL.Query().Get("include_internal") != "true" {
			continue
		}
		metrics := domain.CalculateMetrics(session)
		session.CWD = ""
		session.Turns = nil
		session.Events = nil
		session.ToolCalls = nil
		session.Findings = nil
		items = append(items, item{Session: session, Metrics: metrics})
	}
	writeJSON(w, http.StatusOK, items)
}

func sessionRoute(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session id required"})
		return
	}
	session, err := store.GetSession(r.Context(), parts[0])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if len(parts) == 1 {
		writeJSON(w, http.StatusOK, session)
		return
	}
	switch parts[1] {
	case "turns":
		writeJSON(w, http.StatusOK, session.Turns)
	case "events":
		writeJSON(w, http.StatusOK, session.Events)
	case "findings":
		writeJSON(w, http.StatusOK, session.Findings)
	case "metrics":
		writeJSON(w, http.StatusOK, domain.CalculateMetrics(session))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func compare(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	a, errA := store.GetSession(r.Context(), r.URL.Query().Get("a"))
	b, errB := store.GetSession(r.Context(), r.URL.Query().Get("b"))
	if errA != nil || errB != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "two valid session ids are required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"a": a, "b": b, "metrics_a": domain.CalculateMetrics(a), "metrics_b": domain.CalculateMetrics(b), "differences": observableDifferences(a, b)})
}

func observableDifferences(a, b domain.Session) []string {
	ma, mb := domain.CalculateMetrics(a), domain.CalculateMetrics(b)
	var values []string
	values = append(values, fmt.Sprintf("Session A used %d tool calls; Session B used %d.", ma.ToolCalls, mb.ToolCalls))
	values = append(values, fmt.Sprintf("Session A cache reuse was %.0f%%; Session B was %.0f%%.", ma.CacheRatio*100, mb.CacheRatio*100))
	values = append(values, fmt.Sprintf("Session A recorded %d failed calls; Session B recorded %d.", ma.FailedToolCalls, mb.FailedToolCalls))
	values = append(values, fmt.Sprintf("Session A performed %d read/search calls; Session B performed %d.", ma.Reads+ma.Searches, mb.Reads+mb.Searches))
	values = append(values, fmt.Sprintf("Session A reached its first edit after %s; Session B after %s.", observedDuration(ma.TimeToFirstEditMS), observedDuration(mb.TimeToFirstEditMS)))
	values = append(values, fmt.Sprintf("Session A had %d possible scope corrections; Session B had %d.", findingCount(a, "possible_scope_correction"), findingCount(b, "possible_scope_correction")))
	return values
}

func observedDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "no observed edit"
	}
	return time.Duration(milliseconds * int64(time.Millisecond)).Round(time.Second).String()
}

func findingCount(session domain.Session, kind string) int {
	count := 0
	for _, finding := range session.Findings {
		if finding.FindingType == kind {
			count++
		}
	}
	return count
}

func receiveHook(store *storage.Store, w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid hook payload"})
		return
	}
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}
	turnID, _ := payload["turn_id"].(string)
	kind, _ := payload["hook_event_name"].(string)
	if kind == "" {
		kind = "hook"
	}
	redacted := map[string]any{}
	for key, value := range payload {
		if key == "prompt" || key == "tool_input" || key == "tool_response" {
			redacted[key] = redact.Text(fmt.Sprint(value), 2000)
		} else if key != "transcript_path" && key != "cwd" {
			redacted[key] = value
		}
	}
	now := time.Now().UTC()
	event := domain.Event{ID: redact.Hash(fmt.Sprintf("%s:%s:%d", sessionID, kind, now.UnixNano()))[:24], SessionID: sessionID, TurnID: turnID, Timestamp: now, EventType: "hook", EventSubtype: kind, Source: "hook", PayloadRedacted: redacted}
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
func filepathBase(path string) string {
	path = strings.TrimSuffix(path, "/")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
