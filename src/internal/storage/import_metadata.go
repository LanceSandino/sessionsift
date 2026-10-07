// @module ImportMetadata batches import fingerprints and updates enrichment without rewriting evidence.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// ImportRecord identifies the last successfully persisted source revision.
type ImportRecord struct {
	Fingerprint string
	SessionID   string
}

// ImportRecords reads the small manifest once per reconciliation pass. In
// Docker, a query per file causes hundreds of cross-filesystem SQLite locks.
func (s *Store) ImportRecords(ctx context.Context) (map[string]ImportRecord, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT path_hash,fingerprint,session_id FROM imports`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make(map[string]ImportRecord)
	for rows.Next() {
		var path string
		var record ImportRecord
		if err := rows.Scan(&path, &record.Fingerprint, &record.SessionID); err != nil {
			return nil, err
		}
		records[path] = record
	}
	return records, rows.Err()
}

// SaveImportMetadata updates supplemental fields and their fingerprint in one
// transaction, preserving all transcript-derived child records.
func (s *Store) SaveImportMetadata(ctx context.Context, pathHash, fingerprint string, session domain.Session) error {
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
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET title=?,model=?,payload_json=?,updated_at=? WHERE id=?`, session.Title, session.Model, string(payload), now, session.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("metadata session missing")
	}
	result, err = tx.ExecContext(ctx, `UPDATE imports SET fingerprint=?,imported_at=? WHERE path_hash=? AND session_id=?`, fingerprint, now, pathHash, session.ID)
	if err != nil {
		return err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("metadata import missing")
	}
	return tx.Commit()
}
