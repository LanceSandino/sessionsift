package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestSaveImportIsIdempotentAndQueryable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	session := domain.Session{ID: "s1", ExternalSessionID: "s1", Title: "First", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion, Turns: []domain.Turn{{ID: "t1", SessionID: "s1", Sequence: 1}}, ToolCalls: []domain.ToolCall{{ID: "c1", SessionID: "s1", Success: true}}, Findings: []domain.Finding{{ID: "f1", SessionID: "s1", FindingType: "test", Confidence: domain.Observed}}}
	if err := store.SaveImport(ctx, "path", "one", session); err != nil {
		t.Fatal(err)
	}
	needed, err := store.ImportNeeded(ctx, "path", "one")
	if err != nil || needed {
		t.Fatalf("needed=%v err=%v", needed, err)
	}
	session.Title = "Updated"
	if err := store.SaveImport(ctx, "path", "two", session); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, "s1")
	if err != nil || got.Title != "Updated" || len(got.ToolCalls) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	list, err := store.ListSessions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestGetMissingSession(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.GetSession(context.Background(), "missing"); err == nil {
		t.Fatal("expected missing session error")
	}
}

func TestImportNeededAndHookPlaceholder(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	needed, err := store.ImportNeeded(ctx, "new", "fingerprint")
	if err != nil || !needed {
		t.Fatalf("needed=%v err=%v", needed, err)
	}
	event := domain.Event{ID: "e1", SessionID: "live", Timestamp: time.Now().UTC(), EventType: "hook", Source: "hook"}
	if err := store.AppendHookEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	session, err := store.GetSession(ctx, "live")
	if err != nil || session.Status != "live" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if err := store.AppendHookEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	event2 := domain.Event{ID: "e2", SessionID: "live", Timestamp: event.Timestamp.Add(time.Second), EventType: "hook", EventSubtype: "UserPromptSubmit", Source: "hook", PayloadRedacted: map[string]any{"prompt": "Fix the live session", "model": "gpt-live"}}
	if err := store.AppendHookEvent(ctx, event2); err != nil {
		t.Fatal(err)
	}
	session, err = store.GetSession(ctx, "live")
	if err != nil || len(session.Events) != 2 || session.Title != "Fix the live session" || session.Model != "gpt-live" {
		t.Fatalf("live session=%+v err=%v", session, err)
	}
	otel := domain.Event{ID: "otel", SessionID: "live", TurnID: "turn-1", Timestamp: event.Timestamp.Add(2 * time.Second), EventType: "otel", EventSubtype: "codex.sse_event", Source: "otel", PayloadRedacted: map[string]any{"input_tokens": int64(120), "cached_input_tokens": int64(80), "output_tokens": int64(20)}}
	if err := store.AppendHookEvent(ctx, otel); err != nil {
		t.Fatal(err)
	}
	session, err = store.GetSession(ctx, "live")
	if err != nil || len(session.Turns) != 1 || session.Turns[0].TokenSource != "otel" || session.Turns[0].InputTokens != 120 {
		t.Fatalf("OTel session=%+v err=%v", session, err)
	}
}
