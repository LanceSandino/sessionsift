// @module MCPSetupVerifyTests proves discovery-only verification and credential stripping.
package mcpsetup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestVerificationEnvironmentStripsCredentialsAndRemoteDocker(t *testing.T) {
	env := verificationEnvironment([]string{"HOME=/synthetic", "SESSIONSIFT_HOME=/synthetic/state", "PATH=/usr/bin", "OPENAI_API_KEY=must-not-pass", "ANTHROPIC_API_KEY=must-not-pass", "OAUTH_ACCESS_TOKEN=must-not-pass", "DOCKER_HOST=tcp://remote.invalid:2376", "DOCKER_CONTEXT=unverified-remote", "DOCKER_CONFIG=/private-auth", "SESSIONSIFT_BRIDGE_URL=https://real.invalid"})
	got := strings.Join(env, "\n")
	if strings.Contains(got, "must-not-pass") || strings.Contains(got, "remote") || strings.Contains(got, "private-auth") || strings.Contains(got, "real.invalid") {
		t.Fatal(got)
	}
	if len(env) != 3 {
		t.Fatal(env)
	}
	if env = verificationEnvironment([]string{"DOCKER_HOST=unix:///synthetic/docker.sock"}); len(env) != 1 {
		t.Fatal("local Docker host discarded")
	}
}
func goodTools() *sdk.ListToolsResult {
	closed := false
	result := &sdk.ListToolsResult{}
	for _, name := range []string{"list_sessions", "get_session", "get_session_metrics", "get_session_timeline", "get_turn", "get_findings", "compare_sessions", "get_analytics", "get_recurring_corrections", "get_context_recommendations"} {
		result.Tools = append(result.Tools, &sdk.Tool{Name: name, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &closed, OpenWorldHint: &closed}})
	}
	return result
}
func TestToolDiscoveryRefusesMissingDuplicateUnsafeAndPaginated(t *testing.T) {
	if result, e := verifiedTools(goodTools()); e != nil || !result.ReadOnly || len(result.Tools) != 10 {
		t.Fatal(result, e)
	}
	for _, variant := range []string{"missing", "duplicate", "write", "annotation", "open", "pagination", "nil"} {
		t.Run(variant, func(t *testing.T) {
			tools := goodTools()
			switch variant {
			case "missing":
				tools.Tools = tools.Tools[:9]
			case "duplicate":
				tools.Tools[0] = tools.Tools[1]
			case "write":
				tools.Tools[0].Annotations.ReadOnlyHint = false
			case "annotation":
				tools.Tools[0].Annotations = nil
			case "open":
				yes := true
				tools.Tools[0].Annotations.OpenWorldHint = &yes
			case "pagination":
				tools.NextCursor = "more"
			case "nil":
				tools.Tools[0] = nil
			}
			if _, e := verifiedTools(tools); e == nil {
				t.Fatal("unsafe tool inventory accepted")
			}
		})
	}
}
func TestVerificationHelperProcess(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "sessionsift-verify-helper-") {
			mode = strings.TrimPrefix(arg, "sessionsift-verify-helper-")
		}
	}
	if mode == "" {
		return
	}
	if mode == "hang" {
		time.Sleep(time.Hour)
		return
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		os.Exit(9)
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "synthetic", Version: "1"}, nil)
	for _, tool := range goodTools().Tools {
		tool.InputSchema = map[string]any{"type": "object"}
		server.AddTool(tool, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) { os.Exit(10); return nil, nil })
	}
	_ = server.Run(context.Background(), &sdk.StdioTransport{})
	os.Exit(0)
}
func TestVerifyUsesRealStdioWithoutCallingTools(t *testing.T) {
	o := options(t, "codex")
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	o.Command, e = filepath.EvalSymlinks(binary)
	if e != nil {
		t.Fatal(e)
	}
	o.Args = []string{"-test.run=TestVerificationHelperProcess", "--", "sessionsift-verify-helper-good"}
	t.Setenv("OPENAI_API_KEY", "must-not-pass")
	result, e := Verify(context.Background(), o)
	if e != nil || len(result.Tools) != 10 || !result.ReadOnly {
		t.Fatalf("verification %+v %v", result, e)
	}
	o.Args = []string{"-test.run=TestVerificationHelperProcess", "--", "sessionsift-verify-helper-hang"}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, e = Verify(ctx, o); e == nil {
		t.Fatal("hung server accepted")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("verification ignored deadline")
	}
}
