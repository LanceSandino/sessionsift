package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

func TestParseMapsSessionTurnsToolsAndUsage(t *testing.T) {
	usageFirst := fields{"input_tokens": 10, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 1000, "output_tokens": 5, "output_tokens_details": fields{"thinking_tokens": 3}}
	usageLast := fields{"input_tokens": 10, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 1000, "output_tokens": 50, "output_tokens_details": fields{"thinking_tokens": 40}}
	session := parseRecords(t, false,
		fields{"type": "queue-operation", "operation": "enqueue", "timestamp": at(0), "sessionId": testSession, "content": "QUEUED_PRIVATE"},
		with(userRecord(1, "p1", "Fix the login bug"), fields{"permissionMode": "acceptEdits"}),
		with(assistantRecord(2, "msg_1", "claude-opus-5", fields{"type": "thinking", "thinking": "PRIVATE_THOUGHT", "signature": "SIG_PRIVATE"}, usageFirst), fields{"effort": "high"}),
		with(assistantRecord(3, "msg_1", "claude-opus-5", toolUseBlock("toolu_1", "Read", fields{"file_path": "/work/repo/src/login.go"}), usageLast), fields{"effort": "high"}),
		with(userRecord(4, "p1", []any{toolResultBlock("toolu_1", "package login", false)}), fields{"toolUseResult": fields{"type": "text", "file": fields{"content": "FILE_BODY"}}}),
		assistantRecord(5, "msg_2", "claude-opus-5", textBlock("Fixed the bug."), fields{"input_tokens": 1, "cache_creation_input_tokens": 2, "cache_read_input_tokens": 3, "output_tokens": 4}),
		with(userRecord(60, "p2", "Now run the tests"), fields{"permissionMode": "plan"}),
		with(assistantRecord(61, "msg_3", "claude-sonnet-5", toolUseBlock("toolu_2", "Bash", fields{"command": "cd /work/repo && go test ./..."}), fields{"input_tokens": 7, "cache_read_input_tokens": 70, "output_tokens": 8}), fields{"effort": "low"}),
		userRecord(63, "p2", []any{toolResultBlock("toolu_2", []any{textBlock("FAIL"), fields{"type": "image", "source": fields{"data": "IMAGE_DATA"}}}, true)}),
		assistantRecord(64, "msg_4", "claude-sonnet-5", textBlock("Tests fail."), nil),
		with(assistantRecord(65, "msg_5", syntheticModel, textBlock("API Error: PRIVATE_ERROR"), fields{"input_tokens": 999, "output_tokens": 999}), fields{"isApiErrorMessage": true}),
	)
	wantID := "claude:" + testSession
	if session.ID != wantID || session.ExternalSessionID != testSession || session.Source != domain.SourceClaudeCode || session.Kind != domain.DeveloperSession {
		t.Fatalf("identity=%+v", session)
	}
	if session.ParserVersion != domain.ClaudeCodeParserVersion || session.Status != "completed" || session.SourceVersion != "2.1.0" || session.Surface != "cli" || session.Originator != "claude-code" || session.RuntimeLocation != "local" {
		t.Fatalf("provenance=%+v", session)
	}
	if session.GitBranch != "main" || session.ApprovalMode != "plan" || session.Model != "claude-sonnet-5" || session.ReasoningEffort != "low" || session.ThreadSource != "" {
		t.Fatalf("context=%+v", session)
	}
	if !session.StartedAt.Equal(testStart) || session.EndedAt.Format("15:04:05") != "10:01:05" {
		t.Fatalf("bounds=%s..%s", session.StartedAt, session.EndedAt)
	}
	if session.Title != "Fix the login bug" || session.TitleSource != "first_prompt" {
		t.Fatalf("title=%q source=%q", session.Title, session.TitleSource)
	}
	if len(session.Turns) != 2 {
		t.Fatalf("turns=%+v", session.Turns)
	}
	one, two := session.Turns[0], session.Turns[1]
	if one.ID != wantID+":p1" || one.ExternalTurnID != "p1" || one.Sequence != 1 || one.SessionID != wantID || one.UserPromptRedacted != "Fix the login bug" {
		t.Fatalf("turn one=%+v", one)
	}
	if one.InputTokens != 1116 || one.CachedInputTokens != 1003 || one.OutputTokens != 54 || one.ReasoningTokens != 40 || one.TokenSource != "transcript" {
		t.Fatalf("turn one usage=%+v", one)
	}
	if one.Model != "claude-opus-5" || one.ReasoningEffort != "high" || one.AssistantSummaryRedacted != "Fixed the bug." || one.EndedAt.Format("15:04:05") != "10:00:05" {
		t.Fatalf("turn one output=%+v", one)
	}
	if two.InputTokens != 77 || two.CachedInputTokens != 70 || two.OutputTokens != 8 || two.Model != "claude-sonnet-5" || two.AssistantSummaryRedacted != "Tests fail." {
		t.Fatalf("turn two=%+v", two)
	}
	if len(session.ToolCalls) != 2 {
		t.Fatalf("tools=%+v", session.ToolCalls)
	}
	read, test := session.ToolCalls[0], session.ToolCalls[1]
	if read.ID != wantID+":toolu_1" || read.TurnID != one.ID || read.SessionID != wantID || read.ToolCategory != "read" || read.ToolTransport != "builtin" || !read.Success || read.DurationMS != 1000 {
		t.Fatalf("read=%+v", read)
	}
	if read.TargetPathDisplay != "src/login.go" || read.TargetPathHash != redact.PathHash("/work/repo/src/login.go") || read.CommandSignature != "" || read.ContentFingerprint != redact.Hash("package login") {
		t.Fatalf("read target=%+v", read)
	}
	if test.ToolCategory != "validation" || test.Success || test.DurationMS != 2000 || test.ResultPreviewRedacted != "FAIL" || test.TurnID != two.ID || test.CommandSignature != "cd /work/repo && go test ./..." {
		t.Fatalf("test=%+v", test)
	}
	wantEvents := []struct{ id, kind, subtype, turn string }{
		{"evt-000002", "prompt", "", one.ID},
		{"evt-000004", "tool", "read", one.ID},
		{"evt-000007", "prompt", "", two.ID},
		{"evt-000008", "tool", "validation", two.ID},
	}
	if len(session.Events) != len(wantEvents) {
		t.Fatalf("events=%+v", session.Events)
	}
	for index, want := range wantEvents {
		got := session.Events[index]
		if got.ID != want.id || got.EventType != want.kind || got.EventSubtype != want.subtype || got.TurnID != want.turn || got.SessionID != wantID || got.Source != "transcript" {
			t.Fatalf("event %d=%+v want %+v", index, got, want)
		}
	}
	if len(session.Warnings) != 1 || session.Warnings[0] != "skipped 1 unsupported queue-operation records" {
		t.Fatalf("warnings=%v", session.Warnings)
	}
	encoded := marshal(t, session)
	for _, private := range []string{"QUEUED_PRIVATE", "PRIVATE_THOUGHT", "SIG_PRIVATE", "FILE_BODY", "IMAGE_DATA", "PRIVATE_ERROR", "thinking"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("session JSON retained %q: %s", private, encoded)
		}
	}
	metrics := domain.CalculateMetrics(session)
	if metrics.Reads != 1 || metrics.Validations != 1 || metrics.FailedToolCalls != 1 || metrics.InputTokens != 1193 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestTitlePrecedence(t *testing.T) {
	prompt := userRecord(1, "p1", "Refactor the billing module")
	cases := []struct {
		name          string
		records       []fields
		title, source string
	}{
		{"rename beats generated", []fields{
			{"type": "ai-title", "aiTitle": "Generated name", "sessionId": testSession}, prompt,
			{"type": "custom-title", "customTitle": "First rename", "sessionId": testSession},
			{"type": "custom-title", "customTitle": "Final   rename\n", "sessionId": testSession},
			{"type": "summary", "summary": "Old summary", "leafUuid": "x"},
		}, "Final rename", "renamed"},
		{"generated beats prompt", []fields{prompt, {"type": "ai-title", "aiTitle": "Billing cleanup"}}, "Billing cleanup", "generated"},
		{"legacy summary", []fields{{"type": "summary", "summary": "Legacy summary", "leafUuid": "x"}, prompt}, "Legacy summary", "generated"},
		{"first prompt", []fields{prompt}, "Refactor the billing module", "first_prompt"},
		{"blank titles ignored", []fields{{"type": "custom-title", "customTitle": "   "}, {"type": "ai-title", "aiTitle": ""}, {"type": "summary", "summary": " "}}, "Untitled Claude Code session", "fallback"},
	}
	for _, tc := range cases {
		session := parseRecords(t, false, tc.records...)
		if session.Title != tc.title || session.TitleSource != tc.source {
			t.Errorf("%s: title=%q source=%q", tc.name, session.Title, session.TitleSource)
		}
	}
	long := parseRecords(t, false, userRecord(1, "p1", strings.Repeat("word ", 40)))
	if len(long.Title) > 80 || !strings.HasSuffix(long.Title, "…") {
		t.Fatalf("title not bounded: %q", long.Title)
	}
}

