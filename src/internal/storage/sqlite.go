// Package storage persists normalized SessionSift records in local SQLite.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	_ "modernc.org/sqlite"
)

// Store wraps the local SQLite database.
type Store struct{ db *sql.DB }

// Open creates or migrates the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close releases the SQLite connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, external_session_id TEXT NOT NULL, title TEXT NOT NULL, repo_root_hash TEXT, repo_display_name TEXT, model TEXT, started_at TEXT, ended_at TEXT, status TEXT, parser_version TEXT, archived INTEGER NOT NULL DEFAULT 0, payload_json TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS turns (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, sequence INTEGER NOT NULL, payload_json TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events (id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id TEXT, timestamp TEXT, event_type TEXT NOT NULL, source TEXT NOT NULL, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id))`,
		`CREATE TABLE IF NOT EXISTS tool_calls (id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id TEXT, timestamp TEXT, tool_name TEXT, tool_category TEXT, success INTEGER, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id))`,
		`CREATE TABLE IF NOT EXISTS findings (id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id TEXT, finding_type TEXT, severity TEXT, confidence TEXT, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id))`,
		`CREATE TABLE IF NOT EXISTS imports (path_hash TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, session_id TEXT NOT NULL, imported_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session_time ON events(session_id,timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_tools_session_time ON tool_calls(session_id,timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_findings_session ON findings(session_id,finding_type)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return nil
}

// ImportNeeded checks the stored fingerprint for an input path identity.
func (s *Store) ImportNeeded(ctx context.Context, pathHash, fingerprint string) (bool, error) {
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT fingerprint FROM imports WHERE path_hash=?`, pathHash).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return existing != fingerprint, nil
}

// SaveImport atomically replaces one session and every derived child record.
func (s *Store) SaveImport(ctx context.Context, pathHash, fingerprint string, session domain.Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,external_session_id,title,repo_root_hash,repo_display_name,model,started_at,ended_at,status,parser_version,archived,payload_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,repo_root_hash=excluded.repo_root_hash,repo_display_name=excluded.repo_display_name,model=excluded.model,started_at=excluded.started_at,ended_at=excluded.ended_at,status=excluded.status,parser_version=excluded.parser_version,archived=excluded.archived,payload_json=excluded.payload_json,updated_at=excluded.updated_at`, session.ID, session.ExternalSessionID, session.Title, session.RepoRootHash, session.RepoDisplayName, session.Model, formatTime(session.StartedAt), formatTime(session.EndedAt), session.Status, session.ParserVersion, session.Archived, string(payload), now)
	if err != nil {
		return err
	}
	for _, table := range []string{"turns", "events", "tool_calls", "findings"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE session_id=?`, session.ID); err != nil {
			return err
		}
	}
	for _, turn := range session.Turns {
		encoded, _ := json.Marshal(turn)
		if _, err = tx.ExecContext(ctx, `INSERT INTO turns(id,session_id,sequence,payload_json) VALUES(?,?,?,?)`, turn.ID, session.ID, turn.Sequence, string(encoded)); err != nil {
			return err
		}
	}
	for _, event := range session.Events {
		encoded, _ := json.Marshal(event)
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,session_id,turn_id,timestamp,event_type,source,payload_json) VALUES(?,?,?,?,?,?,?)`, event.ID, session.ID, event.TurnID, formatTime(event.Timestamp), event.EventType, event.Source, string(encoded)); err != nil {
			return err
		}
	}
	for _, call := range session.ToolCalls {
		encoded, _ := json.Marshal(call)
		if _, err = tx.ExecContext(ctx, `INSERT INTO tool_calls(id,session_id,turn_id,timestamp,tool_name,tool_category,success,payload_json) VALUES(?,?,?,?,?,?,?,?)`, call.ID, session.ID, call.TurnID, formatTime(call.Timestamp), call.ToolName, call.ToolCategory, call.Success, string(encoded)); err != nil {
			return err
		}
	}
	for _, finding := range session.Findings {
		encoded, _ := json.Marshal(finding)
		if _, err = tx.ExecContext(ctx, `INSERT INTO findings(id,session_id,turn_id,finding_type,severity,confidence,payload_json) VALUES(?,?,?,?,?,?,?)`, finding.ID, session.ID, finding.TurnID, finding.FindingType, finding.Severity, finding.Confidence, string(encoded)); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO imports(path_hash,fingerprint,session_id,imported_at) VALUES(?,?,?,?) ON CONFLICT(path_hash) DO UPDATE SET fingerprint=excluded.fingerprint,session_id=excluded.session_id,imported_at=excluded.imported_at`, pathHash, fingerprint, session.ID, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ListSessions returns newest sessions first with child records intact.
