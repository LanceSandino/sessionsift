package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// All fixtures are synthetic; no real Claude Code transcript is read.

type fields = map[string]any

const testSession = "11111111-2222-3333-4444-555555555555"

var testStart = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

func at(seconds float64) string {
	return testStart.Add(time.Duration(seconds * float64(time.Second))).Format(time.RFC3339Nano)
}

// jsonl marshals records, adding the context fields Claude Code repeats on
// every conversation record. A nil value removes a default.
func jsonl(t *testing.T, records ...fields) string {
	t.Helper()
	lines := make([]string, 0, len(records))
	for index, item := range records {
		merged := fields{}
		switch item["type"] {
		case "user", "assistant", "system":
			merged = fields{"sessionId": testSession, "uuid": fmt.Sprintf("uuid-%d", index+1), "cwd": "/work/repo", "version": "2.1.0", "entrypoint": "cli", "gitBranch": "main", "isSidechain": false}
		}
		for key, value := range item {
			if value == nil {
				delete(merged, key)
			} else {
				merged[key] = value
			}
		}
		data, err := json.Marshal(merged)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
	}
	return strings.Join(lines, "\n")
}

func parseRecords(t *testing.T, subagent bool, records ...fields) domain.Session {
	t.Helper()
	session, err := New().Parse(strings.NewReader(jsonl(t, records...)), subagent)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func userRecord(seconds float64, promptID string, content any) fields {
	return fields{"type": "user", "timestamp": at(seconds), "promptId": promptID, "message": fields{"role": "user", "content": content}}
}

func assistantRecord(seconds float64, messageID, model string, block fields, usage fields) fields {
	msg := fields{"id": messageID, "model": model, "role": "assistant", "type": "message", "content": []any{block}}
	if usage != nil {
		msg["usage"] = usage
	}
	return fields{"type": "assistant", "timestamp": at(seconds), "message": msg}
}

func textBlock(text string) fields { return fields{"type": "text", "text": text} }

func toolUseBlock(id, name string, input fields) fields {
	return fields{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResultBlock(id string, content any, isError bool) fields {
	return fields{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isError}
}

func with(base fields, extra fields) fields {
	merged := fields{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func marshal(t *testing.T, session domain.Session) string {
	t.Helper()
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
