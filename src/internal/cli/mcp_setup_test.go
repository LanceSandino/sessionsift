// @module MCPSetupCLITests anchors opt-in configuration and legacy printer compatibility.
package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPSetupCLIRequiresOptInAndPreservesPrinter(t *testing.T) {
	home := t.TempDir()
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("SESSIONSIFT_HOME", filepath.Join(home, "state"))
	configPath := filepath.Join(home, "codex", "config.toml")
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), args, strings.NewReader(""), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, out, err := run("setup", "mcp"); code != 0 || !strings.Contains(out, "[mcp_servers.sessionsift]") || err != "" {
		t.Fatalf("legacy %d %s %s", code, out, err)
	}
	for _, args := range [][]string{{"--client", "codex"}, {"--client", "codex", "--apply"}, {"--client", "codex", "--undo"}, {"--client", "bad"}, {"--client", "codex", "--apply", "--undo", "--yes"}, {"--client", "codex", "--preview", "--yes"}, {"--bogus"}} {
		code, _, errorText := run(append([]string{"setup", "mcp"}, args...)...)
		if len(args) == 2 && args[1] == "codex" {
			if code != 0 {
				t.Fatalf("preview failed: %s", errorText)
			}
		} else if code != 2 {
			t.Fatalf("args %v code %d", args, code)
		}
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Fatal("unconfirmed operation wrote config")
		}
	}
	if code, _, err := run("setup", "mcp", "--client", "codex", "--apply", "--yes"); code != 0 {
		t.Fatalf("apply %d %s", code, err)
	}
	b, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(b), "[mcp_servers.sessionsift]") {
		t.Fatalf("missing entry %s %v", b, err)
	}
	if code, _, err := run("setup", "mcp", "--client", "codex", "--undo", "--yes"); code != 0 {
		t.Fatalf("undo %d %s", code, err)
	}
	b, err = os.ReadFile(configPath)
	if err != nil || strings.Contains(string(b), "[mcp_servers.sessionsift]") {
		t.Fatalf("remaining entry %s %v", b, err)
	}
}

func TestMCPSetupCLIProfileSelectionAndFailures(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("SESSIONSIFT_HOME", filepath.Join(home, "state"))
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), append([]string{"setup", "mcp"}, args...), strings.NewReader(""), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	for _, client := range []string{"codex", "claude-code"} {
		if code, out, err := run("--client", client, "--preview"); code != 0 || !strings.Contains(out, home) {
			t.Fatalf("preview %d %s %s", code, out, err)
		}
		if code, _, err := run("--client", client, "--apply", "--yes"); code != 0 {
			t.Fatalf("apply %s %s", client, err)
		}
		if code, _, err := run("--client", client, "--undo", "--yes"); code != 0 {
			t.Fatalf("undo %s %s", client, err)
		}
	}
	if code, _, _ := run("--client", "codex", "--command", filepath.Join(home, "missing")); code != 1 {
		t.Fatal("missing launcher accepted")
	}
	if code, _, _ := run("--client", "codex", "--command", "/usr/bin/false", "--arg", "mcp", "--verify"); code != 1 {
		t.Fatal("non MCP launcher verified")
	}
	if code, _, _ := run("--client", "codex", "--command", "/usr/bin/false", "--arg", "https://example.invalid"); code != 1 {
		t.Fatal("URL launcher accepted")
	}
	if code, _, _ := run("--client", "codex", "extra"); code != 2 {
		t.Fatal("trailing operand accepted")
	}
	badConfig := filepath.Join(home, "bad-config")
	if err := os.WriteFile(badConfig, []byte("not TOML"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run("--client", "codex", "--config", badConfig, "--apply", "--yes"); code != 1 {
		t.Fatal("ambiguous config accepted")
	}
}
