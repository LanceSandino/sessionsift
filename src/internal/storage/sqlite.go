// Package storage persists normalized SessionSift records in local SQLite.
// @module SQLiteStore persists normalized sessions and serves isolated interactive reads.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	_ "modernc.org/sqlite"
)

// Store wraps the local SQLite database.
type Store struct {
	db               *sql.DB
	healthDB         *sql.DB
	readDB           *sql.DB
	healthMu         sync.Mutex
	healthCheckedAt  time.Time
	healthErr        error
	healthRefreshing bool
}

const healthCheckInterval = 5 * time.Minute

var quickCheckDatabase = func(ctx context.Context, db *sql.DB) error {
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("database quick check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("database quick check: %s", result)
	}
	return nil
}

// CaptureStats reports metadata-only ingestion activity for local diagnostics.
type CaptureStats struct {
	OTelEvents        int        `json:"otel_events"`
	LatestOTelEventAt *time.Time `json:"latest_otel_event_at,omitempty"`
	HookEvents        int        `json:"hook_events"`
	LatestHookEventAt *time.Time `json:"latest_hook_event_at,omitempty"`
	// Claude Code hooks are counted apart from Codex lifecycle hooks.
	ClaudeHookEvents        int        `json:"claude_hook_events"`
	LatestClaudeHookEventAt *time.Time `json:"latest_claude_hook_event_at,omitempty"`
}

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
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure database permissions: %w", err)
	}
	if path != ":memory:" {
		store.healthDB, err = sql.Open("sqlite", path)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("open readiness connection: %w", err)
		}
		store.healthDB.SetMaxOpenConns(1)
		healthCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err = quickCheckDatabase(healthCtx, store.healthDB)
		cancel()
		if err != nil {
			store.Close()
			return nil, err
		}
		store.healthCheckedAt = time.Now()
		store.readDB, err = sql.Open("sqlite", path)
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("open interactive read connection: %w", err)
		}
		store.readDB.SetMaxOpenConns(1)
		if _, err = store.readDB.Exec(`PRAGMA query_only=ON`); err != nil {
			store.Close()
			return nil, err
		}
	}
	return store, nil
}

// Close releases the SQLite connection.
func (s *Store) Close() error {
	if s.readDB != nil {
		_ = s.readDB.Close()
	}
	if s.healthDB != nil {
		_ = s.healthDB.Close()
	}
	return s.db.Close()
}

