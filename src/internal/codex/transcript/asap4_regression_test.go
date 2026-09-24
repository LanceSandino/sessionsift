package transcript

import (
	"strings"
	"testing"
)

// ASAP4-F3: titles and command signatures are shared with every remote viewer,
// so they must be derived from redacted text rather than raw prompts/arguments.
func TestTitleAndCommandSignatureNeverRetainSecrets(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-09-19T10:00:00Z","type":"session_meta","payload":{"id":"secret-title","cwd":"/work/app"}}`,
		`{"timestamp":"2026-09-19T10:00:01Z","type":"turn_context","payload":{"turn_id":"t1","model":"gpt-test"}}`,
		`{"timestamp":"2026-09-19T10:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Deploy with ghp_abcdefghijklmnopqrstuvwxyz today"}]}}`,
		`{"timestamp":"2026-09-19T10:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"curl -H 'Authorization: Bearer supersecretbearervalue' https://api.example && go test ./...\"]}"}}`,
	}, "\n")
	session, err := New().Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(session.Title, "ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("title retained a credential: %q", session.Title)
	}
	if len(session.ToolCalls) != 1 {
		t.Fatalf("tool calls=%+v", session.ToolCalls)
	}
	if signature := session.ToolCalls[0].CommandSignature; signature == "" || strings.Contains(signature, "supersecretbearervalue") {
		t.Fatalf("command signature=%q", signature)
	}
}
