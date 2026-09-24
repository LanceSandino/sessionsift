package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHintsFromPath(t *testing.T) {
	const session = "0f5c2a8e-1b2c-4d5e-8f90-123456789abc"
	cases := map[string]identityHints{
		filepath.Join("projects", "p", session+".jsonl"):                                            {sessionID: session},
		filepath.Join("projects", "p", session, "subagents", "agent-a1.jsonl"):                      {sessionID: session, agentID: "a1"},
		filepath.Join("projects", "p", session, "subagents", "workflows", "wf_1", "agent-b2.jsonl"): {sessionID: session, agentID: "b2"},
		filepath.Join("projects", "p", "notuuid", "subagents", "agent-c3.jsonl"):                    {agentID: "c3"},
		filepath.Join("projects", "p", "agent-legacy.jsonl"):                                        {agentID: "legacy"},
		filepath.Join("projects", "p", "agent-.jsonl"):                                              {},
		filepath.Join("projects", "p", "renamed.jsonl"):                                             {},
	}
	for path, want := range cases {
		if got := hintsFromPath(path); got != want {
			t.Errorf("hintsFromPath(%q)=%+v want %+v", path, got, want)
		}
	}
}

// Truncated subagent files whose only line is malformed state no ids; the
// path must keep them distinct instead of colliding on one fallback.
func TestTruncatedSubagentFilesKeepDistinctIdentities(t *testing.T) {
	const session = "0f5c2a8e-1b2c-4d5e-8f90-123456789abc"
	dir := filepath.Join(t.TempDir(), "projects", "p", session, "subagents", "workflows", "wf_1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, agent := range []string{"a0", "a3"} {
		path := filepath.Join(dir, "agent-"+agent+".jsonl")
		if err := os.WriteFile(path, []byte(`{"parentUuid":"x","isSide`), 0o600); err != nil {
			t.Fatal(err)
		}
		parsed, _, err := New().ParseFile(path, true)
		if err != nil {
			t.Fatal(err)
		}
		want := "claude:" + session + ":agent:" + agent
		if parsed.ID != want || len(parsed.Warnings) != 1 || parsed.ThreadSource != "subagent" {
			t.Fatalf("session=%+v want id %s", parsed, want)
		}
		ids[parsed.ID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("identities collided: %v", ids)
	}
}

// A file whose records all carry another session's id (a continued
// conversation) keeps its own file identity so it cannot overwrite that
// session; a file with no path id falls back to the record id.
func TestPathIdentityBeatsRecordsAndDigestSeparatesUnnamedInputs(t *testing.T) {
	const own = "9f5c2a8e-1b2c-4d5e-8f90-123456789abc"
	dir := t.TempDir()
	data := []byte(jsonl(t, userRecord(1, "p1", "Hello")))
	for name, want := range map[string]string{own + ".jsonl": "claude:" + own, "renamed-file.jsonl": "claude:" + testSession} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		parsed, _, err := New().ParseFile(path, false)
		if err != nil || parsed.ID != want || parsed.Turns[0].ID != want+":p1" {
			t.Fatalf("%s: session=%+v err=%v", name, parsed, err)
		}
	}
	first, _ := New().Parse(strings.NewReader(`{"type":"mode","mode":"a"}`), false)
	second, _ := New().Parse(strings.NewReader(`{"type":"mode","mode":"b"}`), false)
	if first.ID == second.ID || !strings.HasPrefix(first.ID, "claude:session-") {
		t.Fatalf("unnamed inputs share an id: %q %q", first.ID, second.ID)
	}
}