func TestSecretsAreRedactedInPromptTitleArgumentsAndSignature(t *testing.T) {
	const secret = "ghp_abcdefghijklmnopqrstuvwxyz0123"
	records := []fields{
		userRecord(1, "p1", "Deploy with token "+secret+" please"),
		assistantRecord(2, "m1", "claude-opus-5", toolUseBlock("t1", "Bash", fields{"command": "curl -H 'X-Key: " + secret + "' https://example.test/items/123456"}), nil),
		userRecord(3, "p1", []any{toolResultBlock("t1", "echo "+secret, false)}),
		userRecord(4, "p1", "Also rotate "+secret),
	}
	session := parseRecords(t, false, records...)
	if !strings.Contains(session.Title, "[REDACTED]") || !strings.Contains(session.Turns[0].UserPromptRedacted, "[REDACTED]") {
		t.Fatalf("prompt/title not redacted: %+v", session)
	}
	call := session.ToolCalls[0]
	if call.ToolCategory != "execute" || !strings.Contains(call.CommandSignature, "[redacted]") || !strings.Contains(call.CommandSignature, "<n>") {
		t.Fatalf("signature=%q", call.CommandSignature)
	}
	renamed := parseRecords(t, false, append(records, fields{"type": "custom-title", "customTitle": "Rotate " + secret})...)
	if renamed.Title != "Rotate [REDACTED]" {
		t.Fatalf("custom title=%q", renamed.Title)
	}
	for _, value := range []domain.Session{session, renamed} {
		if encoded := marshal(t, value); strings.Contains(encoded, secret) || strings.Contains(encoded, "ghp_") {
			t.Fatalf("secret leaked: %s", encoded)
		}
	}
}

