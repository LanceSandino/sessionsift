package hooks

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRedactsContentAndDropsPaths(t *testing.T) {
	payload := map[string]any{
		"session_id": "abc", "turn_id": "t1", "hook_event_name": "PostToolUse", "tool_name": "Bash",
		"stop_hook_active": true, "model": "claude-opus-5-5", "count": json.Number("3"),
		"tool_input":             map[string]any{"command": "curl -H 'Authorization: Bearer " + "abcdefghijklmnopq' x"},
		"tool_response":          strings.Repeat("x", 5000),
		"last_assistant_message": "api_key=" + "secretvalue123",
		"cwd":                    "/Users/alice/app", "transcript_path": "/Users/alice/.claude/x.jsonl", "agent_transcript_path": "/p",
		"output_dir": "/tmp/out", "working_directory": "/tmp/work",
	}
	event, err := Normalize(ClaudeCode, payload, time.Unix(100, 0), "identity", false)
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "claude:abc" || event.Source != "claude_hook" || event.EventSubtype != "PostToolUse" || event.TurnID != "t1" {
		t.Fatalf("event=%+v", event)
	}
	encoded, _ := json.Marshal(event.PayloadRedacted)
	text := string(encoded)
	for _, forbidden := range []string{"abcdefghijklmnopq", "secretvalue123", "alice", "transcript_path", "cwd", "output_dir", "working_directory", strings.Repeat("x", 2100)} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("payload retained %q: %s", forbidden, text)
		}
	}
	if event.PayloadRedacted["stop_hook_active"] != true || event.PayloadRedacted["tool_name"] != "Bash" || event.PayloadRedacted["session_id"] != "claude:abc" {
		t.Fatalf("metadata changed: %s", text)
	}
	if _, ok := event.PayloadRedacted["tool_input"].(string); !ok {
		t.Fatalf("content objects must be flattened to redacted text: %s", text)
	}
	again, _ := Normalize(ClaudeCode, payload, time.Unix(100, 0), "identity", false)
	if again.ID != event.ID {
		t.Fatal("event identity is not deterministic")
	}
}

func TestNormalizeValidatesAndBoundsIdentifiers(t *testing.T) {
	if _, err := Normalize(Codex, map[string]any{"hook_event_name": "Stop"}, time.Now(), "x", false); err == nil {
		t.Fatal("missing session accepted")
	}
	if _, err := Normalize(Codex, map[string]any{"session_id": strings.Repeat("s", 300)}, time.Now(), "x", false); err == nil {
		t.Fatal("oversized session accepted")
	}
	event, err := Normalize(Codex, map[string]any{"session_id": "s", "hook_event_name": "<script>" + strings.Repeat("A", 100), "model": map[string]any{"nested": "value"}}, time.Now(), "x", true)
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "s" || event.Source != "hook_spool" || strings.ContainsAny(event.EventSubtype, "<>") || len(event.EventSubtype) > 64 {
		t.Fatalf("event=%+v", event)
	}
	if _, ok := event.PayloadRedacted["model"].(string); !ok {
		t.Fatalf("non-scalar metadata must become bounded text: %+v", event.PayloadRedacted)
	}
	defaulted, _ := Normalize(Codex, map[string]any{"session_id": "s"}, time.Now(), "y", false)
	if defaulted.EventSubtype != "hook" || defaulted.Source != "hook" {
		t.Fatalf("defaulted=%+v", defaulted)
	}
	claudeSpool, _ := Normalize(ClaudeCode, map[string]any{"session_id": "s", "nothing": nil}, time.Now(), "z", true)
	if claudeSpool.Source != "claude_hook_spool" || claudeSpool.PayloadRedacted["nothing"] != "" {
		t.Fatalf("claude spool=%+v", claudeSpool)
	}
}

func TestParseProvider(t *testing.T) {
	for input, want := range map[string]Provider{"": Codex, "codex": Codex, "Claude": ClaudeCode, "claude_code": ClaudeCode, "claude-code": ClaudeCode} {
		if got, ok := ParseProvider(input); !ok || got != want {
			t.Fatalf("ParseProvider(%q)=%v,%v", input, got, ok)
		}
	}
	if _, ok := ParseProvider("gemini"); ok {
		t.Fatal("unknown provider accepted")
	}
}
