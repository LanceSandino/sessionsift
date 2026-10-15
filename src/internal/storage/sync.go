// @module LocalSyncState persists encrypted offline work, remote replicas, conflicts, tombstones, and high-water state.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"
)

var ErrSyncRollback = errors.New("sync cursor rollback detected")

type SyncOutboxItem struct {
	ObjectID         string    `json:"object_id"`
	Revision         uint64    `json:"revision"`
	ExpectedRevision uint64    `json:"expected_revision"`
	EnvelopeJSON     []byte    `json:"envelope_json"`
	Attempts         int       `json:"attempts"`
	NextAttemptAt    time.Time `json:"next_attempt_at"`
	LastError        string    `json:"last_error,omitempty"`
}

type SyncReplica struct {
	ObjectID       string          `json:"object_id"`
	SourceDeviceID string          `json:"source_device_id"`
	Kind           string          `json:"kind"`
	Epoch          uint64          `json:"epoch"`
	Revision       uint64          `json:"revision"`
	Digest         string          `json:"digest"`
	PayloadJSON    json.RawMessage `json:"payload"`
	RemoteSource   bool            `json:"remote_source"`
	LocallyEdited  bool            `json:"locally_edited"`
	Deleted        bool            `json:"deleted"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type SyncApplyResult struct {
	Conflict bool `json:"conflict"`
}

type SyncConflict struct {
	ID             int64           `json:"id"`
	ObjectID       string          `json:"object_id"`
	LocalJSON      json.RawMessage `json:"local"`
	RemoteJSON     json.RawMessage `json:"remote"`
	RemoteRevision uint64          `json:"remote_revision"`
	CreatedAt      time.Time       `json:"created_at"`
}

type SyncSettings struct {
	Enabled          bool   `json:"enabled"`
	Paused           bool   `json:"paused"`
	RotationPending  bool   `json:"rotation_pending"`
	Summaries        bool   `json:"summaries"`
	FindingsNotes    bool   `json:"findings_notes"`
	DetailedEvidence bool   `json:"detailed_evidence"`
	KeyEpoch         uint64 `json:"key_epoch"`
}

type SyncRuntimeState struct {
	HighWaterCursor int64      `json:"high_water_cursor"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	UsageBytes      int64      `json:"usage_bytes"`
	QuotaBytes      int64      `json:"quota_bytes"`
}

