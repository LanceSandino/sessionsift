package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestHealthAndBackupProduceRestorablePrivateDatabase(t *testing.T) {
	directory := t.TempDir()
	livePath := filepath.Join(directory, "live.db")
	store, err := Open(livePath)
	if err != nil {
		t.Fatal(err)
	}
	info, statErr := os.Stat(livePath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("live database mode=%v", info.Mode().Perm())
	}
	ctx := context.Background()
	now := time.Now().UTC()
	session := domain.Session{ID: "backup-session", ExternalSessionID: "backup-session", Title: "Backed up", StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion}
	if err := store.SaveImport(ctx, "backup-path", "one", session); err != nil {
		t.Fatal(err)
	}
	if err := store.Health(ctx); err != nil {
		t.Fatalf("healthy store: %v", err)
	}
	destination := filepath.Join(directory, "backups", "snapshot.db")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Backup(ctx, destination); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode=%v err=%v", info.Mode().Perm(), err)
	}
	restored, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.GetSession(ctx, session.ID)
	if err != nil || got.Title != session.Title {
		t.Fatalf("restored=%+v err=%v", got, err)
	}
	if err := store.Backup(ctx, destination); err == nil {
		t.Fatal("backup must not overwrite an existing snapshot")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Health(ctx); err == nil {
		t.Fatal("closed store reported healthy")
	}
}

func TestHealthIsNotStarvedByActiveIngestionConnection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = store.Health(ctx); err != nil {
		t.Fatalf("readiness was starved by active connection: %v", err)
	}
}

func TestHealthReturnsCachedDeepCheckWhileRefreshingInBackground(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	original := quickCheckDatabase
	quickCheckDatabase = func(context.Context, *sql.DB) error {
		close(started)
		<-release
		return nil
	}
	t.Cleanup(func() { quickCheckDatabase = original })
	store.healthMu.Lock()
	store.healthCheckedAt = time.Now().Add(-2 * healthCheckInterval)
	store.healthMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err = store.Health(ctx); err != nil {
		t.Fatalf("cached readiness failed: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("deep health refresh did not start")
	}
	close(release)
}

func TestCaptureStatsReportsOnlySourceCountsAndRecency(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	empty, err := store.CaptureStats(ctx)
	if err != nil || empty.OTelEvents != 0 || empty.HookEvents != 0 {
		t.Fatalf("empty stats=%+v err=%v", empty, err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, event := range []domain.Event{
		{ID: "capture-hook", SessionID: "capture-session", Timestamp: now, EventType: "hook", Source: "hook"},
		{ID: "capture-otel", SessionID: "capture-session", Timestamp: now.Add(time.Second), EventType: "otel", Source: "otel"},
	} {
		if err := store.AppendHookEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := store.CaptureStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.HookEvents != 1 || stats.OTelEvents != 1 || stats.LatestHookEventAt == nil || stats.LatestOTelEventAt == nil {
		t.Fatalf("stats=%+v", stats)
	}
	if !stats.LatestHookEventAt.Equal(now) || !stats.LatestOTelEventAt.Equal(now.Add(time.Second)) {
		t.Fatalf("unexpected recency: %+v", stats)
	}
}

// Regression anchor for CU-20260924-008: hooks replayed from the spool and Claude Code
// hooks are capture activity too; counting only live Codex hooks reported them as idle.
func TestCaptureStatsCountsSpooledAndClaudeCodeHooks(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sessionsift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, event := range []domain.Event{
		{ID: "codex-live", SessionID: "s", Timestamp: now, EventType: "hook", Source: "hook"},
		{ID: "codex-spooled", SessionID: "s", Timestamp: now.Add(time.Second), EventType: "hook", Source: "hook_spool"},
		{ID: "claude-live", SessionID: "c", Timestamp: now.Add(2 * time.Second), EventType: "hook", Source: "claude_hook"},
		{ID: "claude-spooled", SessionID: "c", Timestamp: now.Add(3 * time.Second), EventType: "hook", Source: "claude_hook_spool"},
	} {
		if err := store.AppendHookEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := store.CaptureStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.HookEvents != 2 || stats.LatestHookEventAt == nil || !stats.LatestHookEventAt.Equal(now.Add(time.Second)) {
		t.Fatalf("Codex hooks = %d at %v, want 2 at the spooled event", stats.HookEvents, stats.LatestHookEventAt)
	}
	if stats.ClaudeHookEvents != 2 || stats.LatestClaudeHookEventAt == nil || !stats.LatestClaudeHookEventAt.Equal(now.Add(3*time.Second)) {
		t.Fatalf("Claude Code hooks = %d at %v, want 2 at the spooled event", stats.ClaudeHookEvents, stats.LatestClaudeHookEventAt)
	}
}

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

func TestOpenFailsWhenParentDirectoryIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "test.db")
	if store, err := Open(path); err == nil {
		store.Close()
		t.Fatal("expected open to fail for a missing parent directory")
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

func TestReconcileChatGPTSourcePrunesOnlyUnreferencedSessions(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"chat-1", "chat-2"} {
		session := domain.Session{
			ID:                id,
			ExternalSessionID: id,
			Source:            domain.SourceChatGPT,
			Kind:              domain.ChatSession,
			Title:             id,
			StartedAt:         now,
			EndedAt:           now,
			ParserVersion:     domain.ChatGPTExportVersion,
		}
		if err := store.SaveImport(ctx, "path-"+id, "fingerprint", session); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.ReconcileChatGPTSource(ctx, "export-a", []string{"chat-1", "chat-2", "chat-2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileChatGPTSource(ctx, "export-b", []string{"chat-2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileChatGPTSource(ctx, "export-a", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetSession(ctx, "chat-1"); err == nil {
		t.Fatal("expected an unreferenced chat to be pruned")
	}
	if _, err := store.GetSession(ctx, "chat-2"); err != nil {
		t.Fatalf("expected shared chat to remain: %v", err)
	}
	needed, err := store.ImportNeeded(ctx, "path-chat-1", "fingerprint")
	if err != nil || !needed {
		t.Fatalf("expected pruned import to be needed again: needed=%v err=%v", needed, err)
	}

	if err := store.ReconcileChatGPTSource(ctx, "export-b", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession(ctx, "chat-2"); err == nil {
		t.Fatal("expected the last unreferenced chat to be pruned")
	}
}

func TestReconcileChatGPTSourceHonorsCanceledContext(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.ReconcileChatGPTSource(ctx, "export", nil); err == nil {
		t.Fatal("expected canceled context error")
	}
}

func TestMetricIntAndBoundedTitleVariants(t *testing.T) {
	cases := []struct {
		value any
		want  int64
		ok    bool
	}{
		{value: int64(12), want: 12, ok: true},
		{value: float64(13), want: 13, ok: true},
		{value: json.Number("14"), want: 14, ok: true},
		{value: json.Number("bad"), ok: false},
		{value: "15", ok: false},
	}
	for _, test := range cases {
		got, ok := metricInt(map[string]any{"tokens": test.value}, "missing", "tokens")
		if got != test.want || ok != test.ok {
			t.Fatalf("metricInt(%v)=(%d,%v), want (%d,%v)", test.value, got, ok, test.want, test.ok)
		}
	}
	if title := boundedTitle("  a   short\n title "); title != "a short title" {
		t.Fatalf("unexpected normalized title %q", title)
	}
	long := "123456789012345678901234567890123456789012345678901234567890123456789012345678901"
	if title := boundedTitle(long); len(title) != 80 || title[len(title)-3:] != "…" {
		t.Fatalf("unexpected bounded title %q", title)
	}
}
