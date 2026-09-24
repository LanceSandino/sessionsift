package ingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

// The workspace scrub runs on real imports for both developer-agent providers.
func TestImportersNeverPersistTheAbsoluteWorkspace(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	codex := filepath.Join(root, "codex", "sessions", "one.jsonl")
	claude := filepath.Join(root, "claude", "projects", "p", "0d7c9f8e-1234-4a5b-8c9d-0123456789ab.jsonl")
	for _, path := range []string{codex, claude} {
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	codexBody := strings.Join([]string{
		`{"timestamp":"2026-09-23T09:00:00Z","type":"session_meta","payload":{"id":"codex-ws","cwd":"/srv/clients/acme/payments"}}`,
		`{"timestamp":"2026-09-23T09:00:01Z","type":"turn_context","payload":{"turn_id":"t1","model":"gpt"}}`,
		`{"timestamp":"2026-09-23T09:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Open /srv/clients/acme/payments/api.go"}]}}`,
		`{"timestamp":"2026-09-23T09:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"cat /srv/clients/acme/payments/api.go\"]}"}}`,
	}, "\n")
	claudeRecords := []map[string]any{
		{"type": "user", "uuid": "u1", "sessionId": "0d7c9f8e-1234-4a5b-8c9d-0123456789ab", "timestamp": "2026-09-23T10:00:00Z", "cwd": `D:\clients\acme\checkout`, "promptId": "p1", "message": map[string]any{"role": "user", "content": `Fix D:\clients\acme\checkout\cart.go`}},
		{"type": "assistant", "uuid": "a1", "sessionId": "0d7c9f8e-1234-4a5b-8c9d-0123456789ab", "timestamp": "2026-09-23T10:00:05Z", "cwd": `D:\clients\acme\checkout`, "message": map[string]any{"id": "m1", "role": "assistant", "model": "claude-opus-5-5", "content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"file_path": `D:\clients\acme\checkout\cart.go`}}}}},
	}
	var claudeBody strings.Builder
	for _, record := range claudeRecords {
		line, _ := json.Marshal(record)
		claudeBody.Write(line)
		claudeBody.WriteByte('\n')
	}
	if err = os.WriteFile(codex, []byte(codexBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(claude, []byte(claudeBody.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = ImportAll(context.Background(), store, filepath.Join(root, "codex")); err != nil {
		t.Fatal(err)
	}
	if _, err = ImportClaude(context.Background(), store, filepath.Join(root, "claude")); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(context.Background())
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions=%d err=%v", len(sessions), err)
	}
	for _, session := range sessions {
		encoded, _ := json.Marshal(session)
		if text := strings.ToLower(string(encoded)); strings.Contains(text, "clients") || strings.Contains(text, "acme") {
			t.Fatalf("%s persisted the absolute workspace: %s", session.ID, encoded)
		}
		if session.RepoDisplayName == "" || !strings.Contains(string(encoded), session.RepoDisplayName) {
			t.Fatalf("%s lost its workspace display name", session.ID)
		}
	}
}
