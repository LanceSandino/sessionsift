// @module SyncStore persists opaque client ciphertext, CAS revisions, tombstones, and a bounded delta cursor.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrSyncConflict = errors.New("sync revision conflict")
	ErrSyncNotFound = errors.New("sync object not found")
	ErrSyncQuota    = errors.New("sync storage quota exceeded")
	ErrSyncDisk     = errors.New("sync storage paused because bridge disk space is low")
)

type SyncLimits struct {
	MaxObjectBytes    int64
	MaxWorkspaceBytes int64
	MaxPageSize       int
	MinFreeBytes      int64
}

// MaxSyncPageSize is the hard ceiling for one change-feed page. An operator
// may lower the page size, but a request limit sizes both an allocation and a
// SQL LIMIT, so no configuration may raise it past this bound.
const MaxSyncPageSize = 1000

var defaultSyncLimits = SyncLimits{MaxObjectBytes: 2 << 20, MaxWorkspaceBytes: 512 << 20, MaxPageSize: 200, MinFreeBytes: 256 << 20}

var syncAvailableBytes = func(path string) (uint64, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(path), &stats); err != nil {
		return 0, err
	}
	return uint64(stats.Bavail) * uint64(stats.Bsize), nil
}

type SyncObject struct {
	WorkspaceID    string    `json:"workspace_id"`
	ObjectID       string    `json:"object_id"`
	SourceDeviceID string    `json:"source_device_id"`
	WriterDeviceID string    `json:"writer_device_id"`
	Kind           string    `json:"kind"`
	Epoch          uint64    `json:"epoch"`
	Revision       uint64    `json:"revision"`
	ParentDigest   string    `json:"parent_digest,omitempty"`
	Nonce          []byte    `json:"nonce,omitempty"`
	Ciphertext     []byte    `json:"ciphertext,omitempty"`
	Digest         string    `json:"digest"`
	Signature      []byte    `json:"signature,omitempty"`
	Deleted        bool      `json:"deleted,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SyncChange struct {
	Cursor         int64     `json:"cursor"`
	WorkspaceID    string    `json:"workspace_id"`
	ObjectID       string    `json:"object_id"`
	SourceDeviceID string    `json:"source_device_id"`
	WriterDeviceID string    `json:"writer_device_id"`
	Kind           string    `json:"kind"`
	Epoch          uint64    `json:"epoch"`
	Revision       uint64    `json:"revision"`
	Digest         string    `json:"digest"`
	Deleted        bool      `json:"deleted"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SyncUsage struct {
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
	Limit   int64 `json:"limit"`
}

func (s *Store) migrateSync(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS sync_objects (
			workspace_id TEXT NOT NULL REFERENCES workspaces(id), object_id TEXT NOT NULL,
			source_device_id TEXT NOT NULL REFERENCES devices(id), writer_device_id TEXT NOT NULL REFERENCES devices(id), kind TEXT NOT NULL,
			epoch INTEGER NOT NULL, revision INTEGER NOT NULL, parent_digest TEXT NOT NULL,
			nonce BLOB NOT NULL, ciphertext BLOB NOT NULL, digest TEXT NOT NULL,
			signature BLOB NOT NULL, deleted INTEGER NOT NULL, updated_at TEXT NOT NULL,
			PRIMARY KEY(workspace_id,object_id)
		)`,
		`CREATE TABLE IF NOT EXISTS sync_changes (
			cursor INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id TEXT NOT NULL,
			object_id TEXT NOT NULL, source_device_id TEXT NOT NULL, writer_device_id TEXT NOT NULL, kind TEXT NOT NULL,
			epoch INTEGER NOT NULL, revision INTEGER NOT NULL, digest TEXT NOT NULL,
			deleted INTEGER NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_changes_workspace_cursor ON sync_changes(workspace_id,cursor)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_changes_workspace_object_cursor ON sync_changes(workspace_id,object_id,cursor)`,
		`CREATE TABLE IF NOT EXISTS sync_workspace_state (
			workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id), min_epoch INTEGER NOT NULL DEFAULT 1,
			deleted_at TEXT
		)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("sync migration: %w", err)
		}
	}
	for _, table := range []string{"sync_objects", "sync_changes"} {
		added, err := ensureSyncColumn(ctx, s.db, table, "writer_device_id", "TEXT NOT NULL DEFAULT ''")
		if err != nil {
			return err
		}
		if added {
			if _, err = s.db.ExecContext(ctx, "UPDATE "+table+" SET writer_device_id=source_device_id WHERE writer_device_id='' "); err != nil {
				return fmt.Errorf("backfill sync writer: %w", err)
			}
		}
	}
	return nil
}

