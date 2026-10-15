// @module MCPSetupVerify performs bounded local stdio discovery without reading transcripts.
package mcpsetup

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Verification struct {
	Tools    []string
	ReadOnly bool
}

// Verify starts the explicitly selected local launcher and discovers tools only.
// It calls no tools, reads no sessions, uses no LLM and inherits no API/OAuth keys.
func Verify(ctx context.Context, o Options) (Verification, error) {
	if e := validateOptions(o); e != nil {
		return Verification{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, o.Command, o.Args...)
	command.Env = verificationEnvironment(os.Environ())
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	client := sdk.NewClient(&sdk.Implementation{Name: "sessionsift-local-setup-verification", Version: "1"}, nil)
	session, e := client.Connect(ctx, &sdk.CommandTransport{Command: command, TerminateDuration: time.Second}, nil)
	if e != nil {
		return Verification{}, errors.New("local MCP initialization failed; check the selected command and start the local agent")
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, nil)
	if e != nil {
		return Verification{}, errors.New("local MCP tool discovery failed")
	}
	return verifiedTools(tools)
}
func verifiedTools(result *sdk.ListToolsResult) (Verification, error) {
	expected := map[string]bool{"list_sessions": true, "get_session": true, "get_session_metrics": true, "get_session_timeline": true, "get_turn": true, "get_findings": true, "compare_sessions": true, "get_analytics": true, "get_recurring_corrections": true, "get_context_recommendations": true}
	if result == nil || len(result.Tools) != len(expected) || result.NextCursor != "" {
		return Verification{}, errors.New("expected exactly ten local read-only MCP tools")
	}
	names := []string{}
	for _, tool := range result.Tools {
		if tool == nil || !expected[tool.Name] || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			return Verification{}, errors.New("unexpected or non-read-only MCP tool")
		}
		delete(expected, tool.Name)
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return Verification{Tools: names, ReadOnly: true}, nil
}
func verificationEnvironment(input []string) []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "CODEX_HOME": true, "CLAUDE_CONFIG_DIR": true, "SESSIONSIFT_HOME": true, "SESSIONSIFT_ADDR": true, "TMPDIR": true}
	output := []string{}
	for _, entry := range input {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if allowed[key] || (key == "DOCKER_HOST" && strings.HasPrefix(value, "unix://")) {
			output = append(output, entry)
		}
	}
	return output
}