// Health verifies that the connection is reachable and the SQLite file passes
// its lightweight integrity check. It is suitable for readiness probes.
func (s *Store) Health(ctx context.Context) error {
	db := s.db
	if s.healthDB != nil {
		db = s.healthDB
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	s.healthMu.Lock()
	if time.Since(s.healthCheckedAt) >= healthCheckInterval && !s.healthRefreshing {
		s.healthRefreshing = true
		go s.refreshHealth(db)
	}
	err := s.healthErr
	s.healthMu.Unlock()
	return err
}

func (s *Store) refreshHealth(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	err := quickCheckDatabase(ctx, db)
	cancel()
	s.healthMu.Lock()
	s.healthCheckedAt = time.Now()
	s.healthErr = err
	s.healthRefreshing = false
	s.healthMu.Unlock()
}

// CaptureStats returns counts and latest timestamps without exposing event payloads.
func (s *Store) CaptureStats(ctx context.Context) (CaptureStats, error) {
	stats := CaptureStats{}
	if err := captureSourceStats(ctx, s.reader(), "otel", &stats.OTelEvents, &stats.LatestOTelEventAt); err != nil {
		return CaptureStats{}, err
	}
	// A hook replayed from the spool after the agent was down is still hook capture.
	if err := captureSourceStats(ctx, s.reader(), "hook", &stats.HookEvents, &stats.LatestHookEventAt); err != nil {
		return CaptureStats{}, err
	}
	if err := captureSourceStats(ctx, s.reader(), "claude_hook", &stats.ClaudeHookEvents, &stats.LatestClaudeHookEventAt); err != nil {
		return CaptureStats{}, err
	}
	return stats, nil
}

func captureSourceStats(ctx context.Context, db *sql.DB, source string, count *int, latest **time.Time) error {
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(timestamp), '') FROM events WHERE source IN (?, ?)`, source, source+"_spool").Scan(count, &raw); err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return fmt.Errorf("parse latest %s capture timestamp: %w", source, err)
	}
	*latest = &parsed
	return nil
}

// Backup creates a transactionally consistent SQLite snapshot without
// overwriting an existing file. The destination parent must already exist.
func (s *Store) Backup(ctx context.Context, destination string) error {
	destination, err := filepath.Abs(filepath.Clean(destination))
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(destination); statErr == nil {
		return fmt.Errorf("backup destination already exists: %s (%s)", destination, info.Mode())
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if info, statErr := os.Stat(filepath.Dir(destination)); statErr != nil || !info.IsDir() {
		if statErr != nil {
			return statErr
		}
		return fmt.Errorf("backup parent is not a directory: %s", filepath.Dir(destination))
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, destination); err != nil {
		return fmt.Errorf("database backup: %w", err)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return fmt.Errorf("secure backup permissions: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, external_session_id TEXT NOT NULL, title TEXT NOT NULL, repo_root_hash TEXT, repo_display_name TEXT, model TEXT, started_at TEXT, ended_at TEXT, status TEXT, parser_version TEXT, archived INTEGER NOT NULL DEFAULT 0, payload_json TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS turns (` + turnColumns + `)`,
		`CREATE TABLE IF NOT EXISTS events (id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id TEXT, timestamp TEXT, event_type TEXT NOT NULL, source TEXT NOT NULL, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id))`,
		`CREATE TABLE IF NOT EXISTS tool_calls (id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id TEXT, timestamp TEXT, tool_name TEXT, tool_category TEXT, success INTEGER, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id))`,
		`CREATE TABLE IF NOT EXISTS findings (id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, turn_id TEXT, finding_type TEXT, severity TEXT, confidence TEXT, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id))`,
		`CREATE TABLE IF NOT EXISTS imports (path_hash TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, session_id TEXT NOT NULL, imported_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS chatgpt_sources (source_hash TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, PRIMARY KEY(source_hash,session_id))`,
		`CREATE TABLE IF NOT EXISTS optimization_experiments (id TEXT PRIMARY KEY, title TEXT NOT NULL, hypothesis TEXT NOT NULL DEFAULT '', recommendation_id TEXT NOT NULL DEFAULT '', baseline_session_id TEXT NOT NULL, followup_session_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS optimization_recommendations (recommendation_id TEXT PRIMARY KEY, state TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS optimization_adoption_daily (day TEXT PRIMARY KEY, optimize_views INTEGER NOT NULL DEFAULT 0, experiments_created INTEGER NOT NULL DEFAULT 0, recommendations_changed INTEGER NOT NULL DEFAULT 0, reports_exported INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_optimization_experiments_updated ON optimization_experiments(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session_time ON events(session_id,timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_tools_session_time ON tool_calls(session_id,timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_findings_session ON findings(session_id,finding_type)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	if err := s.rekeyTurnsBySession(ctx); err != nil {
		return fmt.Errorf("migration: %w", err)
	}
	return nil
}

// turnColumns keys turns by session, like events and tool calls: provider turn
// IDs are only unique within one session.
const turnColumns = `id TEXT NOT NULL, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, sequence INTEGER NOT NULL, payload_json TEXT NOT NULL, PRIMARY KEY(session_id,id)`

// rekeyTurnsBySession rebuilds a turns table created with the earlier
// globally unique id key, keeping every row, in one transaction.
func (s *Store) rekeyTurnsBySession(ctx context.Context) error {
	var sessionKey int
	if err := s.db.QueryRowContext(ctx, `SELECT pk FROM pragma_table_info('turns') WHERE name='session_id'`).Scan(&sessionKey); err != nil {
		return err
	}
	if sessionKey != 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE turns_by_session (` + turnColumns + `)`,
		`INSERT INTO turns_by_session(id,session_id,sequence,payload_json) SELECT id,session_id,sequence,payload_json FROM turns`,
		`DROP TABLE turns`,
		`ALTER TABLE turns_by_session RENAME TO turns`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReconcileChatGPTSource is retained for callers of the original ChatGPT-only name.
func (s *Store) ReconcileChatGPTSource(ctx context.Context, sourceHash string, sessionIDs []string) error {
	return s.ReconcileExportSource(ctx, sourceHash, sessionIDs)
}

// ReconcileExportSource records the current membership of one user-supplied
// export (ChatGPT or Claude) and removes chats no longer present in any export.
func (s *Store) ReconcileExportSource(ctx context.Context, sourceHash string, sessionIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		seen[sessionID] = struct{}{}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO chatgpt_sources(source_hash,session_id) VALUES(?,?)`, sourceHash, sessionID); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT session_id FROM chatgpt_sources WHERE source_hash=?`, sourceHash)
	if err != nil {
		return err
	}
	var removed []string
	for rows.Next() {
		var sessionID string
		if err = rows.Scan(&sessionID); err != nil {
			rows.Close()
			return err
		}
		if _, ok := seen[sessionID]; !ok {
			removed = append(removed, sessionID)
		}
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, sessionID := range removed {
		if _, err = tx.ExecContext(ctx, `DELETE FROM chatgpt_sources WHERE source_hash=? AND session_id=?`, sourceHash, sessionID); err != nil {
			return err
		}
		var references int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM chatgpt_sources WHERE session_id=?`, sessionID).Scan(&references); err != nil {
			return err
		}
		if references == 0 {
			if _, err = tx.ExecContext(ctx, `DELETE FROM imports WHERE session_id=?`, sessionID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, sessionID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
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
	rows, err := s.reader().QueryContext(ctx, `SELECT payload_json FROM sessions ORDER BY started_at DESC`)
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
	if err := s.reader().QueryRowContext(ctx, `SELECT payload_json FROM sessions WHERE id=?`, id).Scan(&payload); err != nil {
		return domain.Session{}, err
	}
	var session domain.Session
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

// WAL readers see committed snapshots while ingestion owns the write connection.
func (s *Store) reader() *sql.DB {
	if s.readDB != nil {
		return s.readDB
	}
	return s.db
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
		session = liveSession(event)
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
		if prompt, ok := event.PayloadRedacted["prompt"].(string); ok && prompt != "" && session.Title == liveTitle(session.Source) {
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

// liveSession creates the placeholder a hook event attaches to until the
// authoritative transcript import replaces it.
func liveSession(event domain.Event) domain.Session {
	source, external, version := domain.SourceCodex, event.SessionID, domain.ParserVersion
	if strings.HasPrefix(event.SessionID, "claude:") {
		source, external, version = domain.SourceClaudeCode, strings.TrimPrefix(event.SessionID, "claude:"), domain.ClaudeCodeParserVersion
	}
	return domain.Session{ID: event.SessionID, ExternalSessionID: external, Title: liveTitle(source), TitleSource: "live", Kind: domain.DeveloperSession, Source: source, StartedAt: event.Timestamp, EndedAt: event.Timestamp, Status: "live", ParserVersion: version}
}

func liveTitle(source domain.SessionSource) string {
	if source == domain.SourceClaudeCode {
		return "Live Claude Code session"
	}
	return "Live Codex session"
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
	return redact.Title(value, 80)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