func ensureSyncColumn(ctx context.Context, db *sql.DB, table, column, definition string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return false, rows.Err()
		}
	}
	if err = rows.Err(); err != nil {
		return false, err
	}
	if _, err = db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition); err != nil {
		return false, fmt.Errorf("add sync column %s.%s: %w", table, column, err)
	}
	return true, nil
}

// ConfigureSync sets deterministic hosted/self-hosted quotas before serving requests.
func (s *Store) ConfigureSync(limits SyncLimits) {
	if limits.MaxObjectBytes > 0 {
		s.syncLimits.MaxObjectBytes = limits.MaxObjectBytes
	}
	if limits.MaxWorkspaceBytes > 0 {
		s.syncLimits.MaxWorkspaceBytes = limits.MaxWorkspaceBytes
	}
	if limits.MaxPageSize > 0 {
		s.syncLimits.MaxPageSize = min(limits.MaxPageSize, MaxSyncPageSize)
	}
	if limits.MinFreeBytes > 0 {
		s.syncLimits.MinFreeBytes = limits.MinFreeBytes
	}
}

func (s *Store) PutSyncObject(deviceID string, object SyncObject, expectedRevision uint64) (SyncChange, error) {
	device, err := s.syncDevice(deviceID)
	if err != nil {
		return SyncChange{}, err
	}
	if err = validateSyncObject(device, object, s.syncLimits); err != nil {
		return SyncChange{}, err
	}
	object.WorkspaceID = device.WorkspaceID
	publicKey, err := s.SyncDeviceKey(device.ID, object.WriterDeviceID)
	if err != nil {
		return SyncChange{}, err
	}
	digest := digestSyncObject(object)
	if object.Digest != hex.EncodeToString(digest[:]) || !ed25519.Verify(publicKey, digest[:], object.Signature) {
		return SyncChange{}, errors.New("sync object signature invalid")
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return SyncChange{}, err
	}
	defer tx.Rollback()
	var minEpoch uint64 = 1
	err = tx.QueryRow(`SELECT min_epoch FROM sync_workspace_state WHERE workspace_id=?`, device.WorkspaceID).Scan(&minEpoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SyncChange{}, err
	}
	if object.Epoch < minEpoch {
		return SyncChange{}, ErrSyncConflict
	}
	var current SyncObject
	current, err = scanSyncObject(tx.QueryRow(`SELECT workspace_id,object_id,source_device_id,writer_device_id,kind,epoch,revision,parent_digest,nonce,ciphertext,digest,signature,deleted,updated_at FROM sync_objects WHERE workspace_id=? AND object_id=?`, device.WorkspaceID, object.ObjectID))
	if errors.Is(err, sql.ErrNoRows) {
		if expectedRevision != 0 || object.Revision != 1 {
			return SyncChange{}, ErrSyncConflict
		}
	} else if err != nil {
		return SyncChange{}, err
	} else {
		if current.Revision == object.Revision && current.Digest == object.Digest {
			change, findErr := latestSyncChange(tx, device.WorkspaceID, object.ObjectID, object.Revision)
			if findErr != nil {
				return SyncChange{}, findErr
			}
			return change, tx.Commit()
		}
		if current.Revision != expectedRevision || object.Revision != current.Revision+1 || object.ParentDigest != current.Digest || object.Epoch < current.Epoch || (current.Deleted && object.Epoch <= current.Epoch) {
			return SyncChange{}, ErrSyncConflict
		}
		if current.Kind != object.Kind || current.SourceDeviceID != object.SourceDeviceID {
			return SyncChange{}, ErrSyncConflict
		}
	}
	available, err := syncAvailableBytes(s.dbPath)
	if err != nil {
		return SyncChange{}, fmt.Errorf("check sync storage capacity: %w", err)
	}
	if available < uint64(s.syncLimits.MinFreeBytes) {
		return SyncChange{}, ErrSyncDisk
	}
	var used int64
	if err = tx.QueryRow(`SELECT COALESCE(SUM(LENGTH(ciphertext)),0) FROM sync_objects WHERE workspace_id=? AND deleted=0 AND object_id<>?`, device.WorkspaceID, object.ObjectID).Scan(&used); err != nil {
		return SyncChange{}, err
	}
	if used+int64(len(object.Ciphertext)) > s.syncLimits.MaxWorkspaceBytes {
		return SyncChange{}, ErrSyncQuota
	}
	now := time.Now().UTC()
	_, err = tx.Exec(`INSERT INTO sync_objects(workspace_id,object_id,source_device_id,writer_device_id,kind,epoch,revision,parent_digest,nonce,ciphertext,digest,signature,deleted,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(workspace_id,object_id) DO UPDATE SET writer_device_id=excluded.writer_device_id,epoch=excluded.epoch,revision=excluded.revision,parent_digest=excluded.parent_digest,nonce=excluded.nonce,ciphertext=excluded.ciphertext,digest=excluded.digest,signature=excluded.signature,deleted=excluded.deleted,updated_at=excluded.updated_at`,
		device.WorkspaceID, object.ObjectID, object.SourceDeviceID, object.WriterDeviceID, object.Kind, object.Epoch, object.Revision, object.ParentDigest, object.Nonce, object.Ciphertext, object.Digest, object.Signature, object.Deleted, now.Format(time.RFC3339Nano))
	if err != nil {
		return SyncChange{}, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_workspace_state(workspace_id,min_epoch,deleted_at) VALUES(?,1,NULL) ON CONFLICT(workspace_id) DO NOTHING`, device.WorkspaceID); err != nil {
		return SyncChange{}, err
	}
	change, err := appendSyncChange(tx, object, device.WorkspaceID, now)
	if err != nil {
		return SyncChange{}, err
	}
	return change, tx.Commit()
}

func (s *Store) DeleteSyncObject(deviceID, objectID string, expectedRevision, revision uint64, digest string, signature []byte) (SyncChange, error) {
	current, err := s.GetSyncObject(deviceID, objectID)
	if err != nil {
		return SyncChange{}, err
	}
	current.Revision, current.ParentDigest, current.Digest = revision, current.Digest, digest
	current.WriterDeviceID = deviceID
	current.Nonce, current.Ciphertext, current.Signature, current.Deleted = []byte{}, []byte{}, signature, true
	return s.PutSyncObject(deviceID, current, expectedRevision)
}

func (s *Store) GetSyncObject(deviceID, objectID string) (SyncObject, error) {
	device, err := s.syncDevice(deviceID)
	if err != nil {
		return SyncObject{}, err
	}
	value, err := scanSyncObject(s.db.QueryRow(`SELECT workspace_id,object_id,source_device_id,writer_device_id,kind,epoch,revision,parent_digest,nonce,ciphertext,digest,signature,deleted,updated_at FROM sync_objects WHERE workspace_id=? AND object_id=?`, device.WorkspaceID, objectID))
	if errors.Is(err, sql.ErrNoRows) {
		return SyncObject{}, ErrSyncNotFound
	}
	return value, err
}

func (s *Store) SyncChanges(deviceID string, cursor int64, limit int) ([]SyncChange, bool, error) {
	device, err := s.syncDevice(deviceID)
	if err != nil {
		return nil, false, err
	}
	if cursor < 0 {
		return nil, false, errors.New("invalid sync cursor")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > s.syncLimits.MaxPageSize {
		limit = s.syncLimits.MaxPageSize
	}
	if limit > MaxSyncPageSize {
		limit = MaxSyncPageSize
	}
	// Objects retain only their current ciphertext. Return only the latest change
	// for each object so an offline/new reader never requests a historical digest
	// that GetSyncObject cannot supply. Racing writes still fail the client digest
	// check and are retried from its unchanged cursor, never silently accepted.
	rows, err := s.db.Query(`SELECT cursor,workspace_id,object_id,source_device_id,writer_device_id,kind,epoch,revision,digest,deleted,updated_at FROM sync_changes AS current WHERE workspace_id=? AND cursor>? AND cursor=(SELECT MAX(cursor) FROM sync_changes AS newer WHERE newer.workspace_id=current.workspace_id AND newer.object_id=current.object_id) ORDER BY cursor LIMIT ?`, device.WorkspaceID, cursor, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	changes := make([]SyncChange, 0, limit)
	for rows.Next() {
		var value SyncChange
		var updated string
		if err = rows.Scan(&value.Cursor, &value.WorkspaceID, &value.ObjectID, &value.SourceDeviceID, &value.WriterDeviceID, &value.Kind, &value.Epoch, &value.Revision, &value.Digest, &value.Deleted, &updated); err != nil {
			return nil, false, err
		}
		value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		changes = append(changes, value)
	}
	more := len(changes) > limit
	if more {
		changes = changes[:limit]
	}
	return changes, more, rows.Err()
}

func (s *Store) SyncUsage(deviceID string) (SyncUsage, error) {
	device, err := s.syncDevice(deviceID)
	if err != nil {
		return SyncUsage{}, err
	}
	usage := SyncUsage{Limit: s.syncLimits.MaxWorkspaceBytes}
	err = s.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(LENGTH(ciphertext)),0) FROM sync_objects WHERE workspace_id=? AND deleted=0`, device.WorkspaceID).Scan(&usage.Objects, &usage.Bytes)
	return usage, err
}

