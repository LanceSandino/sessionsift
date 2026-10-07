package export

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// All fixtures are synthetic; no real Claude.ai export is read.

func parseOne(t *testing.T, input string) domain.Session {
	t.Helper()
	sessions, err := Parse(strings.NewReader(input))
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	return sessions[0]
}

func encode(t *testing.T, session domain.Session) string {
	t.Helper()
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const basicExport = `[{
  "uuid": "conv-1", "name": "Plan a launch", "summary": "PRIVATE_SUMMARY",
  "created_at": "2026-03-01T10:00:00.000000Z", "updated_at": "2026-03-01T10:05:00Z",
  "account": {"uuid": "acct-PRIVATE-UUID"},
  "chat_messages": [
    {"uuid": "m1", "sender": "human", "text": "Draft the launch plan", "created_at": "2026-03-01T10:00:10Z",
     "content": [{"type": "text", "text": "Draft the launch plan", "start_timestamp": "2026-03-01T10:00:10Z", "citations": []}],
     "attachments": [{"file_name": "brief.pdf", "file_size": 1200, "file_type": "pdf", "extracted_content": "EXTRACTED_PRIVATE_TEXT"}],
     "files": [{"file_name": "chart.png"}]},
    {"uuid": "m2", "sender": "assistant", "text": "ignored when content exists", "created_at": "2026-03-01T10:00:30Z",
     "content": [
       {"type": "thinking", "thinking": "PRIVATE_THINKING"},
       {"type": "text", "text": "Here is the plan."},
       {"type": "tool_use", "id": "tu1", "name": "web_search", "input": {"query": "launch checklist"}},
       {"type": "tool_result", "tool_use_id": "tu1", "name": "web_search", "content": [{"type": "text", "text": "results"}], "is_error": false},
       {"type": "tool_use", "name": "mcp__drive__read_file", "input": {"id": "doc"}, "start_timestamp": "2026-03-01T10:00:20Z"},
       {"type": "tool_result", "name": "mcp__drive__read_file", "content": "permission denied", "is_error": true},
       {"type": "text", "text": "Done."}
     ]},
    {"uuid": "m3", "sender": "human", "text": "Thanks", "created_at": "2026-03-01T10:01:00Z"},
    {"uuid": "m4", "sender": "assistant", "text": "You're welcome.", "created_at": "2026-03-01T10:01:10Z"}
  ]
}]`

func TestParseMapsConversationTurnsAndTools(t *testing.T) {
	session := parseOne(t, basicExport)
	if session.ID != "claude:conv-1" || session.ExternalSessionID != "conv-1" || session.Title != "Plan a launch" || session.TitleSource != "export" {
		t.Fatalf("identity=%+v", session)
	}
	if session.Kind != domain.ChatSession || session.Source != domain.SourceClaude || session.ParserVersion != domain.ClaudeExportVersion || session.SourceVersion != "claude-export" || session.Status != "completed" {
		t.Fatalf("provenance=%+v", session)
	}
	if session.StartedAt.Format("15:04:05") != "10:00:00" || session.EndedAt.Format("15:04:05") != "10:05:00" {
		t.Fatalf("bounds=%s..%s", session.StartedAt, session.EndedAt)
	}
	if len(session.Turns) != 2 {
		t.Fatalf("turns=%+v", session.Turns)
	}
	one := session.Turns[0]
	if one.ID != "claude:conv-1:m1" || one.ExternalTurnID != "m1" || one.UserPromptRedacted != "Draft the launch plan" || one.AssistantSummaryRedacted != "Here is the plan.\nDone." || one.EndedAt.Format("15:04:05") != "10:00:30" {
		t.Fatalf("turn one=%+v", one)
	}
	if one.InputTokens != 0 || one.OutputTokens != 0 || session.Model != "" {
		t.Fatalf("exports carry no usage: %+v", one)
	}
	if session.Turns[1].AssistantSummaryRedacted != "You're welcome." || session.Turns[1].Sequence != 2 {
		t.Fatalf("turn two=%+v", session.Turns[1])
	}
	if len(session.ToolCalls) != 2 {
		t.Fatalf("tools=%+v", session.ToolCalls)
	}
	search, drive := session.ToolCalls[0], session.ToolCalls[1]
	if search.ID != "claude:conv-1:tool:tu1" || search.ToolCategory != "other" || search.ToolTransport != "builtin" || !search.Success || search.ResultPreviewRedacted != "results" || search.ArgumentsRedacted != `{"query":"launch checklist"}` || search.TurnID != one.ID {
		t.Fatalf("search=%+v", search)
	}
	if drive.ID != "claude:conv-1:tool:m2-5" || drive.ToolTransport != "mcp" || drive.ToolServer != "drive" || drive.Success || drive.ResultPreviewRedacted != "permission denied" || drive.Timestamp.Format("15:04:05") != "10:00:20" {
		t.Fatalf("drive=%+v", drive)
	}
	if len(session.Events) != 4 || session.Events[0].ID != "claude:conv-1:event:m1" || session.Events[0].Source != "claude_export" || session.Events[1].EventType != "tool" {
		t.Fatalf("events=%+v", session.Events)
	}
	attachments, ok := session.Events[0].PayloadRedacted["attachments"].(map[string]any)
	if !ok || attachments["count"] != 2 || strings.Join(attachments["file_names"].([]string), ",") != "brief.pdf,chart.png" {
		t.Fatalf("attachments=%v", session.Events[0].PayloadRedacted)
	}
	encoded := encode(t, session)
	for _, private := range []string{"EXTRACTED_PRIVATE_TEXT", "PRIVATE_THINKING", "PRIVATE_SUMMARY", "acct-PRIVATE-UUID", "ignored when content exists"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("export retained %q", private)
		}
	}
}

