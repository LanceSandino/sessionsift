package transcript

import (
	"strings"
	"testing"
	"time"
)

// Transcript numbers are untrusted 64-bit values. Fields stored as int keep
// only values that fit in 32 bits, so a build for any platform reads them
// the same way instead of silently wrapping.
func TestOutOfRangeTranscriptIntegersAreIgnored(t *testing.T) {
	session, err := New().Parse(strings.NewReader(strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s1","cwd":"/workspace"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":"t1"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":10,"window_minutes":"4294967296300","resets_at":1768000000}}}}`,
	}, "\n")), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Turns) == 0 {
		t.Fatal("expected one turn")
	}
	turn := session.Turns[len(session.Turns)-1]
	if turn.RateLimitUsedEnd == nil || *turn.RateLimitUsedEnd != 10 {
		t.Fatalf("usage snapshot dropped: %+v", turn.RateLimitUsedEnd)
	}
	if turn.RateLimitWindowMinutes != 0 {
		t.Fatalf("out-of-range window kept as %d", turn.RateLimitWindowMinutes)
	}

	for _, tc := range []struct {
		output  string
		exit    *int
		success bool
	}{
		{`{"exit_code":"4294967296"}`, nil, true},
		{`{"exit_code":-2147483649}`, nil, true},
		{`{"exit_code":-1073741819}`, intPointer(-1073741819), false},
		{`{"exit_code":0}`, intPointer(0), true},
	} {
		call := newToolCall("c1", "s1", "t1", time.Now(), "shell", `{}`)
		call.Success = true
		updateToolResult(&call, tc.output)
		if (call.ExitCode == nil) != (tc.exit == nil) || (call.ExitCode != nil && *call.ExitCode != *tc.exit) || call.Success != tc.success {
			t.Errorf("%s: exit=%v success=%v", tc.output, call.ExitCode, call.Success)
		}
	}
}

func intPointer(value int) *int { return &value }