// SyncWorkspaceEpoch returns the minimum accepted key epoch for an authorized
// device. Clients use it to resolve an ambiguous destructive-response outcome
// without asking the bridge to reveal or handle encryption keys.
func (s *Store) SyncWorkspaceEpoch(deviceID string) (uint64, error) {
	device, err := s.syncDevice(deviceID)
	if err != nil {
		return 0, err
	}
	var epoch uint64 = 1
	err = s.db.QueryRow(`SELECT min_epoch FROM sync_workspace_state WHERE workspace_id=?`, device.WorkspaceID).Scan(&epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	if epoch == 0 {
		return 0, errors.New("invalid sync workspace epoch")
	}
	return epoch, nil
}

// DeleteSyncWorkspace atomically purges current ciphertext and its change history,
// then advances the minimum accepted key epoch so an offline stale device cannot resurrect it.
func (s *Store) DeleteSyncWorkspace(deviceID string) (uint64, error) {
	device, err := s.syncDevice(deviceID)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var maxEpoch uint64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(epoch),0) FROM sync_objects WHERE workspace_id=?`, device.WorkspaceID).Scan(&maxEpoch); err != nil {
		return 0, err
	}
	var current uint64 = 1
	err = tx.QueryRow(`SELECT min_epoch FROM sync_workspace_state WHERE workspace_id=?`, device.WorkspaceID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next := maxEpoch + 1
	if next <= current {
		next = current + 1
	}
	if _, err = tx.Exec(`DELETE FROM sync_objects WHERE workspace_id=?`, device.WorkspaceID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(`DELETE FROM sync_changes WHERE workspace_id=?`, device.WorkspaceID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_workspace_state(workspace_id,min_epoch,deleted_at) VALUES(?,?,?)
		ON CONFLICT(workspace_id) DO UPDATE SET min_epoch=excluded.min_epoch,deleted_at=excluded.deleted_at`,
		device.WorkspaceID, next, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	return next, tx.Commit()
}

