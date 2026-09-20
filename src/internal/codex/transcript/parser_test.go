package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestParseToleratesUnknownAndMalformedRecords(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-09-19T10:00:00Z","type":"session_meta","payload":{"session_id":"s1","cwd":"/private/payments-service","cli_version":"1.0","context_window":{"window_id":"w1"}}}`,
		`{"timestamp":"2026-09-19T10:00:01Z","type":"turn_context","payload":{"turn_id":"t1","model":"gpt-test","effort":"high"}}`,
		`{"timestamp":"2026-09-19T10:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>private metadata</environment_context>Fix auth without token ghp_abcdefghijklmnopqrstuvwxyz"}]}}`,
		`{"timestamp":"2026-09-19T10:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"c1","name":"read_file","arguments":"{\"path\":\"/private/payments-service/src/auth.go\"}"}}`,
		`{"timestamp":"2026-09-19T10:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"ok"}}`,
		`{"timestamp":"2026-09-19T10:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":10,"reasoning_output_tokens":3}}}}`,
		`{"timestamp":"2026-09-19T10:00:06Z","type":"future_record","payload":{"new":true}}`,
		`{broken`,
	}, "\n")

	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "s1" || session.Model != "gpt-test" || len(session.Turns) != 1 {
		t.Fatalf("unexpected session: %+v", session)
	}
	if !strings.HasPrefix(session.Title, "Fix auth") {
		t.Fatalf("title retained metadata: %q", session.Title)
	}
	if strings.Contains(session.Turns[0].UserPromptRedacted, "ghp_") {
		t.Fatal("prompt secret was not redacted")
	}
	if session.Turns[0].InputTokens != 100 || session.Turns[0].CachedInputTokens != 40 {
		t.Fatalf("token usage not parsed: %+v", session.Turns[0])
	}
	if len(session.ToolCalls) != 1 || session.ToolCalls[0].ToolCategory != "read" || session.ToolCalls[0].TargetPathDisplay != "src/auth.go" {
		t.Fatalf("tool not normalized: %+v", session.ToolCalls)
	}
	if session.Turns[0].SessionID != "s1" || session.ToolCalls[0].SessionID != "s1" || session.Events[0].SessionID != "s1" {
		t.Fatalf("child records lost session identity: %+v", session)
	}
	if len(session.Warnings) != 2 {
		t.Fatalf("warnings = %v", session.Warnings)
	}
}

func TestParserNormalizesOutputsCompactionAndUsageRecord(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s2","cwd":"/tmp/repo"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"root_turn_id":"t2","cwd":"/tmp/repo"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","call_id":"call","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call","output":"{\"exit_code\":1}"}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"post_compact"}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"token_usage_record","payload":{"turn_id":"t2","usage":{"input_tokens":"50","output_tokens":5}}}`,
		`{"timestamp":"2026-01-01T00:00:06Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"Done"}]}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "s2" || len(session.Events) != 2 || len(session.ToolCalls) != 1 || session.ToolCalls[0].Success || session.Turns[0].InputTokens != 50 || session.Turns[0].AssistantSummaryRedacted != "Done" {
		t.Fatalf("session=%+v", session)
	}
}