func TestParseFallbackTitlesAndTimestamps(t *testing.T) {
	input := `[
  {"uuid": "", "name": "no id"},
  "not a conversation",
  {"uuid": "c2", "name": "  ", "created_at": "2026-01-01T00:00:00Z", "chat_messages": [
    {"uuid": "a0", "sender": "assistant", "text": "Hello before any prompt"},
    {"uuid": "h1", "sender": "human", "text": "What is a monad?", "created_at": "bad time"},
    {"uuid": "x", "sender": "system", "text": "ignored"}
  ]},
  {"uuid": "c3", "chat_messages": [{"uuid": "h", "sender": "human", "content": [{"type": "image"}]}]},
  {"uuid": "c4", "name": 42, "chat_messages": [{"uuid": "h", "sender": "human", "text": "Drifted name type", "created_at": "2026-02-01T00:00:00Z"}]}
]`
	sessions, err := Parse(strings.NewReader(input))
	if err != nil || len(sessions) != 3 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	prompt, empty, drifted := sessions[0], sessions[1], sessions[2]
	if prompt.Title != "What is a monad?" || prompt.TitleSource != "first_prompt" || len(prompt.Turns) != 1 || !prompt.Turns[0].StartedAt.Equal(prompt.StartedAt) {
		t.Fatalf("prompt=%+v", prompt)
	}
	if !prompt.EndedAt.Equal(prompt.StartedAt) || prompt.Turns[0].AssistantSummaryRedacted != "" {
		t.Fatalf("prompt bounds=%+v", prompt)
	}
	if empty.Title != "Untitled Claude conversation" || empty.TitleSource != "fallback" || !empty.StartedAt.IsZero() || !empty.EndedAt.IsZero() {
		t.Fatalf("empty=%+v", empty)
	}
	if drifted.ID != "claude:c4" || drifted.Title != "Drifted name type" || drifted.StartedAt.IsZero() {
		t.Fatalf("drifted=%+v", drifted)
	}
}

func TestParseRedactsSecretsInNameAndText(t *testing.T) {
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz0123" // gitleaks:allow -- synthetic redaction fixture
	session := parseOne(t, `[{"uuid": "c1", "name": "Rotate `+secret+`", "chat_messages": [
    {"uuid": "h1", "sender": "human", "text": "My token is `+secret+`", "attachments": [{"file_name": "`+secret+`.txt"}]},
    {"uuid": "a1", "sender": "assistant", "content": [{"type": "text", "text": "Revoke `+secret+`"}, {"type": "tool_use", "id": "t", "name": "run", "input": {"token": "`+secret+`"}}]}
  ]}]`)
	if session.Title != "Rotate [REDACTED]" || !strings.Contains(session.Turns[0].UserPromptRedacted, "[REDACTED]") {
		t.Fatalf("session=%+v", session)
	}
	if encoded := encode(t, session); strings.Contains(encoded, secret) || strings.Contains(encoded, "ghp_") {
		t.Fatalf("secret leaked: %s", encoded)
	}
}

