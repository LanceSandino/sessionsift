package transcript

import (
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

func TestSteeringInterruptsAndInjectedNotifications(t *testing.T) {
	session := parseRecords(t, false,
		userRecord(1, "p1", "Build the feature"),
		assistantRecord(2, "m1", "claude-opus-5", textBlock("Working"), nil),
		userRecord(3, "p1", "Also add docs"),
		userRecord(4, "p1", []any{textBlock("[Request interrupted by user]")}),
		with(userRecord(10, "p2", "<task-notification>\n<summary>PRIVATE_TASK done</summary>\n</task-notification>"), fields{"origin": fields{"kind": "task-notification"}}),
		assistantRecord(11, "m2", "claude-opus-5", textBlock("The build finished."), nil),
		userRecord(12, "p2", "<task-notification>second PRIVATE_TASK</task-notification>"),
		with(userRecord(13, "p3", "Ship it"), fields{"origin": fields{"kind": "human"}}),
	)
	if len(session.Turns) != 3 || session.Turns[0].UserPromptRedacted != "Build the feature" || session.Turns[1].UserPromptRedacted != "" || session.Turns[2].UserPromptRedacted != "Ship it" {
		t.Fatalf("turns=%+v", session.Turns)
	}
	if session.Title != "Build the feature" || session.Turns[1].AssistantSummaryRedacted != "The build finished." {
		t.Fatalf("session=%+v", session)
	}
	want := []struct{ kind, subtype, turn string }{
		{"prompt", "", session.Turns[0].ID},
		{"message", "steering_or_queued", session.Turns[0].ID},
		{"message", "interrupted", session.Turns[0].ID},
		{"message", "task_notification", session.Turns[1].ID},
		{"message", "task_notification", session.Turns[1].ID},
		{"prompt", "", session.Turns[2].ID},
	}
	if len(session.Events) != len(want) {
		t.Fatalf("events=%+v", session.Events)
	}
	for index, item := range want {
		got := session.Events[index]
		if got.EventType != item.kind || got.EventSubtype != item.subtype || got.TurnID != item.turn {
			t.Fatalf("event %d=%+v want %+v", index, got, item)
		}
	}
	if session.Events[1].PayloadRedacted["text"] != "Also add docs" {
		t.Fatalf("steering payload=%v", session.Events[1].PayloadRedacted)
	}
	if strings.Contains(marshal(t, session), "PRIVATE_TASK") {
		t.Fatal("task notification content retained")
	}
}

func TestTurnKeyFallsBackToUUIDThenLine(t *testing.T) {
	session := parseRecords(t, false,
		userRecord(1, "", "First"),
		with(userRecord(2, "", "Second"), fields{"uuid": nil}),
	)
	if len(session.Turns) != 2 || session.Turns[0].ExternalTurnID != "uuid-1" || session.Turns[1].ExternalTurnID != "evt-000002" {
		t.Fatalf("turns=%+v", session.Turns)
	}
}

func TestToolResultsPairingSuccessAndDuration(t *testing.T) {
	session := parseRecords(t, false,
		userRecord(1, "p1", "Run things"),
		assistantRecord(2, "m1", "claude-opus-5", toolUseBlock("t1", "Bash", fields{"command": "make"}), nil),
		with(userRecord(2.5, "p1", []any{toolResultBlock("t1", "done", false)}), fields{"toolUseResult": fields{"stdout": "STDOUT_PRIVATE", "durationMs": 42}}),
		assistantRecord(3, "m2", "claude-opus-5", toolUseBlock("t2", "Bash", fields{"command": "sleep 100"}), nil),
		with(userRecord(9, "p1", []any{toolResultBlock("t2", "[Request interrupted by user for tool use]", false)}), fields{"toolUseResult": fields{"interrupted": true}}),
		assistantRecord(10, "m3", "claude-opus-5", toolUseBlock("t3", "Read", fields{"file_path": "/x/y.go"}), nil),
		userRecord(10.25, "p1", []any{toolResultBlock("t3", []any{textBlock("line one"), textBlock("line two")}, true)}),
		userRecord(11, "p1", []any{toolResultBlock("unknown", "orphan", false)}),
		assistantRecord(12, "m4", "claude-opus-5", toolUseBlock("t4", "Bash", fields{"command": "ls"}), nil),
		with(userRecord(11.5, "p1", []any{toolResultBlock("t4", "skewed clock", false), toolResultBlock("t5", "x", false)}), fields{"toolUseResult": fields{"durationMs": 7}}),
	)
	if len(session.ToolCalls) != 4 {
		t.Fatalf("tools=%+v", session.ToolCalls)
	}
	t1, t2, t3, t4 := session.ToolCalls[0], session.ToolCalls[1], session.ToolCalls[2], session.ToolCalls[3]
	if !t1.Success || t1.DurationMS != 42 || t1.ResultPreviewRedacted != "done" || t1.ContentFingerprint != redact.Hash("done") {
		t.Fatalf("t1=%+v", t1)
	}
	if t2.Success || t2.DurationMS != 6000 {
		t.Fatalf("interrupted t2=%+v", t2)
	}
	if t3.Success || t3.DurationMS != 250 || t3.ResultPreviewRedacted != "line one\nline two" {
		t.Fatalf("failed t3=%+v", t3)
	}
	if !t4.Success || t4.DurationMS != 0 || t4.ResultPreviewRedacted != "skewed clock" {
		t.Fatalf("t4=%+v", t4)
	}
	if strings.Contains(marshal(t, session), "STDOUT_PRIVATE") {
		t.Fatal("toolUseResult content retained")
	}
	if session.Turns[0].EndedAt.Format("15:04:05") != "10:00:12" {
		t.Fatalf("turn end=%s", session.Turns[0].EndedAt)
	}
}