func TestSubagentIdentity(t *testing.T) {
	sidechain := fields{"isSidechain": true, "agentId": "a1b2"}
	wantID := "claude:" + testSession + ":agent:a1b2"
	flagged := parseRecords(t, true,
		with(userRecord(1, "p1", "Investigate the flaky test"), sidechain),
		with(assistantRecord(2, "m1", "claude-haiku-4-5", textBlock("Found it"), nil), sidechain),
	)
	if flagged.ID != wantID || flagged.ExternalSessionID != testSession+":agent:a1b2" || flagged.ThreadSource != "subagent" || flagged.Turns[0].ID != wantID+":p1" || flagged.Title != "Investigate the flaky test" {
		t.Fatalf("flagged=%+v", flagged)
	}
	detected := parseRecords(t, false, with(userRecord(1, "p1", "Investigate"), sidechain))
	if detected.ID != wantID || detected.ThreadSource != "subagent" {
		t.Fatalf("detected=%+v", detected)
	}
	anonymous := parseRecords(t, true, assistantRecord(1, "m1", "claude-haiku-4-5", textBlock("Done"), nil))
	if !strings.HasPrefix(anonymous.ID, "claude:"+testSession+":agent:agent-") || anonymous.Title != "Claude Code subagent" || anonymous.TitleSource != "fallback" {
		t.Fatalf("anonymous=%+v", anonymous)
	}
	mixed := parseRecords(t, false,
		userRecord(1, "p1", "Main prompt"),
		with(userRecord(2, "p1", "Sidechain prompt PRIVATE_SIDECHAIN"), sidechain),
		with(assistantRecord(3, "m9", "claude-haiku-4-5", textBlock("Sidechain answer"), fields{"input_tokens": 50}), sidechain),
	)
	if mixed.ID != "claude:"+testSession || len(mixed.Events) != 1 || mixed.Turns[0].InputTokens != 0 || mixed.Model != "" {
		t.Fatalf("mixed=%+v", mixed)
	}
	if len(mixed.Warnings) != 1 || mixed.Warnings[0] != "skipped 2 sidechain records in a main transcript" || strings.Contains(marshal(t, mixed), "PRIVATE_SIDECHAIN") {
		t.Fatalf("mixed warnings=%v", mixed.Warnings)
	}
}

