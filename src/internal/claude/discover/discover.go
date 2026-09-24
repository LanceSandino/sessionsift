// Package discover locates Claude Code transcript files.
// @module ClaudeDiscover finds main-session and subagent JSONL transcripts without reading credentials.
package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source identifies a transcript and whether it belongs to a delegated subagent.
type Source struct {
	Path     string
	Subagent bool
}

// Home returns CLAUDE_CONFIG_DIR or the standard ~/.claude location.
func Home() (string, error) {
	if configured := os.Getenv("CLAUDE_CONFIG_DIR"); configured != "" {
		return filepath.Clean(configured), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Sessions walks only <home>/projects, so credentials, settings, memory and
// every other sibling of the projects directory are never listed or opened.
// Symlinked directories are not followed and symlinked files are skipped.
func Sessions(claudeHome string) ([]Source, error) {
	base := filepath.Join(claudeHome, "projects")
	var sources []Source
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		subagent := isSubagent(base, path)
		// Workflow directories under subagents also hold journal.jsonl, a
		// run log keyed by agent id rather than a transcript; only
		// agent-<id>.jsonl files there are conversations.
		if subagent && !strings.HasPrefix(entry.Name(), "agent-") {
			return nil
		}
		sources = append(sources, Source{Path: path, Subagent: subagent})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, nil
}

// isSubagent reports whether a subagents directory lies between the project
// directory (the first element under projects) and the transcript file.
func isSubagent(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 || parts[0] == ".." {
		return false
	}
	for _, part := range parts[1 : len(parts)-1] {
		if part == "subagents" {
			return true
		}
	}
	return false
}