func (s *Store) ListSessions(ctx context.Context) ([]domain.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []domain.Session
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var session domain.Session
		if err := json.Unmarshal([]byte(payload), &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// GetSession returns one fully normalized session.
func (s *Store) GetSession(ctx context.Context, id string) (domain.Session, error) {
	var payload string
	if err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM sessions WHERE id=?`, id).Scan(&payload); err != nil {
		return domain.Session{}, err
	}
	var session domain.Session
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

// AppendHookEvent stores a redacted live hook event even before transcript reconciliation.
func (s *Store) AppendHookEvent(ctx context.Context, event domain.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var session domain.Session
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM sessions WHERE id=?`, event.SessionID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		session = domain.Session{ID: event.SessionID, ExternalSessionID: event.SessionID, Title: "Live Codex session", Kind: domain.DeveloperSession, StartedAt: event.Timestamp, EndedAt: event.Timestamp, Status: "live", ParserVersion: domain.ParserVersion}
	} else if err != nil {
		return err
	} else if err = json.Unmarshal([]byte(existing), &session); err != nil {
		return err
	}
	if !containsEvent(session.Events, event.ID) {
		session.Events = append(session.Events, event)
	}
	if session.StartedAt.IsZero() || event.Timestamp.Before(session.StartedAt) {
		session.StartedAt = event.Timestamp
	}
	if event.Timestamp.After(session.EndedAt) {
		session.EndedAt = event.Timestamp
	}
	if event.EventSubtype == "SessionEnd" {
		session.Status = "completed"
	} else if session.Status == "" {
		session.Status = "live"
	}
	if model, ok := event.PayloadRedacted["model"].(string); ok && model != "" {
		session.Model = model
	}
	if event.EventSubtype == "UserPromptSubmit" {
		if prompt, ok := event.PayloadRedacted["prompt"].(string); ok && prompt != "" && session.Title == "Live Codex session" {
			session.Title = boundedTitle(prompt)
		}
	}
	applyOTelUsage(&session, event)
	sessionPayload, err := json.Marshal(session)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,external_session_id,title,model,started_at,ended_at,status,parser_version,archived,payload_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,model=excluded.model,started_at=excluded.started_at,ended_at=excluded.ended_at,status=excluded.status,parser_version=excluded.parser_version,payload_json=excluded.payload_json,updated_at=excluded.updated_at`, session.ID, session.ExternalSessionID, session.Title, session.Model, formatTime(session.StartedAt), formatTime(session.EndedAt), session.Status, session.ParserVersion, false, string(sessionPayload), now)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO events(id,session_id,turn_id,timestamp,event_type,source,payload_json) VALUES(?,?,?,?,?,?,?)`, event.ID, event.SessionID, event.TurnID, formatTime(event.Timestamp), event.EventType, event.Source, string(payload)); err != nil {
		return err
	}
	return tx.Commit()
}

func applyOTelUsage(session *domain.Session, event domain.Event) {
	if event.Source != "otel" || event.TurnID == "" {
		return
	}
	var turn *domain.Turn
	for index := range session.Turns {
		if session.Turns[index].ID == event.TurnID || session.Turns[index].ExternalTurnID == event.TurnID {
			turn = &session.Turns[index]
			break
		}
	}
	if turn == nil {
		session.Turns = append(session.Turns, domain.Turn{ID: event.TurnID, SessionID: session.ID, ExternalTurnID: event.TurnID, Sequence: len(session.Turns) + 1, StartedAt: event.Timestamp})
		turn = &session.Turns[len(session.Turns)-1]
	}
	if value, ok := metricInt(event.PayloadRedacted, "input_tokens", "input_token_count"); ok {
		turn.InputTokens = value
		turn.TokenSource = "otel"
	}
	if value, ok := metricInt(event.PayloadRedacted, "cached_input_tokens", "cached_input_token_count"); ok {
		turn.CachedInputTokens = value
		turn.TokenSource = "otel"
	}
	if value, ok := metricInt(event.PayloadRedacted, "output_tokens", "output_token_count"); ok {
		turn.OutputTokens = value
		turn.TokenSource = "otel"
	}
	if value, ok := metricInt(event.PayloadRedacted, "reasoning_tokens", "reasoning_output_tokens"); ok {
		turn.ReasoningTokens = value
		turn.TokenSource = "otel"
	}
	if event.Timestamp.After(turn.EndedAt) {
		turn.EndedAt = event.Timestamp
	}
}

func metricInt(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		switch value := values[key].(type) {
		case int64:
			return value, true
		case float64:
			return int64(value), true
		case json.Number:
			parsed, err := value.Int64()
			return parsed, err == nil
		}
	}
	return 0, false
}

func containsEvent(events []domain.Event, id string) bool {
	for _, event := range events {
		if event.ID == id {
			return true
		}
	}
	return false
}

func boundedTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 80 {
		return value[:77] + "…"
	}
	return value
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
