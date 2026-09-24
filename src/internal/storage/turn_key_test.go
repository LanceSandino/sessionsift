package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// Regression anchor for CU-20260924-005: turn rows belong to a session, like
// events and tool calls, so two sessions may reuse one provider turn ID.

func turnPrimaryKey(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(turns)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	key := map[string]int{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var fallback sql.NullString
		if err = rows.Scan(&cid, &name, &kind, &notNull, &fallback, &pk); err != nil {
			t.Fatal(err)
		}
		if pk > 0 {
			key[name] = pk
		}
	}
	return key
}

func TestOpenRekeysLegacyTurnsBySessionAndKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, external_session_id TEXT NOT NULL, title TEXT NOT NULL, repo_root_hash TEXT, repo_display_name TEXT, model TEXT, started_at TEXT, ended_at TEXT, status TEXT, parser_version TEXT, archived INTEGER NOT NULL DEFAULT 0, payload_json TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE turns (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, sequence INTEGER NOT NULL, payload_json TEXT NOT NULL)`,
		`INSERT INTO sessions(id,external_session_id,title,payload_json,updated_at) VALUES('legacy','legacy','Legacy','{}','2026-01-01T00:00:00Z')`,
		`INSERT INTO turns(id,session_id,sequence,payload_json) VALUES('legacy-turn','legacy',1,'{"id":"legacy-turn"}')`,
	} {
		if _, err = legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	key := turnPrimaryKey(t, store.db)
	var sessionID, payload string
	rowErr := store.db.QueryRow(`SELECT session_id,payload_json FROM turns WHERE id='legacy-turn'`).Scan(&sessionID, &payload)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if key["session_id"] != 1 || key["id"] != 2 || len(key) != 2 {
		t.Fatalf("turns primary key=%v", key)
	}
	if rowErr != nil || sessionID != "legacy" || payload != `{"id":"legacy-turn"}` {
		t.Fatalf("legacy turn row session=%q payload=%q err=%v", sessionID, payload, rowErr)
	}

	// Reopening an already migrated database is a no-op.
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, id := range []string{"one", "two"} {
		session := domain.Session{ID: id, ExternalSessionID: id, Title: id, StartedAt: now, EndedAt: now,
			Turns: []domain.Turn{{ID: "t1", SessionID: id, ExternalTurnID: "t1", Sequence: 1, StartedAt: now}}}
		if err = store.SaveImport(context.Background(), "path-"+id, "fingerprint", session); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	var count int
	if err = store.db.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("turn rows=%d err=%v", count, err)
	}
}

func TestFreshDatabaseKeysTurnsBySession(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if key := turnPrimaryKey(t, store.db); key["session_id"] != 1 || key["id"] != 2 || len(key) != 2 {
		t.Fatalf("turns primary key=%v", key)
	}
}

// A rebuild that cannot complete rolls back: Open fails loudly and the legacy
// table and its rows are left exactly as they were.
func TestFailedTurnRekeyRollsBackAndKeepsLegacyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, external_session_id TEXT NOT NULL, title TEXT NOT NULL, repo_root_hash TEXT, repo_display_name TEXT, model TEXT, started_at TEXT, ended_at TEXT, status TEXT, parser_version TEXT, archived INTEGER NOT NULL DEFAULT 0, payload_json TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE turns (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, sequence INTEGER NOT NULL, payload_json TEXT NOT NULL)`,
		`CREATE TABLE turns_by_session (blocking INTEGER)`,
		`INSERT INTO sessions(id,external_session_id,title,payload_json,updated_at) VALUES('legacy','legacy','Legacy','{}','2026-01-01T00:00:00Z')`,
		`INSERT INTO turns(id,session_id,sequence,payload_json) VALUES('legacy-turn','legacy',1,'{}')`,
	} {
		if _, err = legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}

	if store, openErr := Open(path); openErr == nil {
		_ = store.Close()
		t.Fatal("expected the blocked rebuild to fail Open")
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var count int
	if err = check.QueryRow(`SELECT COUNT(*) FROM turns WHERE id='legacy-turn'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("legacy rows=%d err=%v", count, err)
	}
	if key := turnPrimaryKey(t, check); key["id"] != 1 || len(key) != 1 {
		t.Fatalf("legacy key changed: %v", key)
	}
}
