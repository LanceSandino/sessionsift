// @module ClaudeTranscript/Classify categorizes Claude Code tools and shell commands and derives targets and signatures.

package transcript

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/toolmeta"
)

var (
	// commandSegments splits a shell line at control operators so each
	// simple command can be inspected by its leading word.
	commandSegments = regexp.MustCompile(`&&|\|\||[;|\n]`)
	// commandWrappers are prefixes that do not change what a command does.
	commandWrappers   = regexp.MustCompile(`^(?:(?:[a-z_][a-z0-9_]*=\S*|sudo|time|env|npx|timeout\s+\S+)\s+)+`)
	directoryChange   = regexp.MustCompile(`^(?:cd|pushd|set-location)\b`)
	validationCommand = regexp.MustCompile(`^(?:go\s+(?:test|build|vet)|(?:npm|pnpm|yarn|bun)(?:\s+run)?\s+(?:test|build|lint|typecheck|type-check)|cargo\s+(?:test|build|check|clippy)|(?:python3?\s+-m\s+)?pytest|vitest|jest|tsc|[a-z0-9_-]*lint)\b`)
	readCommand       = regexp.MustCompile(`^(?:cat|head|tail|less|type|get-content|sed\s+-n)\b`)
	searchCommand     = regexp.MustCompile(`^(?:rg|grep|find|ls|select-string|get-childitem)\b`)
	signatureID       = regexp.MustCompile(`\b[0-9a-f]{8,}\b`)
	signatureNumber   = regexp.MustCompile(`\b\d{4,}\b`)
)

// newToolCall normalizes one tool_use block. The raw input is kept only long
// enough to classify it and derive the target path; what is stored is
// redacted and bounded.
func newToolCall(id, turnID string, at time.Time, name string, raw json.RawMessage) domain.ToolCall {
	arguments := compactJSON(raw)
	var input map[string]any
	decodeInto(raw, &input)
	redactedArgs := redact.Text(arguments, 4_000)
	category := classify(name, input)
	call := domain.ToolCall{ID: id, TurnID: turnID, Timestamp: at, ToolName: name, ToolCategory: category, ArgumentsRedacted: redactedArgs, Success: true}
	call.ToolTransport, call.ToolServer = toolmeta.Describe(name)
	if target := targetPath(input); target != "" {
		call.TargetPathHash = redact.PathHash(target)
		call.TargetPathDisplay = redact.DisplayPath(redact.Text(target, 0))
	}
	if category == "execute" || category == "validation" {
		source := redactedArgs
		if command := stringField(input, "command"); command != "" {
			source = redact.Text(command, 4_000)
		}
		call.CommandSignature = commandSignature(source)
	}
	return call
}

// classify maps Claude Code's built-in tool names to SessionSift categories.
func classify(name string, input map[string]any) string {
	if transport, _ := toolmeta.Describe(name); transport == "mcp" {
		return "other"
	}
	switch strings.TrimSpace(name) {
	case "Read", "NotebookRead":
		return "read"
	case "Grep", "Glob", "LS", "ToolSearch":
		return "search"
	case "WebSearch", "WebFetch":
		return "network"
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return "edit"
	case "Bash", "PowerShell":
		return classifyCommand(stringField(input, "command"))
	case "Task", "Agent":
		return "delegate"
	case "TodoWrite", "TaskCreate", "TaskUpdate":
		return "plan"
	}
	return "execute"
}

// classifyCommand treats a shell line as validation when any simple command
// in it runs tests, builds, vets, lints or type-checks; otherwise the first
// command after any directory change decides read, search or execute.
func classifyCommand(command string) string {
	leading := ""
	for _, segment := range commandSegments.Split(strings.ToLower(command), -1) {
		segment = commandWrappers.ReplaceAllString(strings.TrimSpace(segment), "")
		if segment == "" {
			continue
		}
		if validationCommand.MatchString(segment) {
			return "validation"
		}
		if leading == "" && !directoryChange.MatchString(segment) {
			leading = segment
		}
	}
	switch {
	case readCommand.MatchString(leading):
		return "read"
	case searchCommand.MatchString(leading):
		return "search"
	}
	return "execute"
}

func targetPath(input map[string]any) string {
	for _, key := range []string{"file_path", "notebook_path", "path"} {
		if value := strings.TrimSpace(stringField(input, key)); value != "" {
			return value
		}
	}
	return ""
}

func stringField(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

// commandSignature groups repeated commands: it lowercases and collapses
// whitespace, then masks ids and long numbers. Input must already be redacted.
func commandSignature(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	value = signatureID.ReplaceAllString(value, "<id>")
	value = signatureNumber.ReplaceAllString(value, "<n>")
	return clip(value, 500)
}

// clip truncates to at most limit bytes without splitting a UTF-8 rune.
func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && value[limit]&0xC0 == 0x80 {
		limit--
	}
	return value[:limit]
}
