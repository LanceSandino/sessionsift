// Package state reads optional, user-visible Codex thread metadata without mutating Codex state.
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/LanceSandino/sessionsift/src/internal/redact"
	_ "modernc.org/sqlite"
)

// Metadata contains the stable, useful subset of one Codex thread-index row.
type Metadata struct {
	Name            string
	Originator      string
	Surface         string
	ThreadSource    string
	RuntimeLocation string
	ApprovalMode    string
	SandboxPolicy   string
	NetworkAccess   string
	GitBranch       string
	GitOrigin       string
	CLIVersion      string
	Model           string
	ReasoningEffort string
	CreatedAtMS     int64
	UpdatedAtMS     int64
	Revision        string
}

// Snapshot is an immutable in-memory view keyed by Codex thread id.
type Snapshot struct{ threads map[string]Metadata }

// Len reports how many thread rows were loaded.
func (s Snapshot) Len() int { return len(s.threads) }

// Thread returns metadata for a transcript identity.
func (s Snapshot) Thread(id string) (Metadata, bool) {
	value, ok := s.threads[id]
	return value, ok
}

// Load reads the Codex state database in read-only mode. A missing database is optional.
func Load(ctx context.Context, path string) (Snapshot, error) {
	snapshot := Snapshot{threads: map[string]Metadata{}}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	} else if err != nil {
		return snapshot, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return snapshot, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, `SELECT id,name,source,thread_source,originator,sandbox_policy,approval_mode,git_branch,git_origin_url,cli_version,model,reasoning_effort,created_at_ms,updated_at_ms FROM threads`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var values [11]sql.NullString
		var created, updated sql.NullInt64
		if err := rows.Scan(&id, &values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8], &values[9], &values[10], &created, &updated); err != nil {
			return snapshot, err
		}
		policy, network := executionPolicy(values[4].String)
		surface := surfaceName(values[1].String)
		metadata := Metadata{
			Name: bounded(values[0].String, 160), Surface: surface, ThreadSource: bounded(values[2].String, 80),
			Originator: bounded(values[3].String, 120), SandboxPolicy: policy, NetworkAccess: network,
			ApprovalMode: bounded(values[5].String, 80), GitBranch: bounded(values[6].String, 200),
			GitOrigin: bounded(values[7].String, 500), CLIVersion: bounded(values[8].String, 80),
			Model: bounded(values[9].String, 120), ReasoningEffort: bounded(values[10].String, 80),
			CreatedAtMS: created.Int64, UpdatedAtMS: updated.Int64,
		}
		metadata.RuntimeLocation = runtimeLocation(surface, metadata.Originator)
		metadata.Revision = redact.Hash(strings.Join([]string{metadata.Name, metadata.Originator, metadata.Surface, metadata.ThreadSource, metadata.ApprovalMode, metadata.SandboxPolicy, metadata.NetworkAccess, metadata.GitBranch, metadata.Model, metadata.ReasoningEffort, strconv.FormatInt(metadata.UpdatedAtMS, 10)}, "\x00"))
		snapshot.threads[id] = metadata
	}
	return snapshot, rows.Err()
}

func executionPolicy(raw string) (policy, network string) {
	var value struct {
		Type    string `json:"type"`
		Network string `json:"network"`
	}
	if json.Unmarshal([]byte(raw), &value) == nil {
		return bounded(value.Type, 80), bounded(value.Network, 80)
	}
	return "", ""
}

func surfaceName(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		return "subagent"
	}
	return bounded(raw, 80)
}

func runtimeLocation(surface, originator string) string {
	value := strings.ToLower(surface + " " + originator)
	if strings.Contains(value, "remote") || strings.Contains(value, "cloud") {
		return "remote"
	}
	if surface == "cli" || surface == "vscode" || strings.Contains(value, "desktop") || strings.Contains(value, "tui") {
		return "local"
	}
	return ""
}

func bounded(value string, limit int) string {
	value = strings.Join(strings.Fields(redact.Text(value, limit*2)), " ")
	if len(value) > limit {
		return value[:limit-3] + "…"
	}
	return value
}

var identityPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// IdentityFromPath extracts the transcript UUID used by the Codex thread index.
func IdentityFromPath(path string) string {
	return identityPattern.FindString(filepath.Base(path))
}