func TestParserTracksActiveEndModelChangesAndObservedUsage(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"settings","cwd":"/tmp/repo"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"one","model":"gpt-5.6-sol","effort":"high"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":84,"window_minutes":10080,"resets_at":1768000000}}}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"event_msg","payload":{"type":"task_complete"}}`,
		`{"timestamp":"2026-01-01T00:01:00Z","type":"turn_context","payload":{"turn_id":"two","model":"gpt-6-astra","effort":"low"}}`,
		`{"timestamp":"2026-01-01T00:01:01Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":94,"window_minutes":10080,"resets_at":1768000010}}}}`,
		`{"timestamp":"2026-01-01T00:01:10Z","type":"event_msg","payload":{"type":"task_complete"}}`,
		`{"timestamp":"2026-01-01T04:00:00Z","type":"event_msg","payload":{"type":"thread_settings_applied"}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.EndedAt.Format(time.RFC3339); got != "2026-01-01T00:01:10Z" {
		t.Fatalf("settings extended end to %s", got)
	}
	if session.Turns[1].Model != "gpt-6-astra" || session.Turns[1].ReasoningEffort != "low" {
		t.Fatalf("turn settings=%+v", session.Turns[1])
	}
	if session.Turns[1].RateLimitUsedStart == nil || *session.Turns[1].RateLimitUsedStart != 84 || session.Turns[1].RateLimitUsedEnd == nil || *session.Turns[1].RateLimitUsedEnd != 94 {
		t.Fatalf("usage=%+v", session.Turns[1])
	}
	metrics := domain.CalculateMetrics(session)
	if metrics.DurationMS != 14000 || metrics.ElapsedDurationMS != 70000 || metrics.UsageDeltaPercent != 10 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestParserRestartsUsageBaselineOnlyWhenMeterDecreases(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"reset","cwd":"/tmp/repo"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"one"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":90,"window_minutes":10080,"resets_at":1768000000}}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"task_complete"}}`,
		`{"timestamp":"2026-01-01T00:01:00Z","type":"turn_context","payload":{"turn_id":"two"}}`,
		`{"timestamp":"2026-01-01T00:01:01Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":2,"window_minutes":10080,"resets_at":1769000000}}}}`,
		`{"timestamp":"2026-01-01T00:01:02Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":4,"window_minutes":10080,"resets_at":1769000010}}}}`,
		`{"timestamp":"2026-01-01T00:01:03Z","type":"event_msg","payload":{"type":"task_complete"}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	turn := session.Turns[1]
	if turn.RateLimitUsedStart == nil || *turn.RateLimitUsedStart != 2 || turn.RateLimitUsedEnd == nil || *turn.RateLimitUsedEnd != 4 {
		t.Fatalf("reset usage=%+v", turn)
	}
}

func TestParserExpandsOrchestratedToolsAndMCPServers(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"nested","cwd":"/tmp/repo"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"root_turn_id":"turn"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call","name":"exec","input":"const [a,b] = await Promise.all([tools.exec_command({cmd: 'pwd'}), tools.mcp__cua_repl__js({code: 'ok'})]);"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call","output":"{\"isError\":true}"}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.ToolCalls) != 2 || len(session.Events) != 2 {
		t.Fatalf("expected two expanded tool calls: %+v", session.ToolCalls)
	}
	if session.ToolCalls[0].ToolName != "exec_command" || session.ToolCalls[0].ToolTransport != "builtin" {
		t.Fatalf("unexpected built-in tool: %+v", session.ToolCalls[0])
	}
	if session.ToolCalls[1].ToolName != "mcp__cua_repl__js" || session.ToolCalls[1].ToolTransport != "mcp" || session.ToolCalls[1].ToolServer != "cua_repl" {
		t.Fatalf("unexpected MCP tool: %+v", session.ToolCalls[1])
	}
	if session.ToolCalls[0].Success || session.ToolCalls[1].Success {
		t.Fatalf("wrapper result was not applied to expanded tools: %+v", session.ToolCalls)
	}
}

func TestParserPreservesInitialPromptAndClassifiesSameTurnFollowup(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"steering","cwd":"/tmp/repo","originator":"Codex Desktop","source":"vscode","thread_source":"user"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"turn"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Build the feature"}]}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Also push it when done"}]}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if session.Turns[0].UserPromptRedacted != "Build the feature" {
		t.Fatalf("initial prompt was overwritten: %+v", session.Turns[0])
	}
	if session.Originator != "Codex Desktop" || session.Surface != "vscode" || session.ThreadSource != "user" {
		t.Fatalf("session context missing: %+v", session)
	}
	if len(session.Events) != 2 || session.Events[1].EventSubtype != "steering_or_queued" || session.Events[1].PayloadRedacted["text"] != "Also push it when done" {
		t.Fatalf("follow-up event=%+v", session.Events)
	}
}

func TestParserHelpers(t *testing.T) {
	cases := map[string]string{"apply_patch": "edit", "read_file": "read", "rg auth": "search", "npm test": "validation", "web http": "network", "unknown": "execute"}
	for input, want := range cases {
		if got := classify(input, input); got != want {
			t.Errorf("classify(%q)=%q want %q", input, got, want)
		}
	}
	if got := extractTarget(`{"nested":{"file_path":"/tmp/src/a.go"}}`); got != "/tmp/src/a.go" {
		t.Fatalf("target=%q", got)
	}
	if got := extractTarget(`cat ./src/a.go`); got != "./src/a.go" {
		t.Fatalf("regex target=%q", got)
	}
	if commandSignature("run abcdef123456 12345") == "run abcdef123456 12345" {
		t.Fatal("signature not normalized")
	}
	if anyString(map[string]any{"x": 1}) == "" {
		t.Fatal("map not encoded")
	}
	if value, ok := numeric(json.Number("9")); !ok || value != 9 {
		t.Fatalf("numeric=%d %v", value, ok)
	}
	if _, ok := numeric(true); ok {
		t.Fatal("bool must not be numeric")
	}
	if titleFrom(strings.Repeat("a", 100)) == strings.Repeat("a", 100) {
		t.Fatal("title not bounded")
	}
	if fallbackID(time.Unix(1, 0), 2) == "" {
		t.Fatal("fallback empty")
	}
	if got := cleanUserContent("# Chrome tabs:\n- injected\n\n## My request:\ntest the page"); got != "test the page" {
		t.Fatalf("request wrapper not removed: %q", got)
	}
	if got := cleanUserContent("## My request:\ndelete these\n<image>binary placeholder</image>"); got != "delete these" {
		t.Fatalf("image wrapper not removed: %q", got)
	}
	if got := nestedToolNames("no nested calls"); len(got) != 0 {
		t.Fatalf("unexpected nested tools: %v", got)
	}
}

func TestCompactedRecordIsAFirstClassEvent(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s1"}}`,
		`{"timestamp":"2026-01-01T00:01:00Z","type":"compacted","payload":{"message":"","replacement_history":[]}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Events) != 1 || session.Events[0].EventType != "compaction" {
		t.Fatalf("events=%+v", session.Events)
	}
	for _, warning := range session.Warnings {
		if strings.Contains(warning, "compacted") {
			t.Fatalf("compaction should not be unsupported: %q", warning)
		}
	}
}

func TestWorldStateRecordsOnlySafeShapeMetadata(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s1"}}`,
		`{"timestamp":"2026-01-01T00:01:00Z","type":"world_state","payload":{"full":true,"state":{"model":{"secret":"not retained"},"permissions":{"value":"private"}}}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Events) != 1 || session.Events[0].EventType != "context" {
		t.Fatalf("events=%+v", session.Events)
	}
	encoded, _ := json.Marshal(session.Events[0].PayloadRedacted)
	if strings.Contains(string(encoded), "not retained") || strings.Contains(string(encoded), "private") {
		t.Fatalf("world state content leaked: %s", encoded)
	}
	if len(session.Warnings) != 0 {
		t.Fatalf("warnings=%v", session.Warnings)
	}
}

func TestToolResultErrorAndFindTurnFallback(t *testing.T) {
	call := domain.ToolCall{Success: true}
	updateToolResult(&call, `{"isError":true}`)
	if call.Success {
		t.Fatal("isError should fail")
	}
	session := domain.Session{Turns: []domain.Turn{{ExternalTurnID: "one"}}}
	if findTurn(&session, "missing") == nil {
		t.Fatal("expected latest turn fallback")
	}
	if findTurn(&domain.Session{}, "missing") != nil {
		t.Fatal("empty session should have no turn")
	}
}

func TestParseFileHashesInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	data := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"file-session"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	session, fingerprint, err := New().ParseFile(path, true)
	if err != nil || session.ID != "file-session" || len(fingerprint) != 64 || !session.Archived {
		t.Fatalf("session=%+v hash=%q err=%v", session, fingerprint, err)
	}
	if _, _, err := New().ParseFile(filepath.Join(t.TempDir(), "missing"), false); err == nil {
		t.Fatal("expected open error")
	}
}

func TestTranscriptIDWinsOverParentSessionID(t *testing.T) {
	input := `{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"child","session_id":"parent"}}`
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil || session.ID != "child" {
		t.Fatalf("session=%+v err=%v", session, err)
	}
}

func TestParserClassifiesGuardianAutoReview(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"review","source":{"subagent":{"other":"guardian"}}}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"t1","model":"codex-auto-review"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"The following is the Codex agent history whose request action you are assessing"}]}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if session.Kind != domain.InternalReviewSession || session.Title != "Auto-review approval assessment" {
		t.Fatalf("session=%+v", session)
	}
}

func TestParseSurvivesEmptyTranscript(t *testing.T) {
	session, err := New().Parse(strings.NewReader(""), true)
	if err != nil {
		t.Fatal(err)
	}
	if session.Title != "Untitled Codex session" || !session.Archived {
		t.Fatalf("unexpected empty session: %+v", session)
	}
}
