package transcript

import (
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestCompactionAndAPIErrorEventsOmitContent(t *testing.T) {
	session := parseRecords(t, false,
		userRecord(1, "p1", "Long task"),
		fields{"type": "system", "subtype": "compact_boundary", "timestamp": at(2), "level": "info", "content": "Conversation compacted", "compactMetadata": fields{"trigger": "auto", "preTokens": 180000, "postTokens": 12000, "durationMs": 4500}},
		with(userRecord(3, "p1-c", "Summary PRIVATE_SUMMARY"), fields{"isCompactSummary": true}),
		fields{"type": "system", "subtype": "api_error", "timestamp": at(4), "level": "error", "error": fields{"message": "PRIVATE_ERROR_TEXT", "formatted": "PRIVATE_FORMATTED"}, "retryAttempt": 1},
		fields{"type": "system", "subtype": "compact_boundary", "timestamp": at(5)},
	)
	if len(session.Events) != 4 {
		t.Fatalf("events=%+v", session.Events)
	}
	compaction, apiError, bare := session.Events[1], session.Events[2], session.Events[3]
	if compaction.EventType != "compaction" || compaction.EventSubtype != "compact_boundary" || compaction.DurationMS != 4500 || compaction.TurnID != session.Turns[0].ID {
		t.Fatalf("compaction=%+v", compaction)
	}
	if compaction.PayloadRedacted["trigger"] != "auto" || compaction.PayloadRedacted["pre_tokens"] != int64(180000) || compaction.PayloadRedacted["post_tokens"] != int64(12000) {
		t.Fatalf("compaction payload=%v", compaction.PayloadRedacted)
	}
	if apiError.EventType != "error" || apiError.EventSubtype != "api_error" || len(apiError.PayloadRedacted) != 1 || apiError.PayloadRedacted["level"] != "error" {
		t.Fatalf("api error=%+v", apiError)
	}
	if bare.PayloadRedacted != nil || bare.DurationMS != 0 {
		t.Fatalf("bare compaction=%+v", bare)
	}
	encoded := marshal(t, session)
	for _, private := range []string{"PRIVATE_ERROR_TEXT", "PRIVATE_FORMATTED", "PRIVATE_SUMMARY", "Conversation compacted"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("retained %q", private)
		}
	}
	if metrics := domain.CalculateMetrics(session); metrics.Compactions != 2 {
		t.Fatalf("compactions=%d", metrics.Compactions)
	}
}

func TestMultipleToolBlocksDuplicatesAndOrphanAssistant(t *testing.T) {
	multi := fields{"type": "assistant", "timestamp": at(2), "message": fields{"id": "m1", "model": "claude-opus-5", "content": []any{
		toolUseBlock("t1", "Glob", fields{"path": "/work/repo/src"}),
		toolUseBlock("t1", "Glob", fields{"path": "/work/repo/src"}),
		toolUseBlock("", "mcp__github__create_issue", fields{"title": "x"}),
	}}}
	session := parseRecords(t, false,
		assistantRecord(0.5, "m0", "claude-opus-5", toolUseBlock("t0", "Skill", fields{"skill": "wrapup"}), fields{"input_tokens": 9}),
		userRecord(1, "p1", "Search"),
		multi,
	)
	if len(session.ToolCalls) != 3 || len(session.Events) != 4 {
		t.Fatalf("tools=%+v events=%+v", session.ToolCalls, session.Events)
	}
	orphan, glob, mcp := session.ToolCalls[0], session.ToolCalls[1], session.ToolCalls[2]
	if orphan.TurnID != "" || orphan.ToolCategory != "execute" || orphan.CommandSignature != `{"skill":"wrapup"}` || session.Events[0].TurnID != "" {
		t.Fatalf("orphan=%+v", orphan)
	}
	if glob.ToolCategory != "search" || glob.TargetPathDisplay != "repo/src" {
		t.Fatalf("glob=%+v", glob)
	}
	prefix := "claude:" + testSession + ":"
	if mcp.ID != prefix+"evt-000003:tool-3" || mcp.ToolTransport != "mcp" || mcp.ToolServer != "github" || mcp.ToolCategory != "other" {
		t.Fatalf("mcp=%+v", mcp)
	}
	if session.Events[2].ID != "evt-000003" || session.Events[3].ID != "evt-000003:tool-3" {
		t.Fatalf("event ids=%+v", session.Events)
	}
	if session.Turns[0].InputTokens != 0 {
		t.Fatalf("usage before first prompt attributed: %+v", session.Turns[0])
	}
}
