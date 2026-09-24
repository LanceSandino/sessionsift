package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/ingest"
)

const claudeTranscript = `{"type":"user","uuid":"u1","sessionId":"cc-1","timestamp":"2026-09-01T10:00:00Z","cwd":"/work/app","promptId":"p1","message":{"role":"user","content":"Fix the flaky test"}}
{"type":"assistant","uuid":"a1","sessionId":"cc-1","timestamp":"2026-09-01T10:00:05Z","cwd":"/work/app","message":{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Done"}],"usage":{"input_tokens":10,"cache_read_input_tokens":90,"output_tokens":5}}}
`

func TestImportIncludesClaudeCodeAndClaudeExports(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", filepath.Join(root, "state"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	claudeHome := filepath.Join(root, "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	transcript := filepath.Join(claudeHome, "projects", "app", "cc-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(claudeTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"import"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), `"claude_code"`) {
		t.Fatalf("import code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"sessions"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "claude:cc-1") || !strings.Contains(out.String(), "Fix the flaky test") {
		t.Fatalf("sessions=%s err=%s", out.String(), errOut.String())
	}

	export := filepath.Join(root, "conversations.json")
	body := `[{"uuid":"chat-1","name":"Trip plan","created_at":"2026-09-01T09:00:00Z","updated_at":"2026-09-01T09:05:00Z","account":{"uuid":"acct"},"chat_messages":[{"uuid":"m1","sender":"human","text":"Plan a trip","created_at":"2026-09-01T09:00:00Z"},{"uuid":"m2","sender":"assistant","text":"Here is a plan","created_at":"2026-09-01T09:01:00Z"}]}]`
	if err := os.WriteFile(export, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"import-claude", export}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), `"imported": 1`) {
		t.Fatalf("import-claude code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	if code := Run(context.Background(), []string{"import-claude"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("missing export path code=%d", code)
	}
	paths, _ := config.Resolve()
	if remembered := rememberedPath(paths.ClaudeExport, "SESSIONSIFT_CLAUDE_EXPORT"); remembered != filepath.Clean(export) {
		t.Fatalf("remembered=%q", remembered)
	}
	out.Reset()
	previousHealth := getHealth
	getHealth = func() (*http.Response, error) { return nil, context.DeadlineExceeded }
	t.Cleanup(func() { getHealth = previousHealth })
	if code := Run(context.Background(), []string{"doctor"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Claude Code      1 found") || !strings.Contains(out.String(), "Claude sessions  1 imported") || !strings.Contains(out.String(), "Claude.ai chats  1 imported") || !strings.Contains(out.String(), "watching conversations.json") {
		t.Fatalf("doctor=%s", out.String())
	}
}

func TestClaudeSetupPrintsAndEnablesHooks(t *testing.T) {
	claudeHome := filepath.Join(t.TempDir(), "claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("SESSIONSIFT_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"setup", "claude"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "claude mcp add") || !strings.Contains(out.String(), "import-claude") {
		t.Fatalf("setup claude=%s", out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "mcp"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Claude Code") || !strings.Contains(out.String(), "claude_desktop_config.json") {
		t.Fatalf("setup mcp=%s", out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "enable", "claude"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "Enabled SessionSift Claude Code hooks") {
		t.Fatalf("enable code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	settings, err := os.ReadFile(filepath.Join(claudeHome, "settings.json"))
	if err != nil || !strings.Contains(string(settings), "hook claude") {
		t.Fatalf("settings=%s err=%v", settings, err)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"setup", "enable", "claude"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "already enabled") {
		t.Fatalf("second enable=%s", out.String())
	}
	if err = os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"setup", "enable", "claude"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("invalid settings code=%d", code)
	}
}

func TestClaudeHookSpoolsUnderProviderPrefixAndReplays(t *testing.T) {
	state := t.TempDir()
	t.Setenv("SESSIONSIFT_HOME", state)
	t.Setenv("SESSIONSIFT_ADDR", "127.0.0.1:1")
	payload := `{"session_id":"cc-live","hook_event_name":"UserPromptSubmit","prompt":"use token ghp_abcdefghijklmnopqrstuvwxyz","cwd":"/Users/someone/app"}`
	var errOut bytes.Buffer
	if code := Run(context.Background(), []string{"hook", "claude"}, strings.NewReader(payload), &bytes.Buffer{}, &errOut); code != 0 {
		t.Fatalf("hook code=%d err=%s", code, errOut.String())
	}
	paths, _ := config.Resolve()
	entries, err := os.ReadDir(paths.RawEvents)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "claude-") {
		t.Fatalf("spool=%v err=%v", entries, err)
	}
	store, err := openStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if replayed, err := ingest.ImportSpool(context.Background(), store, paths.RawEvents); err != nil || replayed != 1 {
		t.Fatalf("replayed=%d err=%v", replayed, err)
	}
	session, err := store.GetSession(context.Background(), "claude:cc-live")
	if err != nil || session.Source != domain.SourceClaudeCode || session.Title == "" || strings.Contains(session.Title, "ghp_") {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	if strings.Contains(stringifyEvents(session), "/Users/someone") {
		t.Fatal("spooled hook retained a workstation path")
	}
}

func stringifyEvents(session domain.Session) string {
	var builder strings.Builder
	for _, event := range session.Events {
		for key, value := range event.PayloadRedacted {
			builder.WriteString(key)
			builder.WriteString("=")
			if text, ok := value.(string); ok {
				builder.WriteString(text)
			}
			builder.WriteString(";")
		}
	}
	return builder.String()
}
