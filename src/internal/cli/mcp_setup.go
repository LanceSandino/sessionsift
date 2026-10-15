// @module MCPSetupCLI parses explicit local stdio onboarding without changing legacy printers.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/mcpsetup"
)

type mcpArgs []string

func (a *mcpArgs) String() string         { return fmt.Sprint([]string(*a)) }
func (a *mcpArgs) Set(value string) error { *a = append(*a, value); return nil }

func setupMCP(ctx context.Context, paths config.Paths, args []string, out, errOut io.Writer) int {
	f := flag.NewFlagSet("setup mcp", flag.ContinueOnError)
	f.SetOutput(errOut)
	client := f.String("client", "", "codex or claude-code")
	preview := f.Bool("preview", false, "print proposed configuration only (default)")
	apply := f.Bool("apply", false, "install only the owned local stdio entry")
	undo := f.Bool("undo", false, "remove only the unchanged owned entry")
	verify := f.Bool("verify", false, "discover ten read-only tools, without reading sessions")
	yes := f.Bool("yes", false, "confirm configuration mutation; close client/editors first")
	configPath := f.String("config", "", "explicit absolute client config path")
	command := f.String("command", "", "explicit absolute executable (default this binary)")
	var commandArgs mcpArgs
	f.Var(&commandArgs, "arg", "launcher argument, repeatable; defaults to mcp")
	if err := f.Parse(args); err != nil {
		return 2
	}
	actions := 0
	for _, selected := range []bool{*preview, *apply, *undo, *verify} {
		if selected {
			actions++
		}
	}
	if f.NArg() != 0 || actions > 1 || (*yes && !*apply && !*undo) || ((*apply || *undo) && !*yes) || (*client != "codex" && *client != "claude-code") {
		fmt.Fprintln(errOut, "use setup mcp --client codex|claude-code [--preview|--verify|--apply --yes|--undo --yes]; close clients/editors before apply/undo")
		return 2
	}
	if *configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fail(errOut, err)
		}
		if *client == "codex" {
			root := os.Getenv("CODEX_HOME")
			if root == "" {
				root = filepath.Join(home, ".codex")
			}
			*configPath = filepath.Join(root, "config.toml")
		} else {
			root := os.Getenv("CLAUDE_CONFIG_DIR")
			if root == "" {
				root = home
			}
			*configPath = filepath.Join(root, ".claude.json")
		}
	}
	if *command == "" {
		executable, err := os.Executable()
		if err != nil {
			return fail(errOut, err)
		}
		*command = executable
	}
	// Homebrew's bin entry is a symlink: persist the verified executable target,
	// never a mutable shell command or arbitrary URL.
	resolved, err := filepath.EvalSymlinks(*command)
	if err != nil {
		return fail(errOut, err)
	}
	if len(commandArgs) == 0 {
		commandArgs = mcpArgs{"mcp"}
	}
	o := mcpsetup.Options{Client: *client, ConfigPath: *configPath, StateDir: filepath.Join(paths.Home, "mcp-setup", *client), Command: resolved, Args: commandArgs}
	if *verify {
		result, err := mcpsetup.Verify(ctx, o)
		if err != nil {
			return fail(errOut, err)
		}
		fmt.Fprintf(out, "Verified %d local read-only tools; no tools called, no sessions read.\n", len(result.Tools))
		return 0
	}
	if !*apply && !*undo {
		result, err := mcpsetup.Preview(o)
		if err != nil {
			return fail(errOut, err)
		}
		fmt.Fprintf(out, "Preview only; no files changed. Target: %s\n%s\n", result.ConfigPath, result.Snippet)
		return 0
	}
	var result mcpsetup.Result
	if *undo {
		result, err = mcpsetup.Undo(o)
	} else {
		result, err = mcpsetup.Apply(o)
	}
	if err != nil {
		return fail(errOut, err)
	}
	fmt.Fprintf(out, "Local MCP configuration changed=%t; target=%s\n", result.Changed, result.ConfigPath)
	return 0
}
