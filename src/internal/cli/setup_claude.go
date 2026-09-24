// @module CLISetupClaude prints and applies Claude Code integration: hooks, MCP, and exports.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	claudediscover "github.com/LanceSandino/sessionsift/src/internal/claude/discover"
	"github.com/LanceSandino/sessionsift/src/internal/claudeconfig"
)

// enableClaudeSetup merges SessionSift hook entries into Claude Code settings.
func enableClaudeSetup(out, errOut io.Writer) int {
	home, err := claudediscover.Home()
	if err != nil {
		return fail(errOut, err)
	}
	binary, err := os.Executable()
	if err != nil {
		return fail(errOut, err)
	}
	if binary, err = filepath.Abs(binary); err != nil {
		return fail(errOut, err)
	}
	result, err := claudeconfig.Enable(home, binary)
	if err != nil {
		return fail(errOut, err)
	}
	if !result.HooksChanged {
		fmt.Fprintln(out, "SessionSift Claude Code hooks are already enabled.")
		return 0
	}
	fmt.Fprintf(out, "Enabled SessionSift Claude Code hooks in %s. Review them with /hooks in Claude Code.\n", result.SettingsPath)
	return 0
}

const claudeSnippet = `Claude Code transcripts under ~/.claude/projects (or $CLAUDE_CONFIG_DIR/projects)
are imported automatically by "sessionsift start" and "sessionsift import".

Live events (optional): run "sessionsift setup enable claude", or merge this into
the "hooks" object of ~/.claude/settings.json yourself:
  "SessionStart":     [{"hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}],
  "UserPromptSubmit": [{"hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}],
  "PostToolUse":      [{"matcher":"*","hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}],
  "PreCompact":       [{"hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}],
  "Stop":             [{"hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}],
  "SubagentStop":     [{"hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}],
  "SessionEnd":       [{"hooks":[{"type":"command","command":"sessionsift hook claude","timeout":10}]}]
The hook never blocks Claude Code and prints nothing; payloads are redacted locally.

Local MCP for Claude Code:
  claude mcp add --scope user sessionsift -- sessionsift mcp

Claude.ai conversations: export them from Settings > Privacy > Export data, then run
  sessionsift import-claude /path/to/claude-export.zip
Only conversations.json is read; account and profile files in the export are ignored.`
