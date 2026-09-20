// Package ingest coordinates discovery, parsing, analysis, and transactional persistence.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/analyze"
	"github.com/LanceSandino/sessionsift/src/internal/codex/discover"
	"github.com/LanceSandino/sessionsift/src/internal/codex/transcript"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

// Result summarizes an incremental import run.
type Result struct {
	Discovered int      `json:"discovered"`
	Imported   int      `json:"imported"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Warnings   []string `json:"warnings,omitempty"`
}

// ImportSpool replays product-owned hook files after collector downtime.
func ImportSpool(ctx context.Context, store *storage.Store, directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return imported, readErr
		}
		var payload map[string]any
		if json.Unmarshal(data, &payload) != nil {
			continue
		}
		sessionID, _ := payload["session_id"].(string)
		if sessionID == "" {
			continue
		}
		turnID, _ := payload["turn_id"].(string)
		kind, _ := payload["hook_event_name"].(string)
		if kind == "" {
			kind = "hook"
		}
		redacted := map[string]any{}
		for key, value := range payload {
			if key == "prompt" || key == "tool_input" || key == "tool_response" {
				redacted[key] = redact.Text(fmt.Sprint(value), 2_000)
			} else if key != "transcript_path" && key != "cwd" {
				redacted[key] = value
			}
		}
		event := domain.Event{
			ID:              redact.Hash(entry.Name() + string(data))[:24],
			SessionID:       sessionID,
			TurnID:          turnID,
			Timestamp:       time.Now().UTC(),
			EventType:       "hook",
			EventSubtype:    kind,
			Source:          "hook_spool",
			PayloadRedacted: redacted,
		}
		if err := store.AppendHookEvent(ctx, event); err != nil {
			return imported, err
		}
		if err := os.Remove(path); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

// ImportAll imports changed current and archived sessions under codexHome.
func ImportAll(ctx context.Context, store *storage.Store, codexHome string) (Result, error) {
	sources, err := discover.Sessions(codexHome)
	if err != nil {
		return Result{}, err
	}
	result := Result{Discovered: len(sources)}
	parser := transcript.New()
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		info, err := os.Stat(source.Path)
		if err != nil {
			result.Failed++
			result.Warnings = append(result.Warnings, err.Error())
			continue
		}
		quickFingerprint := sourceFingerprint(info.Size(), info.ModTime())
		pathHash := redact.Hash(source.Path)
		needed, err := store.ImportNeeded(ctx, pathHash, quickFingerprint)
		if err != nil {
			return result, err
		}
		if !needed {
			result.Skipped++
			continue
		}
		session, _, err := parser.ParseFile(source.Path, source.Archived)
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
		if err := store.SaveImport(ctx, pathHash, quickFingerprint, session); err != nil {
			return result, err
		}
		result.Imported++
	}
	return result, nil
}

func sourceFingerprint(size int64, modified time.Time) string {
	return fmt.Sprintf("%s:%d:%d", domain.ParserVersion, size, modified.UnixNano())
}
