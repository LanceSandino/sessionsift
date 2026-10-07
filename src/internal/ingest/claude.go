// @module IngestClaude imports Claude Code transcripts and Claude.ai data exports incrementally.
package ingest

import (
	"context"
	"fmt"
	"os"

	"github.com/LanceSandino/sessionsift/src/internal/analyze"
	claudediscover "github.com/LanceSandino/sessionsift/src/internal/claude/discover"
	claudeexport "github.com/LanceSandino/sessionsift/src/internal/claude/export"
	claudetranscript "github.com/LanceSandino/sessionsift/src/internal/claude/transcript"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

// ImportClaude imports changed main-session and subagent transcripts under a
// Claude Code home (~/.claude or $CLAUDE_CONFIG_DIR). Unchanged files are
// skipped by size, modification time, and parser version.
func ImportClaude(ctx context.Context, store *storage.Store, claudeHome string) (Result, error) {
	sources, err := claudediscover.Sessions(claudeHome)
	if err != nil {
		return Result{}, err
	}
	result := Result{Discovered: len(sources)}
	records, err := store.ImportRecords(ctx)
	if err != nil {
		return result, err
	}
	parser := claudetranscript.New()
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		info, err := os.Stat(source.Path)
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, redact.DisplayPath(source.Path)+": unavailable")
			continue
		}
		fingerprint := fmt.Sprintf("%s:%d:%d", domain.ClaudeCodeParserVersion, info.Size(), info.ModTime().UnixNano())
		pathHash := redact.Hash("claude-code:" + source.Path)
		if previous, imported := records[pathHash]; imported && previous.Fingerprint == fingerprint {
			result.Skipped++
			continue
		}
		session, _, err := parser.ParseFile(source.Path, source.Subagent)
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", redact.DisplayPath(source.Path), err))
			continue
		}
		findings, err := analyze.Run(ctx, session, analyze.Default())
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, err.Error())
			continue
		}
		session.Findings = findings
		scrubWorkspace(&session)
		if err := store.SaveImport(ctx, pathHash, fingerprint, session); err != nil {
			// As for Codex: report it, retry next pass, keep importing the rest.
			result.Failed++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", redact.DisplayPath(source.Path), err))
			continue
		}
		result.Imported++
	}
	return result, nil
}

// ImportClaudeExport imports conversations from a Claude.ai export ZIP or
// conversations JSON. Account, profile, and project files are never read.
func ImportClaudeExport(ctx context.Context, store *storage.Store, path string) (Result, error) {
	return importExport(ctx, store, path, exportFormat{label: "Claude", version: domain.ClaudeExportVersion, parse: claudeexport.Parse})
}
