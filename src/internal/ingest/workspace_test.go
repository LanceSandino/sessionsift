package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// ASAP4-F10: the session working directory is private (never serialized), but
// its absolute path leaked through prompts, tool arguments, results, titles,
// signatures, events and findings whenever it lay outside the home directory.
func TestWorkspacePathIsScrubbedFromEverySharedField(t *testing.T) {
	for _, cwd := range []string{`C:\Dev\Clients\Acme\checkout`, `/srv/clients/acme/checkout`} {
		escaped := strings.ReplaceAll(cwd, `\`, `\\`)
		forward := strings.ReplaceAll(cwd, `\`, `/`)
		upper := strings.ToUpper(cwd)
		session := domain.Session{
			CWD: cwd, RepoDisplayName: "checkout", Title: "Fix " + cwd,
			Warnings:  []string{"read " + forward + "/a.go"},
			Turns:     []domain.Turn{{UserPromptRedacted: "look at " + cwd + "/src", AssistantSummaryRedacted: "edited " + upper + "/x"}},
			Events:    []domain.Event{{PayloadRedacted: map[string]any{"text": "in " + cwd, "count": 3, "nested": map[string]any{"path": forward}}}},
			ToolCalls: []domain.ToolCall{{ArgumentsRedacted: `{"file_path":"` + escaped + `\\main.go"}`, ResultPreviewRedacted: forward + "/main.go:12", CommandSignature: "cd " + strings.ToLower(forward) + " && go test"}},
			Findings:  []domain.Finding{{Title: "Re-read " + cwd, Summary: "in " + forward, Evidence: []string{cwd + "/a.go"}}},
		}
		scrubWorkspace(&session)
		encoded, _ := json.Marshal(session)
		text := strings.ToLower(string(encoded))
		for _, secret := range []string{"clients", "acme"} {
			if strings.Contains(text, secret) {
				t.Fatalf("cwd %s leaked %q: %s", cwd, secret, encoded)
			}
		}
		if !strings.Contains(session.Turns[0].UserPromptRedacted, "checkout") || session.Events[0].PayloadRedacted["count"] != 3 {
			t.Fatalf("scrub removed meaning: %+v", session)
		}
		if session.CWD != cwd {
			t.Fatal("private CWD must remain available locally")
		}
	}
}

func TestWorkspaceScrubSkipsRootsAndHomeMarkers(t *testing.T) {
	for _, cwd := range []string{"", "/", `C:\`, "~", "/a"} {
		session := domain.Session{CWD: cwd, Title: "keep /a/b and C:\\x"}
		scrubWorkspace(&session)
		if session.Title != "keep /a/b and C:\\x" {
			t.Fatalf("cwd %q changed title to %q", cwd, session.Title)
		}
	}
}