func (s *Store) migrateSync(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sync_outbox (
			object_id TEXT NOT NULL, revision INTEGER NOT NULL, expected_revision INTEGER NOT NULL,
			envelope_json BLOB NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL, last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, PRIMARY KEY(object_id,revision)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_outbox_due ON sync_outbox(next_attempt_at,created_at)`,
		`CREATE TABLE IF NOT EXISTS sync_replicas (
			object_id TEXT PRIMARY KEY, source_device_id TEXT NOT NULL, kind TEXT NOT NULL,
			epoch INTEGER NOT NULL, revision INTEGER NOT NULL, digest TEXT NOT NULL,
			payload_json TEXT NOT NULL, base_json TEXT NOT NULL DEFAULT '{}', remote_source INTEGER NOT NULL DEFAULT 1,
			locally_edited INTEGER NOT NULL DEFAULT 0, deleted INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sync_conflicts (
			id INTEGER PRIMARY KEY AUTOINCREMENT, object_id TEXT NOT NULL,
			local_json TEXT NOT NULL, remote_json TEXT NOT NULL, remote_revision INTEGER NOT NULL,
			created_at TEXT NOT NULL, resolved_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_conflicts_open ON sync_conflicts(resolved_at,created_at)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			id INTEGER PRIMARY KEY CHECK(id=1), high_water_cursor INTEGER NOT NULL DEFAULT 0,
			last_sync_at TEXT, last_error TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO sync_state(id,high_water_cursor) VALUES(1,0) ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS sync_settings (
			id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0,
			paused INTEGER NOT NULL DEFAULT 0, summaries INTEGER NOT NULL DEFAULT 1,
			findings_notes INTEGER NOT NULL DEFAULT 1, detailed_evidence INTEGER NOT NULL DEFAULT 0,
			key_epoch INTEGER NOT NULL DEFAULT 1, rotation_pending INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO sync_settings(id) VALUES(1) ON CONFLICT(id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS sync_local_heads (
			object_id TEXT PRIMARY KEY, content_digest TEXT NOT NULL, revision INTEGER NOT NULL,
			wire_digest TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("local sync migration: %w", err)
		}
	}
	for _, column := range []struct{ name, definition string }{
		{"key_epoch", "INTEGER NOT NULL DEFAULT 1"},
		{"rotation_pending", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureLocalSyncColumn(ctx, s.db, "sync_settings", column.name, column.definition); err != nil {
			return err
		}
	}
	if err := ensureLocalSyncColumn(ctx, s.db, "sync_replicas", "base_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sync_replicas SET base_json=payload_json WHERE base_json='{}'`)
	if err := ensureLocalSyncColumn(ctx, s.db, "sync_local_heads", "wire_digest", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"usage_bytes", "INTEGER NOT NULL DEFAULT 0"}, {"quota_bytes", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureLocalSyncColumn(ctx, s.db, "sync_state", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func ensureLocalSyncColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		found = found || name == column
	}
	err = rows.Close()
	if err != nil || found {
		return err
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func (s *Store) SyncSettings(ctx context.Context) (SyncSettings, error) {
	var value SyncSettings
	err := s.reader().QueryRowContext(ctx, `SELECT enabled,paused,rotation_pending,summaries,findings_notes,detailed_evidence,key_epoch FROM sync_settings WHERE id=1`).
		Scan(&value.Enabled, &value.Paused, &value.RotationPending, &value.Summaries, &value.FindingsNotes, &value.DetailedEvidence, &value.KeyEpoch)
	return value, err
}

func (s *Store) SaveSyncSettings(ctx context.Context, value SyncSettings) error {
	if !value.Summaries && !value.FindingsNotes && !value.DetailedEvidence {
		return errors.New("select at least one sync category")
	}
	if value.KeyEpoch == 0 {
		value.KeyEpoch = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sync_settings SET enabled=?,paused=?,rotation_pending=?,summaries=?,findings_notes=?,detailed_evidence=?,key_epoch=? WHERE id=1`, value.Enabled, value.Paused, value.RotationPending, value.Summaries, value.FindingsNotes, value.DetailedEvidence, value.KeyEpoch)
	return err
}

func (s *Store) PrepareSyncRevision(ctx context.Context, objectID, contentDigest string) (revision, expected uint64, parentDigest string, changed bool, err error) {
	if objectID == "" || contentDigest == "" {
		return 0, 0, "", false, errors.New("invalid local sync head")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, "", false, err
	}
	defer tx.Rollback()
	var currentDigest string
	var currentRevision uint64
	err = tx.QueryRowContext(ctx, `SELECT content_digest,revision,wire_digest FROM sync_local_heads WHERE object_id=?`, objectID).Scan(&currentDigest, &currentRevision, &parentDigest)
	if errors.Is(err, sql.ErrNoRows) {
		revision, expected, changed = 1, 0, true
	} else if err != nil {
		return 0, 0, "", false, err
	} else if currentDigest == contentDigest {
		return currentRevision, currentRevision - 1, parentDigest, false, tx.Commit()
	} else {
		revision, expected, changed = currentRevision+1, currentRevision, true
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_local_heads(object_id,content_digest,revision,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(object_id) DO UPDATE SET content_digest=excluded.content_digest,revision=excluded.revision,updated_at=excluded.updated_at`,
		objectID, contentDigest, revision, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, 0, "", false, err
	}
	return revision, expected, parentDigest, changed, tx.Commit()
}

func (s *Store) RecordSyncHead(ctx context.Context, objectID string, revision uint64, wireDigest string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sync_local_heads SET wire_digest=?,updated_at=? WHERE object_id=? AND revision=?`, wireDigest, time.Now().UTC().Format(time.RFC3339Nano), objectID, revision)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count > 1 {
		return errors.New("sync local head update was not unique")
	}
	return nil
}

func (s *Store) PendingSyncCount(ctx context.Context) (int, error) {
	var count int
	err := s.reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_outbox`).Scan(&count)
	return count, err
}

func (s *Store) EnqueueSync(ctx context.Context, item SyncOutboxItem) error {
	if item.ObjectID == "" || item.Revision == 0 || len(item.EnvelopeJSON) == 0 || !json.Valid(item.EnvelopeJSON) {
		return errors.New("invalid encrypted sync outbox item")
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_outbox(object_id,revision,expected_revision,envelope_json,next_attempt_at,created_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(object_id,revision) DO NOTHING`,
		item.ObjectID, item.Revision, item.ExpectedRevision, item.EnvelopeJSON, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) PendingSync(ctx context.Context, now time.Time, limit int) ([]SyncOutboxItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT object_id,revision,expected_revision,envelope_json,attempts,next_attempt_at,last_error FROM sync_outbox WHERE next_attempt_at<=? ORDER BY created_at LIMIT ?`, now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SyncOutboxItem{}
	for rows.Next() {
		var item SyncOutboxItem
		var next string
		if err = rows.Scan(&item.ObjectID, &item.Revision, &item.ExpectedRevision, &item.EnvelopeJSON, &item.Attempts, &next, &item.LastError); err != nil {
			return nil, err
		}
		item.NextAttemptAt, _ = time.Parse(time.RFC3339Nano, next)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeferSync(ctx context.Context, objectID string, revision uint64, next time.Time, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sync_outbox SET attempts=attempts+1,next_attempt_at=?,last_error=? WHERE object_id=? AND revision=?`, next.UTC().Format(time.RFC3339Nano), message, objectID, revision)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("sync outbox item not found")
	}
	return nil
}

func (s *Store) CompleteSync(ctx context.Context, objectID string, revision uint64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_outbox WHERE object_id=? AND revision=?`, objectID, revision)
	return err
}

func (s *Store) ApplySyncReplica(ctx context.Context, replica SyncReplica) (SyncApplyResult, error) {
	if replica.ObjectID == "" || replica.SourceDeviceID == "" || replica.Kind == "" || replica.Epoch == 0 || replica.Revision == 0 || replica.Digest == "" || !json.Valid(replica.PayloadJSON) {
		return SyncApplyResult{}, errors.New("invalid sync replica")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SyncApplyResult{}, err
	}
	defer tx.Rollback()
	var currentJSON, baseJSON string
	var currentRevision uint64
	var currentDigest string
	var locallyEdited bool
	err = tx.QueryRowContext(ctx, `SELECT payload_json,base_json,revision,locally_edited,digest FROM sync_replicas WHERE object_id=?`, replica.ObjectID).Scan(&currentJSON, &baseJSON, &currentRevision, &locallyEdited, &currentDigest)
	if err == nil {
		if replica.Revision < currentRevision || (replica.Revision == currentRevision && replica.Digest != currentDigest) {
			return SyncApplyResult{}, ErrSyncRollback
		}
		if replica.Revision == currentRevision {
			return SyncApplyResult{}, tx.Commit()
		}
		if locallyEdited {
			merged, clean := mergeMutableJSON(json.RawMessage(baseJSON), json.RawMessage(currentJSON), replica.PayloadJSON)
			if clean {
				stillEdited := !jsonBodyEqual(merged, replica.PayloadJSON)
				now := time.Now().UTC().Format(time.RFC3339Nano)
				_, err = tx.ExecContext(ctx, `UPDATE sync_replicas SET source_device_id=?,kind=?,epoch=?,revision=?,digest=?,payload_json=?,base_json=?,remote_source=1,locally_edited=?,deleted=?,updated_at=? WHERE object_id=?`,
					replica.SourceDeviceID, replica.Kind, replica.Epoch, replica.Revision, replica.Digest, string(merged), string(replica.PayloadJSON), stillEdited, replica.Deleted, now, replica.ObjectID)
				if err != nil {
					return SyncApplyResult{}, err
				}
				return SyncApplyResult{}, tx.Commit()
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err = tx.ExecContext(ctx, `UPDATE sync_replicas SET source_device_id=?,kind=?,epoch=?,revision=?,digest=?,base_json=?,remote_source=1,locally_edited=1,deleted=?,updated_at=? WHERE object_id=?`,
				replica.SourceDeviceID, replica.Kind, replica.Epoch, replica.Revision, replica.Digest, string(replica.PayloadJSON), replica.Deleted, now, replica.ObjectID); err != nil {
				return SyncApplyResult{}, err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO sync_conflicts(object_id,local_json,remote_json,remote_revision,created_at) VALUES(?,?,?,?,?)`, replica.ObjectID, currentJSON, string(replica.PayloadJSON), replica.Revision, now)
			if err != nil {
				return SyncApplyResult{}, err
			}
			return SyncApplyResult{Conflict: true}, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SyncApplyResult{}, err
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_replicas(object_id,source_device_id,kind,epoch,revision,digest,payload_json,base_json,remote_source,locally_edited,deleted,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(object_id) DO UPDATE SET source_device_id=excluded.source_device_id,kind=excluded.kind,epoch=excluded.epoch,revision=excluded.revision,digest=excluded.digest,payload_json=excluded.payload_json,base_json=excluded.payload_json,remote_source=1,locally_edited=0,deleted=excluded.deleted,updated_at=excluded.updated_at`,
		replica.ObjectID, replica.SourceDeviceID, replica.Kind, replica.Epoch, replica.Revision, replica.Digest, string(replica.PayloadJSON), string(replica.PayloadJSON), true, false, replica.Deleted, now.Format(time.RFC3339Nano))
	if err != nil {
		return SyncApplyResult{}, err
	}
	return SyncApplyResult{}, tx.Commit()
}

func (s *Store) MarkSyncReplicaEdited(ctx context.Context, objectID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sync_replicas SET base_json=payload_json,locally_edited=1 WHERE object_id=?`, objectID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("sync replica not found")
	}
	return nil
}

func (s *Store) EditSyncReplica(ctx context.Context, objectID string, payload json.RawMessage) error {
	if !json.Valid(payload) {
		return errors.New("invalid local sync edit")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sync_replicas SET base_json=CASE WHEN locally_edited=0 THEN payload_json ELSE base_json END,payload_json=?,locally_edited=1,updated_at=? WHERE object_id=? AND deleted=0`,
		string(payload), time.Now().UTC().Format(time.RFC3339Nano), objectID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.New("sync replica not found")
	}
	return nil
}

func (s *Store) ListSyncReplicas(ctx context.Context) ([]SyncReplica, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT object_id,source_device_id,kind,epoch,revision,digest,payload_json,remote_source,locally_edited,deleted,updated_at FROM sync_replicas ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []SyncReplica{}
	for rows.Next() {
		var value SyncReplica
		var payload, updated string
		if err = rows.Scan(&value.ObjectID, &value.SourceDeviceID, &value.Kind, &value.Epoch, &value.Revision, &value.Digest, &payload, &value.RemoteSource, &value.LocallyEdited, &value.Deleted, &updated); err != nil {
			return nil, err
		}
		value.PayloadJSON = json.RawMessage(payload)
		value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SyncConflicts(ctx context.Context) ([]SyncConflict, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT id,object_id,local_json,remote_json,remote_revision,created_at FROM sync_conflicts WHERE resolved_at IS NULL ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []SyncConflict{}
	for rows.Next() {
		var value SyncConflict
		var local, remote, created string
		if err = rows.Scan(&value.ID, &value.ObjectID, &local, &remote, &value.RemoteRevision, &created); err != nil {
			return nil, err
		}
		value.LocalJSON, value.RemoteJSON = json.RawMessage(local), json.RawMessage(remote)
		value.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) SyncCursor(ctx context.Context) (int64, error) {
	var cursor int64
	err := s.reader().QueryRowContext(ctx, `SELECT high_water_cursor FROM sync_state WHERE id=1`).Scan(&cursor)
	return cursor, err
}

func (s *Store) SetSyncCursor(ctx context.Context, cursor int64) error {
	if cursor < 0 {
		return ErrSyncRollback
	}
	current, err := s.SyncCursor(ctx)
	if err != nil {
		return err
	}
	if cursor < current {
		return ErrSyncRollback
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sync_state SET high_water_cursor=?,last_sync_at=? WHERE id=1`, cursor, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) SyncRuntimeState(ctx context.Context) (SyncRuntimeState, error) {
	var value SyncRuntimeState
	var lastSync sql.NullString
	err := s.reader().QueryRowContext(ctx, `SELECT high_water_cursor,last_sync_at,last_error,usage_bytes,quota_bytes FROM sync_state WHERE id=1`).
		Scan(&value.HighWaterCursor, &lastSync, &value.LastError, &value.UsageBytes, &value.QuotaBytes)
	if err == nil && lastSync.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, lastSync.String)
		if parseErr == nil {
			value.LastSyncAt = &parsed
		}
	}
	return value, err
}

func (s *Store) RecordSyncRun(ctx context.Context, usageBytes, quotaBytes int64, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
		if len(message) > 500 {
			message = message[:500]
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sync_state SET last_sync_at=?,last_error=?,usage_bytes=?,quota_bytes=? WHERE id=1`,
		time.Now().UTC().Format(time.RFC3339Nano), message, usageBytes, quotaBytes)
	return err
}

// ResetSyncReplicaState removes only SessionSift's derived sync queue and replicas.
// Locally imported provider sessions and source transcripts are intentionally untouched.
func (s *Store) ResetSyncReplicaState(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM sync_outbox`, `DELETE FROM sync_replicas`, `DELETE FROM sync_conflicts`, `DELETE FROM sync_local_heads`,
		`UPDATE sync_state SET high_water_cursor=0,last_sync_at=NULL,last_error='',usage_bytes=0,quota_bytes=0 WHERE id=1`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ResolveSyncConflict(ctx context.Context, conflictID int64, useRemote bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var objectID, remoteJSON string
	var remoteRevision uint64
	err = tx.QueryRowContext(ctx, `SELECT object_id,remote_json,remote_revision FROM sync_conflicts WHERE id=? AND resolved_at IS NULL`, conflictID).
		Scan(&objectID, &remoteJSON, &remoteRevision)
	if err != nil {
		return err
	}
	if useRemote {
		if _, err = tx.ExecContext(ctx, `UPDATE sync_replicas SET payload_json=?,base_json=?,revision=?,locally_edited=0,updated_at=? WHERE object_id=?`, remoteJSON, remoteJSON, remoteRevision, time.Now().UTC().Format(time.RFC3339Nano), objectID); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sync_conflicts SET resolved_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), conflictID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func mergeMutableJSON(baseBody, localBody, remoteBody json.RawMessage) (json.RawMessage, bool) {
	var base, local, remote map[string]any
	if json.Unmarshal(baseBody, &base) != nil || json.Unmarshal(localBody, &local) != nil || json.Unmarshal(remoteBody, &remote) != nil || base == nil || local == nil || remote == nil {
		return nil, false
	}
	keys := make([]string, 0, len(base)+len(local)+len(remote))
	seen := map[string]bool{}
	for _, values := range []map[string]any{base, local, remote} {
		for key := range values {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	merged := map[string]any{}
	for _, key := range keys {
		baseValue, baseOK := base[key]
		localValue, localOK := local[key]
		remoteValue, remoteOK := remote[key]
		switch {
		case jsonFieldEqual(localValue, localOK, remoteValue, remoteOK):
			if localOK {
				merged[key] = localValue
			}
		case jsonFieldEqual(localValue, localOK, baseValue, baseOK):
			if remoteOK {
				merged[key] = remoteValue
			}
		case jsonFieldEqual(remoteValue, remoteOK, baseValue, baseOK):
			if localOK {
				merged[key] = localValue
			}
		default:
			return nil, false
		}
	}
	encoded, err := json.Marshal(merged)
	return encoded, err == nil
}

func jsonFieldEqual(left any, leftOK bool, right any, rightOK bool) bool {
	return leftOK == rightOK && (!leftOK || reflect.DeepEqual(left, right))
}

func jsonBodyEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && reflect.DeepEqual(leftValue, rightValue)
}
