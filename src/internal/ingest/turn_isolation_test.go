package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
	_ "modernc.org/sqlite"
)

// Regression anchors for CU-20260924-005: forked Codex histories reuse turn_id
// values, and one session the store rejects must not skip the rest of a pass.

func writeCodexTranscript(t *testing.T, root, sessionID, turnID string) {
	t.Helper()
	path := filepath.Join(root, "sessions", "2026", sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":%q,"cwd":"/tmp/repo"}}`, sessionID),
		fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:01Z","type":"turn_context","payload":{"turn_id":%q,"model":"gpt-test"}}`, turnID),
		fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"Inspect %s"}]}}`, sessionID),
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"c1","name":"read_file","arguments":"{\"path\":\"README.md\"}"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// rejectSession makes the database refuse one session row, standing in for any
// per-session persistence failure.
func rejectSession(t *testing.T, databasePath, sessionID string) {
	t.Helper()
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_session BEFORE INSERT ON sessions WHEN NEW.id=%q BEGIN SELECT RAISE(ABORT,'rejected by test'); END`, sessionID)); err != nil {
		t.Fatal(err)
	}
}

func openIsolationStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestImportAllKeepsCodexSessionsThatShareATurnID(t *testing.T) {
	root := t.TempDir()
	writeCodexTranscript(t, root, "fork-a", "t1")
	writeCodexTranscript(t, root, "fork-b", "t1")
	store, _ := openIsolationStore(t)

	result, err := ImportAll(context.Background(), store, root)
	if err != nil || result.Imported != 2 || result.Failed != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, id := range []string{"fork-a", "fork-b"} {
		session, getErr := store.GetSession(context.Background(), id)
		if getErr != nil || len(session.Turns) != 1 || len(session.ToolCalls) != 1 {
			t.Fatalf("%s: session=%+v err=%v", id, session, getErr)
		}
		turn := session.Turns[0]
		if turn.ID != id+":t1" || turn.ExternalTurnID != "t1" || turn.SessionID != id {
			t.Fatalf("%s: turn identity=%q external=%q session=%q", id, turn.ID, turn.ExternalTurnID, turn.SessionID)
		}
		if session.ToolCalls[0].TurnID != turn.ID {
			t.Fatalf("%s: tool call linked to %q, want %q", id, session.ToolCalls[0].TurnID, turn.ID)
		}
		for _, event := range session.Events {
			if event.TurnID != turn.ID {
				t.Fatalf("%s: event %s linked to %q, want %q", id, event.ID, event.TurnID, turn.ID)
			}
		}
	}
}

func TestImportAllRecordsARejectedSessionAndContinues(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"keep-a", "reject-me", "keep-b"} {
		writeCodexTranscript(t, root, id, "turn-"+id)
	}
	store, path := openIsolationStore(t)
	rejectSession(t, path, "reject-me")

	result, err := ImportAll(context.Background(), store, root)
	if err != nil || result.Imported != 2 || result.Failed != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "rejected by test") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, id := range []string{"keep-a", "keep-b"} {
		if _, getErr := store.GetSession(context.Background(), id); getErr != nil {
			t.Fatalf("%s was skipped: %v", id, getErr)
		}
	}
	if _, getErr := store.GetSession(context.Background(), "reject-me"); getErr == nil {
		t.Fatal("rejected session was stored")
	}
	// The failed transcript keeps no fingerprint, so the next pass retries it.
	retry, err := ImportAll(context.Background(), store, root)
	if err != nil || retry.Skipped != 2 || retry.Failed != 1 || retry.Imported != 0 {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
}

func TestImportClaudeRecordsARejectedSessionAndContinues(t *testing.T) {
	root := t.TempDir()
	ids := []string{"0d7c9f8e-1234-4a5b-8c9d-0123456789aa", "0d7c9f8e-1234-4a5b-8c9d-0123456789bb"}
	for _, id := range ids {
		path := filepath.Join(root, "projects", "p", id+".jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		line, _ := json.Marshal(map[string]any{"type": "user", "uuid": "u1", "sessionId": id, "timestamp": "2026-09-23T10:00:00Z", "promptId": "p1", "message": map[string]any{"role": "user", "content": "Inspect " + id}})
		if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, path := openIsolationStore(t)
	rejectSession(t, path, "claude:"+ids[0])

	result, err := ImportClaude(context.Background(), store, root)
	if err != nil || result.Imported != 1 || result.Failed != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "rejected by test") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, getErr := store.GetSession(context.Background(), "claude:"+ids[1]); getErr != nil {
		t.Fatalf("remaining transcript was skipped: %v", getErr)
	}
}

func TestImportChatGPTRecordsARejectedConversationWithoutReconciling(t *testing.T) {
	conversation := `{"id":%q,"title":"Chat","create_time":1,"update_time":2,"current_node":"u","mapping":{"u":{"id":"u","message":{"id":"u","author":{"role":"user"},"create_time":1,"content":{"parts":["Hello"]}}}}}`
	exportPath := filepath.Join(t.TempDir(), "conversations.json")
	body := "[" + fmt.Sprintf(conversation, "chat-old") + "]"
	if err := os.WriteFile(exportPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store, path := openIsolationStore(t)
	if _, err := ImportChatGPT(context.Background(), store, exportPath); err != nil {
		t.Fatal(err)
	}
	var oldID string
	sessions, _ := store.ListSessions(context.Background())
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d", len(sessions))
	}
	oldID = sessions[0].ID

	// The export now lists two new chats and one is rejected: the other is
	// saved, and the old chat survives because the read was incomplete.
	body = "[" + fmt.Sprintf(conversation, "chat-rejected") + "," + fmt.Sprintf(conversation, "chat-kept") + "]"
	if err := os.WriteFile(exportPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rejectSession(t, path, "chatgpt:chat-rejected")
	result, err := ImportChatGPT(context.Background(), store, exportPath)
	if err != nil || result.Imported != 1 || result.Failed != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "rejected by test") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, getErr := store.GetSession(context.Background(), oldID); getErr != nil {
		t.Fatalf("partial import reconciled away %s: %v", oldID, getErr)
	}
	if _, getErr := store.GetSession(context.Background(), "chatgpt:chat-kept"); getErr != nil {
		t.Fatalf("remaining conversation was skipped: %v", getErr)
	}
}
