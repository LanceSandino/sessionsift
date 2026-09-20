package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

func TestExecuteProvidesCanonicalReadOperations(t *testing.T) {
	store := serviceStore(t)
	agent := New(store, LocalAuthorizer{})
	cases := []struct {
		operation protocol.Operation
		arguments any
	}{
		{protocol.SessionList, ListArguments{}},
		{protocol.SessionGet, SessionArguments{SessionID: "s1"}},
		{protocol.SessionMetrics, SessionArguments{SessionID: "s1"}},
		{protocol.SessionTimeline, TimelineArguments{SessionID: "s1", Limit: 10}},
		{protocol.SessionFindings, SessionArguments{SessionID: "s1"}},
		{protocol.SessionCompare, CompareArguments{A: "s1", B: "s2"}},
		{protocol.TurnGet, TurnArguments{SessionID: "s1", TurnID: "t1"}},
		{protocol.ConnectionStatus, nil},
	}
	for index, item := range cases {
		body, err := json.Marshal(item.arguments)
		if err != nil {
			t.Fatal(err)
		}
		response := agent.Execute(context.Background(), protocol.Request{Version: protocol.Version, RequestID: "req_" + string(rune('a'+index)), Operation: item.operation, Arguments: body})
		if response.Status != protocol.StatusOK || response.Data == nil || response.Error != nil {
			t.Fatalf("operation=%s response=%+v", item.operation, response)
		}
		if item.operation == protocol.SessionCompare {
			comparison, ok := response.Data.(SessionComparison)
			if !ok || len(comparison.Differences) != 6 {
				t.Fatalf("comparison=%+v", response.Data)
			}
		}
	}
}

func TestExecuteRejectsUnknownDeniedAndInvalidRequests(t *testing.T) {
	store := serviceStore(t)
	agent := New(store, denyAuthorizer{})
	denied := agent.Execute(context.Background(), protocol.Request{Version: protocol.Version, RequestID: "req_denied", Operation: protocol.SessionList})
	if denied.Status != protocol.StatusDenied {
		t.Fatalf("denied=%+v", denied)
	}
	unknown := agent.Execute(context.Background(), protocol.Request{Version: protocol.Version, RequestID: "req_unknown", Operation: "shell.exec"})
	if unknown.Status != protocol.StatusInvalidRequest {
		t.Fatalf("unknown=%+v", unknown)
	}
	missing := New(store, LocalAuthorizer{}).Execute(context.Background(), protocol.Request{Version: protocol.Version, RequestID: "req_missing", Operation: protocol.SessionGet, Arguments: json.RawMessage(`{"session_id":"missing"}`)})
	if missing.Status != protocol.StatusNotFound {
		t.Fatalf("missing=%+v", missing)
	}
}

func TestExecuteBoundsTimelineAndFiltersInternalReviews(t *testing.T) {
	store := serviceStore(t)
	now := time.Now().UTC()
	internal := domain.Session{ID: "review", ExternalSessionID: "review", Title: "Review", Kind: domain.InternalReviewSession, Source: domain.SourceCodex, StartedAt: now, EndedAt: now, Status: "completed", ParserVersion: domain.ParserVersion}
	if err := store.SaveImport(context.Background(), "review-path", "review-fingerprint", internal); err != nil {
		t.Fatal(err)
	}
	agent := NewLocal(store)
	list := agent.Execute(context.Background(), request(t, "list", protocol.SessionList, ListArguments{}))
	if items := list.Data.(SessionListResponse); len(items.Sessions) != 2 || items.Total != 2 || items.HasMore {
		t.Fatalf("visible items=%+v", items)
	}
	all := agent.Execute(context.Background(), request(t, "all", protocol.SessionList, ListArguments{IncludeInternal: true}))
	if items := all.Data.(SessionListResponse); len(items.Sessions) != 3 || items.Total != 3 {
		t.Fatalf("all items=%+v", items)
	}
	paged := agent.Execute(context.Background(), request(t, "paged", protocol.SessionList, ListArguments{Limit: 1}))
	if items := paged.Data.(SessionListResponse); len(items.Sessions) != 1 || !items.HasMore || items.NextCursor == nil || *items.NextCursor != 1 {
		t.Fatalf("paged items=%+v", items)
	}
	timeline := agent.Execute(context.Background(), request(t, "timeline", protocol.SessionTimeline, TimelineArguments{SessionID: "s1", Limit: 1}))
	data := timeline.Data.(SessionTimelineResponse)
	if !data.Truncated || len(data.Events)+len(data.ToolCalls) != 1 {
		t.Fatalf("timeline=%+v", data)
	}
}

