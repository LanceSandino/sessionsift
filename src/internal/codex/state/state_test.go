// Package state tests read-only enrichment from Codex's local thread index.
package state

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLoadReadsRenamedTitleAndExecutionContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state_5.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY, name TEXT, title TEXT, source TEXT, thread_source TEXT,
		originator TEXT, sandbox_policy TEXT, approval_mode TEXT, git_branch TEXT,
		git_origin_url TEXT, cli_version TEXT, model TEXT, reasoning_effort TEXT,
		created_at_ms INTEGER, updated_at_ms INTEGER
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO threads VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"01a0bc96-f391-7802-be76-8adfbe64df54", "Sessionsift MVP", "First prompt", "vscode", "user",
		"Codex Desktop", `{"type":"managed","network":"restricted"}`, "on-request", "main",
		"git@github.com:LanceSandino/sessionsift.git", "0.155.0", "gpt-test", "high", 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := snapshot.Thread("01a0bc96-f391-7802-be76-8adfbe64df54")
	if !ok || metadata.Name != "Sessionsift MVP" || metadata.Originator != "Codex Desktop" || metadata.Surface != "vscode" {
		t.Fatalf("metadata=%+v ok=%v", metadata, ok)
	}
	if metadata.SandboxPolicy != "managed" || metadata.NetworkAccess != "restricted" || metadata.RuntimeLocation != "local" {
		t.Fatalf("execution context=%+v", metadata)
	}
	if metadata.Revision == "" {
		t.Fatal("expected state revision")
	}
	if got := IdentityFromPath("rollout-2026-09-19T22-13-24-01a0bc96-f391-7802-be76-8adfbe64df54.jsonl"); got != "01a0bc96-f391-7802-be76-8adfbe64df54" {
		t.Fatalf("identity=%q", got)
	}
}

func TestLoadMissingStateIsOptional(t *testing.T) {
	snapshot, err := Load(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite"))
	if err != nil || snapshot.Len() != 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
