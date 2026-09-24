// @module ClaudeTranscript/Clean strips harness-injected context from user text and renders slash commands.

package transcript

import (
	"regexp"
	"strings"
)

// strippedBlocks are harness wrappers Claude Code adds around or instead of
// what the user typed: reminders, local command output, IDE context.
var strippedBlocks = compileBlocks("system-reminder", "local-command-stdout", "local-command-stderr", "local-command-caveat", "ide_opened_file", "ide_selection", "command-message")

var (
	commandName = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	commandArgs = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
)

func compileBlocks(tags ...string) []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(tags))
	for _, tag := range tags {
		patterns = append(patterns, regexp.MustCompile(`(?s)<`+tag+`\b[^>]*>.*?</`+tag+`>`))
	}
	return patterns
}

// cleanPrompt reduces a user record to the words the user typed, rendering a
// slash command as "/name args".
func cleanPrompt(value string) string {
	for _, pattern := range strippedBlocks {
		value = pattern.ReplaceAllString(value, "")
	}
	if match := commandName.FindStringSubmatch(value); match != nil {
		name := strings.TrimSpace(match[1])
		if name != "" && !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		args := ""
		if found := commandArgs.FindStringSubmatch(value); found != nil {
			args = strings.TrimSpace(found[1])
		}
		rest := strings.TrimSpace(commandArgs.ReplaceAllString(commandName.ReplaceAllString(value, ""), ""))
		value = strings.TrimSpace(name + " " + args)
		if rest != "" {
			value = strings.TrimSpace(value + "\n" + rest)
		}
	}
	return strings.TrimSpace(value)
}

// isInterrupt recognizes the marker Claude Code writes when the user presses
// Escape; it is neither a prompt nor steering.
func isInterrupt(text string) bool {
	return strings.HasPrefix(text, "[Request interrupted by user")
}