func (s *Store) SyncDeviceKey(requesterID, sourceID string) (ed25519.PublicKey, error) {
	requester, err := s.syncDevice(requesterID)
	if err != nil {
		return nil, err
	}
	var workspaceID string
	var key []byte
	var revoked sql.NullString
	err = s.db.QueryRow(`SELECT workspace_id,public_key,revoked_at FROM devices WHERE id=?`, sourceID).Scan(&workspaceID, &key, &revoked)
	if err != nil || revoked.Valid || workspaceID != requester.WorkspaceID || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("sync source device not found")
	}
	return ed25519.PublicKey(key), nil
}

func (s *Store) syncDevice(deviceID string) (Device, error) {
	value, err := scanDevice(s.db.QueryRow(`SELECT id,workspace_id,owner,name,created_at,last_seen,revoked_at FROM devices WHERE id=?`, deviceID))
	if err != nil || value.revoked || value.Owner == "" {
		return Device{}, errors.New("linked active device required")
	}
	return value.Device, nil
}

func validateSyncObject(device Device, object SyncObject, limits SyncLimits) error {
	if !strings.HasPrefix(object.ObjectID, "obj_") || object.SourceDeviceID == "" || object.WriterDeviceID != device.ID || object.Epoch == 0 || object.Revision == 0 || object.Digest == "" || len(object.Signature) != ed25519.SignatureSize {
		return errors.New("invalid opaque sync object")
	}
	if immutableSyncKind(object.Kind) && object.SourceDeviceID != device.ID {
		return errors.New("sync source device mismatch")
	}
	switch object.Kind {
	case "session_summary", "finding", "note", "experiment", "preference", "evidence":
	default:
		return errors.New("unsupported sync object kind")
	}
	if !object.Deleted && (len(object.Nonce) == 0 || len(object.Ciphertext) == 0) {
		return errors.New("sync object ciphertext required")
	}
	if int64(len(object.Ciphertext)) > limits.MaxObjectBytes {
		return ErrSyncQuota
	}
	return nil
}