func TestParseToolPairingEdgeCases(t *testing.T) {
	session := parseOne(t, `[{"uuid": "c1", "chat_messages": [
    {"uuid": "a0", "sender": "assistant", "content": [
      {"type": "tool_result", "tool_use_id": "early", "name": "search", "content": "orphan one"},
      {"type": "tool_result", "tool_use_id": "early", "content": 7}
    ]},
    {"uuid": "h1", "sender": "human", "text": "go"},
    {"uuid": "a1", "sender": "assistant", "content": [
      {"type": "tool_use", "id": "dup", "name": "search", "input": {}},
      {"type": "tool_use", "id": "dup", "name": "search", "input": {}},
      {"type": "tool_use", "name": "", "input": "{not json"},
      {"type": "tool_result", "name": "search", "content": "by name", "is_error": false},
      {"type": "tool_result", "content": "nameless", "is_error": true},
      {"type": "text", "text": "   "}
    ]},
    {"uuid": "h1", "sender": "human", "text": "duplicate uuid"}
  ]}]`)
	if len(session.ToolCalls) != 5 {
		t.Fatalf("tools=%+v", session.ToolCalls)
	}
	orphan, second, dup, unnamed, nameless := session.ToolCalls[0], session.ToolCalls[1], session.ToolCalls[2], session.ToolCalls[3], session.ToolCalls[4]
	if orphan.ID != "claude:c1:tool:early:result" || orphan.TurnID != "" || orphan.ToolName != "search" || orphan.ResultPreviewRedacted != "orphan one" {
		t.Fatalf("orphan=%+v", orphan)
	}
	if second.ID != "claude:c1:tool:early:result:a0-2" || second.ToolName != "unknown_tool" || second.ResultPreviewRedacted != "" {
		t.Fatalf("second orphan=%+v", second)
	}
	if dup.ID != "claude:c1:tool:dup" || dup.ResultPreviewRedacted != "by name" || !dup.Success || dup.TurnID != "claude:c1:h1" {
		t.Fatalf("dup=%+v", dup)
	}
	if unnamed.ToolName != "unknown_tool" || unnamed.ArgumentsRedacted != `"{not json"` || unnamed.ID != "claude:c1:tool:a1-3" {
		t.Fatalf("unnamed=%+v", unnamed)
	}
	if nameless.ToolName != "unknown_tool" || nameless.Success || nameless.ResultPreviewRedacted != "nameless" {
		t.Fatalf("nameless=%+v", nameless)
	}
	if len(session.Turns) != 2 || session.Turns[1].ID != "claude:c1:h1:4" || session.Turns[0].AssistantSummaryRedacted != "" {
		t.Fatalf("turns=%+v", session.Turns)
	}
	if session.Events[len(session.Events)-1].ID != "claude:c1:event:h1:4" {
		t.Fatalf("events=%+v", session.Events)
	}
}

func TestParseRejectsInvalidDocuments(t *testing.T) {
	for name, input := range map[string]string{
		"object root":      `{"not":"an array"}`,
		"empty":            ``,
		"truncated":        `[{"uuid":"c1"`,
		"unterminated":     `[{"uuid":"c1"}`,
		"broken element":   `[{"uuid": }]`,
		"trailing garbage": `[{"uuid":"c1"},]`,
	} {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	sessions, err := Parse(strings.NewReader(`[]`))
	if err != nil || len(sessions) != 0 {
		t.Fatalf("empty array: sessions=%v err=%v", sessions, err)
	}
}

func TestHelpers(t *testing.T) {
	if compactJSON(nil) != "" || compactJSON(json.RawMessage("{bad")) != "{bad" {
		t.Fatal("compactJSON")
	}
	if contentText(json.RawMessage(`{"x":1}`)) != "" || contentText(json.RawMessage(`[{"type":"image"},{"type":"text","text":"a"},{"type":"text","text":"b"}]`)) != "a\nb" {
		t.Fatal("contentText")
	}
	if decodeLenient(nil, &conversation{}) {
		t.Fatal("empty decoded")
	}
}
