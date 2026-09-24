package transcript

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

func TestClassifyToolNames(t *testing.T) {
	cases := map[string]string{
		"Read": "read", "NotebookRead": "read",
		"Grep": "search", "Glob": "search", "LS": "search", "ToolSearch": "search",
		"WebSearch": "network", "WebFetch": "network",
		"Edit": "edit", "Write": "edit", "MultiEdit": "edit", "NotebookEdit": "edit",
		"Task": "delegate", "Agent": "delegate",
		"TodoWrite": "plan", "TaskCreate": "plan", "TaskUpdate": "plan",
		"mcp__github__create_issue": "other", "mcp__read__Read": "other",
		"Skill": "execute", "": "execute",
	}
	for name, want := range cases {
		if got := classify(name, nil); got != want {
			t.Errorf("classify(%q)=%q want %q", name, got, want)
		}
	}
}

func TestClassifyShellCommands(t *testing.T) {
	cases := map[string]string{
		"go test ./...":                       "validation",
		"cd src && npm run build":             "validation",
		"npm test":                            "validation",
		"npm run test:unit":                   "validation",
		"pnpm test":                           "validation",
		"yarn test --watch=false":             "validation",
		"cargo test":                          "validation",
		"pytest -q":                           "validation",
		"python -m pytest tests":              "validation",
		"npx vitest run":                      "validation",
		"jest --ci":                           "validation",
		"go build ./...":                      "validation",
		"go vet ./...":                        "validation",
		"tsc --noEmit":                        "validation",
		"npm run lint":                        "validation",
		"golangci-lint run":                   "validation",
		"GOFLAGS=-count=1 go test ./...":      "validation",
		"timeout 60 go test ./pkg":            "validation",
		"echo start; go test ./...":           "validation",
		"cat README.md":                       "read",
		"head -n 5 main.go":                   "read",
		"tail -f server.log":                  "read",
		"sed -n '1,20p' main.go":              "read",
		"less notes.txt":                      "read",
		"type config.txt":                     "read",
		"Get-Content .\\a.txt":                "read",
		"cat main.go | grep func":             "read",
		"rg TODO src":                         "search",
		"grep -rn 'go test' Makefile":         "search",
		"find . -name '*.go'":                 "search",
		"ls -la":                              "search",
		"Select-String -Path *.go -Pattern x": "search",
		"Get-ChildItem -Recurse":              "search",
		"cd src && ls":                        "search",
		"Set-Location C:\\x; Get-ChildItem":   "search",
		"git status":                          "execute",
		"sed -i 's/a/b/' file":                "execute",
		"typecheck-me":                        "execute",
		"":                                    "execute",
		"cd /tmp":                             "execute",
	}
	for command, want := range cases {
		if got := classifyCommand(command); got != want {
			t.Errorf("classifyCommand(%q)=%q want %q", command, got, want)
		}
		if got := classify("Bash", map[string]any{"command": command}); got != want {
			t.Errorf("Bash %q=%q want %q", command, got, want)
		}
	}
	if got := classify("PowerShell", map[string]any{"command": "Get-Content a.txt"}); got != "read" {
		t.Fatalf("PowerShell=%q", got)
	}
}

func TestNewToolCallTargetsAndSignatures(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	edit := newToolCall("t1", "turn", at, "NotebookEdit", json.RawMessage(`{"notebook_path": "/work/repo/nb/a.ipynb", "new_source": "x"}`))
	if edit.ToolCategory != "edit" || edit.TargetPathDisplay != "nb/a.ipynb" || edit.TargetPathHash != redact.PathHash("/work/repo/nb/a.ipynb") || edit.CommandSignature != "" || !edit.Success {
		t.Fatalf("edit=%+v", edit)
	}
	if edit.ArgumentsRedacted != `{"notebook_path":"/work/repo/nb/a.ipynb","new_source":"x"}` {
		t.Fatalf("arguments not compacted: %q", edit.ArgumentsRedacted)
	}
	home := newToolCall("t2", "turn", at, "Read", json.RawMessage(`{"file_path":"C:\\Users\\someone\\notes.txt"}`))
	if strings.Contains(home.TargetPathDisplay, "someone") || home.TargetPathHash != redact.PathHash(`C:\Users\someone\notes.txt`) {
		t.Fatalf("home target=%+v", home)
	}
	shell := newToolCall("t3", "turn", at, "Bash", json.RawMessage(`{"command":"git   checkout 0123abcd4567 && echo 98765"}`))
	if shell.ToolCategory != "execute" || shell.CommandSignature != "git checkout <id> && echo <n>" {
		t.Fatalf("shell=%+v", shell)
	}
	empty := newToolCall("t4", "", at, "ExitPlanMode", nil)
	if empty.ArgumentsRedacted != "" || empty.CommandSignature != "" || empty.ToolCategory != "execute" {
		t.Fatalf("empty=%+v", empty)
	}
	long := commandSignature(strings.Repeat("é", 400))
	if len(long) > 500 || !strings.HasPrefix(strings.Repeat("é", 250), long) {
		t.Fatalf("signature not clipped on a rune boundary: %d bytes", len(long))
	}
	if clip("ééé", 3) != "é" || clip("abc", 5) != "abc" {
		t.Fatal("clip")
	}
}
