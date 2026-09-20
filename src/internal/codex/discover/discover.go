// Package discover locates current and archived Codex transcript files.
package discover

import (
	"os"
	"path/filepath"
	"sort"
)

// Source identifies a transcript and whether Codex archived it.
type Source struct {
	Path     string
	Archived bool
}

// Home returns CODEX_HOME or the standard ~/.codex location.
func Home() (string, error) {
	if configured := os.Getenv("CODEX_HOME"); configured != "" {
		return filepath.Clean(configured), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// Sessions recursively discovers JSONL transcripts without touching auth state.
func Sessions(codexHome string) ([]Source, error) {
	var sources []Source
	for _, root := range []struct {
		name     string
		archived bool
	}{{"sessions", false}, {"archived_sessions", true}} {
		base := filepath.Join(codexHome, root.name)
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if !entry.IsDir() && filepath.Ext(path) == ".jsonl" {
				sources = append(sources, Source{Path: path, Archived: root.archived})
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, nil
}