func scanSyncObject(row rowScanner) (SyncObject, error) {
	var value SyncObject
	var updated string
	err := row.Scan(&value.WorkspaceID, &value.ObjectID, &value.SourceDeviceID, &value.WriterDeviceID, &value.Kind, &value.Epoch, &value.Revision, &value.ParentDigest, &value.Nonce, &value.Ciphertext, &value.Digest, &value.Signature, &value.Deleted, &updated)
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return value, err
}

func appendSyncChange(tx *sql.Tx, object SyncObject, workspaceID string, now time.Time) (SyncChange, error) {
	result, err := tx.Exec(`INSERT INTO sync_changes(workspace_id,object_id,source_device_id,writer_device_id,kind,epoch,revision,digest,deleted,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		workspaceID, object.ObjectID, object.SourceDeviceID, object.WriterDeviceID, object.Kind, object.Epoch, object.Revision, object.Digest, object.Deleted, now.Format(time.RFC3339Nano))
	if err != nil {
		return SyncChange{}, err
	}
	cursor, _ := result.LastInsertId()
	return SyncChange{Cursor: cursor, WorkspaceID: workspaceID, ObjectID: object.ObjectID, SourceDeviceID: object.SourceDeviceID, WriterDeviceID: object.WriterDeviceID, Kind: object.Kind, Epoch: object.Epoch, Revision: object.Revision, Digest: object.Digest, Deleted: object.Deleted, UpdatedAt: now}, nil
}

func latestSyncChange(tx *sql.Tx, workspaceID, objectID string, revision uint64) (SyncChange, error) {
	var value SyncChange
	var updated string
	err := tx.QueryRow(`SELECT cursor,workspace_id,object_id,source_device_id,writer_device_id,kind,epoch,revision,digest,deleted,updated_at FROM sync_changes WHERE workspace_id=? AND object_id=? AND revision=? ORDER BY cursor DESC LIMIT 1`, workspaceID, objectID, revision).
		Scan(&value.Cursor, &value.WorkspaceID, &value.ObjectID, &value.SourceDeviceID, &value.WriterDeviceID, &value.Kind, &value.Epoch, &value.Revision, &value.Digest, &value.Deleted, &updated)
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return value, err
}

func immutableSyncKind(kind string) bool {
	switch kind {
	case "session_summary", "finding", "evidence":
		return true
	default:
		return false
	}
}

func digestSyncObject(object SyncObject) [32]byte {
	value := struct {
		WorkspaceID    string `json:"workspace_id"`
		ObjectID       string `json:"object_id"`
		SourceDeviceID string `json:"source_device_id"`
		WriterDeviceID string `json:"writer_device_id"`
		Kind           string `json:"kind"`
		Epoch          uint64 `json:"epoch"`
		Revision       uint64 `json:"revision"`
		ParentDigest   string `json:"parent_digest,omitempty"`
		Nonce          []byte `json:"nonce,omitempty"`
		Ciphertext     []byte `json:"ciphertext,omitempty"`
		Deleted        bool   `json:"deleted,omitempty"`
	}{object.WorkspaceID, object.ObjectID, object.SourceDeviceID, object.WriterDeviceID, object.Kind, object.Epoch, object.Revision, object.ParentDigest, object.Nonce, object.Ciphertext, object.Deleted}
	encoded, _ := json.Marshal(value)
	return sha256.Sum256(encoded)
}