func TestExecuteCoversFailureBoundaries(t *testing.T) {
	store := serviceStore(t)
	agent := NewLocal(store)
	requests := []protocol.Request{
		request(t, "bad-list", protocol.SessionList, map[string]any{"unexpected": true}),
		request(t, "bad-timeline", protocol.SessionTimeline, TimelineArguments{SessionID: "s1", Limit: -1}),
		request(t, "missing-findings", protocol.SessionFindings, SessionArguments{SessionID: "missing"}),
		request(t, "missing-turn", protocol.TurnGet, TurnArguments{SessionID: "s1", TurnID: "missing"}),
		request(t, "missing-compare", protocol.SessionCompare, CompareArguments{A: "s1", B: "missing"}),
		request(t, "disconnect", protocol.ConnectionDisconnect, nil),
	}
	for _, item := range requests {
		response := agent.Execute(context.Background(), item)
		if response.Status == protocol.StatusOK || response.Error == nil {
			t.Fatalf("request=%s response=%+v", item.RequestID, response)
		}
	}
	unavailable := New(nil, nil).Execute(context.Background(), request(t, "unavailable", protocol.SessionList, nil))
	if unavailable.Status != protocol.StatusError {
		t.Fatalf("unavailable=%+v", unavailable)
	}
}

func TestExecuteEnforcesResponseLimit(t *testing.T) {
	huge := domain.Session{ID: "huge", ExternalSessionID: "huge", Title: string(make([]byte, (2<<20)+1)), Kind: domain.DeveloperSession}
	agent := NewLocal(staticRepository{sessions: []domain.Session{huge}})
	response := agent.Execute(context.Background(), request(t, "huge", protocol.SessionList, ListArguments{}))
	if response.Status != protocol.StatusError || response.Error == nil || response.Error.Code != "response_limit" {
		t.Fatalf("response=%+v", response)
	}
}

type staticRepository struct {
	sessions []domain.Session
}

func (r staticRepository) ListSessions(context.Context) ([]domain.Session, error) {
	return r.sessions, nil
}
func (r staticRepository) GetSession(context.Context, string) (domain.Session, error) {
	return domain.Session{}, errors.New("missing")
}

func request(t *testing.T, id string, operation protocol.Operation, arguments any) protocol.Request {
	t.Helper()
	body, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Request{Version: protocol.Version, RequestID: "req_" + id, Operation: operation, Arguments: body}
}

type denyAuthorizer struct{}

func (denyAuthorizer) Authorize(context.Context, protocol.Operation) error {
	return errors.New("denied")
}

func serviceStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for index, id := range []string{"s1", "s2"} {
		session := domain.Session{
			ID: id, ExternalSessionID: id, Title: id, Kind: domain.DeveloperSession, Source: domain.SourceCodex,
			StartedAt: now.Add(time.Duration(index) * time.Minute), EndedAt: now.Add(time.Duration(index+1) * time.Minute), Status: "completed", ParserVersion: domain.ParserVersion,
			Turns:     []domain.Turn{{ID: "t" + string(rune('1'+index)), SessionID: id, Sequence: 1, StartedAt: now, InputTokens: 100, CachedInputTokens: 50}},
			Events:    []domain.Event{{ID: "e" + id, SessionID: id, TurnID: "t" + string(rune('1'+index)), Timestamp: now, EventType: "prompt"}},
			ToolCalls: []domain.ToolCall{{ID: "c" + id, SessionID: id, TurnID: "t" + string(rune('1'+index)), Timestamp: now, ToolName: "read_file", ToolCategory: "read", Success: true}},
			Findings:  []domain.Finding{{ID: "f" + id, SessionID: id, FindingType: "test", Confidence: domain.Observed}},
		}
		if err := store.SaveImport(context.Background(), "path-"+id, "fingerprint-"+id, session); err != nil {
			t.Fatal(err)
		}
	}
	return store
}
