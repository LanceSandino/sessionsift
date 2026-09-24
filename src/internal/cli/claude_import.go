// @module CLIClaudeImport imports and watches Claude Code transcripts and Claude.ai exports.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	claudediscover "github.com/LanceSandino/sessionsift/src/internal/claude/discover"
	"github.com/LanceSandino/sessionsift/src/internal/config"
	"github.com/LanceSandino/sessionsift/src/internal/ingest"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

// claudeWatchInterval paces Claude Code transcript reconciliation.
var claudeWatchInterval = 3 * time.Second

func importAndWatchClaude(ctx context.Context, store *storage.Store, home string, out, errOut io.Writer) {
	result, err := ingest.ImportClaude(ctx, store, home)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(errOut, "Claude Code import warning: %v\n", err)
		}
	} else if result.Discovered > 0 {
		fmt.Fprintf(out, "Imported %d of %d Claude Code transcripts (%d unchanged).\n", result.Imported, result.Discovered, result.Skipped)
		reportImportFailures(errOut, "Claude Code", result)
	}
	ticker := time.NewTicker(claudeWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err = ingest.ImportClaude(ctx, store, home); err != nil && ctx.Err() == nil {
				fmt.Fprintf(errOut, "Claude Code live import warning: %v\n", err)
			}
		}
	}
}

// importAndWatchExport imports a remembered export and re-imports it when the
// file is replaced by a newer export.
func importAndWatchExport(ctx context.Context, store *storage.Store, path, label string, importer func(context.Context, *storage.Store, string) (ingest.Result, error), out, errOut io.Writer) {
	if result, err := importer(ctx, store, path); err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(errOut, "%s import warning: %v\n", label, err)
		}
	} else {
		fmt.Fprintf(out, "Imported %d of %d %s conversations (%d unchanged).\n", result.Imported, result.Discovered, label, result.Skipped)
		reportImportFailures(errOut, label, result)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := fileIdentity(path)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := fileIdentity(path)
			if current == "" || current == last {
				continue
			}
			if _, err := importer(ctx, store, path); err != nil && ctx.Err() == nil {
				fmt.Fprintf(errOut, "%s import warning: %v\n", label, err)
				continue
			}
			last = current
		}
	}
}

func importClaudeExport(ctx context.Context, paths config.Paths, path string, out, errOut io.Writer) int {
	store, err := openStore(paths)
	if err != nil {
		return fail(errOut, err)
	}
	defer store.Close()
	result, err := ingest.ImportClaudeExport(ctx, store, path)
	if err != nil {
		return fail(errOut, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fail(errOut, err)
	}
	if err = os.WriteFile(paths.ClaudeExport, []byte(filepath.Clean(absolute)+"\n"), 0o600); err != nil {
		return fail(errOut, err)
	}
	return write(out, errOut, result)
}

// rememberedPath returns an environment override or a private remembered path.
func rememberedPath(file, environment string) string {
	if configured := strings.TrimSpace(os.Getenv(environment)); configured != "" {
		return filepath.Clean(configured)
	}
	value, err := os.ReadFile(file)
	if err != nil || strings.TrimSpace(string(value)) == "" {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(string(value)))
}

// claudeDoctor reports Claude Code and Claude.ai source health without
// reading transcript contents.
func claudeDoctor(paths config.Paths, claudeCodeCount, claudeCount int) string {
	home, err := claudediscover.Home()
	transcripts := "not found\t!"
	hooks := "not configured (optional)\t!"
	if err == nil {
		if sources, scanErr := claudediscover.Sessions(home); scanErr == nil && len(sources) > 0 {
			transcripts = fmt.Sprintf("%d found\t✓", len(sources))
		}
		if settings, readErr := os.ReadFile(filepath.Join(home, "settings.json")); readErr == nil && strings.Contains(string(settings), "hook claude") {
			hooks = "configured\t✓"
		}
	}
	export := "not configured\t!"
	if path := rememberedPath(paths.ClaudeExport, "SESSIONSIFT_CLAUDE_EXPORT"); path != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			export = "watching " + filepath.Base(path) + "\t✓"
		} else {
			export = "configured path missing\t!"
		}
	}
	return fmt.Sprintf("Claude Code      %s\nClaude sessions  %d imported\nClaude hooks     %s\nClaude.ai source %s\nClaude.ai chats  %d imported\n", transcripts, claudeCodeCount, hooks, export, claudeCount)
}