func TestMalformedLinesAndFieldTypeDrift(t *testing.T) {
	input := strings.Join([]string{
		`{broken`,
		``,
		`[1,2]`,
		jsonl(t, with(userRecord(1, "p1", "Hello"), fields{"isMeta": "not-a-bool", "timestamp": "not-a-time"})),
		jsonl(t, fields{"type": "user", "message": fields{"content": 42}}),
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Warnings) != 2 || session.Warnings[0] != "line 1: malformed JSON" || session.Warnings[1] != "line 3: malformed JSON" {
		t.Fatalf("warnings=%v", session.Warnings)
	}
	if len(session.Turns) != 1 || session.Turns[0].UserPromptRedacted != "Hello" || !session.Turns[0].StartedAt.IsZero() {
		t.Fatalf("drifted record not parsed: %+v", session.Turns)
	}
}

func TestUnsupportedRecordsWarnWithoutContent(t *testing.T) {
	session := parseRecords(t, false,
		fields{"type": "attachment", "timestamp": at(1), "sessionId": testSession, "attachment": fields{"type": "file", "content": "ATTACHMENT_SECRET"}},
		fields{"type": "attachment", "timestamp": at(2), "sessionId": testSession, "attachment": fields{"type": "hook_additional_context", "content": "HOOK_CONTEXT"}},
		fields{"type": "file-history-snapshot", "snapshot": fields{"x": "SNAPSHOT_SECRET"}},
		fields{"type": "future-record", "payload": "FUTURE_SECRET"},
		fields{"type": "system", "subtype": "stop_hook_summary", "timestamp": at(3), "hookInfos": []any{"HOOK_SECRET"}},
		fields{"sessionId": testSession},
	)
	want := []string{
		"skipped 2 unsupported attachment records",
		"skipped 1 unsupported file-history-snapshot records",
		"skipped 1 unsupported future-record records",
		"skipped 1 unsupported system/stop_hook_summary records",
		"skipped 1 unsupported untyped records",
	}
	if strings.Join(session.Warnings, "|") != strings.Join(want, "|") {
		t.Fatalf("warnings=%v", session.Warnings)
	}
	if len(session.Events) != 0 || len(session.Turns) != 0 {
		t.Fatalf("unsupported records produced records: %+v", session)
	}
	encoded := marshal(t, session)
	for _, private := range []string{"ATTACHMENT_SECRET", "HOOK_CONTEXT", "SNAPSHOT_SECRET", "FUTURE_SECRET", "HOOK_SECRET"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("retained %q", private)
		}
	}
	if session.ID != "claude:"+testSession || !session.EndedAt.Equal(testStart.Add(3e9)) {
		t.Fatalf("session=%+v", session)
	}
}

func TestWindowsCWDIsHashedAndNeverSerialized(t *testing.T) {
	cwd := `C:\Users\someone\proj`
	session := parseRecords(t, false, with(userRecord(1, "p1", "Hi"), fields{"cwd": cwd}), with(userRecord(2, "p2", "Later"), fields{"cwd": `D:\elsewhere`}))
	if session.CWD != cwd || session.RepoDisplayName != "proj" || session.RepoRootHash != redact.PathHash(filepath.Clean(cwd)) {
		t.Fatalf("cwd=%q name=%q hash=%q", session.CWD, session.RepoDisplayName, session.RepoRootHash)
	}
	encoded := marshal(t, session)
	for _, private := range []string{cwd, strings.ReplaceAll(cwd, `\`, `\\`), "someone"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("cwd leaked as %q: %s", private, encoded)
		}
	}
	home := parseRecords(t, false, with(userRecord(1, "p1", "Hi"), fields{"cwd": `C:\Users\someone`}))
	if home.RepoDisplayName == "someone" || home.RepoDisplayName == "" {
		t.Fatalf("home directory display=%q", home.RepoDisplayName)
	}
	root := parseRecords(t, false, with(userRecord(1, "p1", "Hi"), fields{"cwd": "/"}))
	if root.RepoDisplayName != "" || root.RepoRootHash != "" {
		t.Fatalf("root cwd=%+v", root)
	}
}

func TestParseFileHashesInputAndReportsErrors(t *testing.T) {
	data := jsonl(t, userRecord(1, "p1", "From disk"))
	path := filepath.Join(t.TempDir(), testSession+".jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	session, fingerprint, err := New().ParseFile(path, false)
	sum := sha256.Sum256([]byte(data))
	if err != nil || session.ID != "claude:"+testSession || fingerprint != hex.EncodeToString(sum[:]) {
		t.Fatalf("session=%+v hash=%q err=%v", session, fingerprint, err)
	}
	if _, _, err := New().ParseFile(filepath.Join(t.TempDir(), "missing.jsonl"), false); err == nil {
		t.Fatal("expected open error")
	}
	oversized := filepath.Join(t.TempDir(), "huge.jsonl")
	if err := os.WriteFile(oversized, []byte(`{"type":"user","x":"`+strings.Repeat("a", 17*1024*1024)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New().ParseFile(oversized, false); err == nil {
		t.Fatal("expected a scanner error for an oversized line")
	}
}

func TestEmptyTranscript(t *testing.T) {
	session, err := New().Parse(strings.NewReader(""), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.ID, "claude:session-") || session.Title != "Untitled Claude Code session" || !session.EndedAt.IsZero() || len(session.Warnings) != 0 {
		t.Fatalf("session=%+v", session)
	}
}
