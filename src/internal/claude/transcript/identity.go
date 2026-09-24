// @module ClaudeTranscript/Identity derives session and agent ids from transcript paths, records and content.

package transcript

import (
	"path/filepath"
	"regexp"
	"strings"
)

var sessionUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// identityHints are the identifiers Claude Code encodes in a transcript's
// path. Each file is named for the session (or agent) it belongs to, while
// its records can all carry another session's id (a continued or forked
// conversation), so a well-formed path id wins over record ids: two files
// must never import as the same session and overwrite each other.
type identityHints struct {
	sessionID string
	agentID   string
}

// hintsFromPath reads <project>/<sessionId>.jsonl,
// <project>/<sessionId>/subagents/[...]/agent-<agentId>.jsonl and the legacy
// project-level agent-<agentId>.jsonl layouts. Anything else yields no hint.
func hintsFromPath(path string) identityHints {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var hints identityHints
	if agent, ok := strings.CutPrefix(stem, "agent-"); ok && agent != "" {
		hints.agentID = agent
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for index := len(parts) - 2; index >= 1; index-- {
		if parts[index] == "subagents" {
			if sessionUUID.MatchString(parts[index-1]) {
				hints.sessionID = parts[index-1]
			}
			return hints
		}
	}
	if sessionUUID.MatchString(stem) {
		hints.sessionID = stem
	}
	return hints
}

// fallbackID names a transcript that states no id and has no path hint by
// its content digest, so two different files can never share an identity.
func fallbackID(prefix, digest string) string {
	return prefix + "-" + digest[:16]
}
