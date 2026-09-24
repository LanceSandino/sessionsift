// @module BridgeRegistration lets a Local Agent join the bridge by itself and link to an owner with a code it shows.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

const (
	linkLifetime       = 10 * time.Minute
	maxActiveLinks     = 5
	unlinkedOwner      = ""
	unlinkedWorkspace  = "workspace_device_"
	deviceNameMaxBytes = 120
)

func (s *Store) migrateConnections(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS device_links (
			code_hash TEXT PRIMARY KEY, device_id TEXT NOT NULL REFERENCES devices(id),
			expires_at TEXT NOT NULL, used_at TEXT, created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_device_links_device ON device_links(device_id,expires_at)`,
		`CREATE TABLE IF NOT EXISTS support_sessions (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			device_id TEXT NOT NULL REFERENCES devices(id), code_hash TEXT NOT NULL UNIQUE,
			session_id TEXT NOT NULL, role TEXT NOT NULL, operations_json TEXT NOT NULL,
			created_at TEXT NOT NULL, code_expires_at TEXT NOT NULL, access_minutes INTEGER NOT NULL,
			supporter TEXT, connected_at TEXT, access_expires_at TEXT, token_hash TEXT,
			last_viewed_at TEXT, revoked_at TEXT, end_reason TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_support_device ON support_sessions(device_id,created_at)`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("bridge connection migration: %w", err)
		}
	}
	return nil
}

func deviceIDFor(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return "device_" + hex.EncodeToString(digest[:12])
}

// RegisterDevice records a Local Agent that proved possession of its key. A new
// device is unlinked: it can serve only support sessions it creates itself
// until an owner enters a link code the device shows. Re-registration is
// idempotent and never changes an existing owner or workspace.
func (s *Store) RegisterDevice(name string, publicKey ed25519.PublicKey) (Device, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return Device{}, errors.New("invalid Ed25519 public key")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > deviceNameMaxBytes {
		return Device{}, errors.New("device name must be between 1 and 120 characters")
	}
	id := deviceIDFor(publicKey)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	existing, err := scanDevice(tx.QueryRow(`SELECT id,workspace_id,owner,name,created_at,last_seen,revoked_at FROM devices WHERE id=?`, id))
	if err == nil {
		if existing.revoked {
			return Device{}, errors.New("this device identity was disconnected; create a new identity to register again")
		}
		return existing.Device, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Device{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	workspaceID := unlinkedWorkspace + strings.TrimPrefix(id, "device_")
	if _, err = tx.Exec(`INSERT INTO workspaces(id,name,owner,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING`, workspaceID, name+" (unlinked device)", unlinkedOwner, now); err != nil {
		return Device{}, err
	}
	if _, err = tx.Exec(`INSERT INTO devices(id,workspace_id,owner,name,public_key,created_at,last_seen) VALUES(?,?,?,?,?,?,?)`, id, workspaceID, unlinkedOwner, name, []byte(publicKey), now, now); err != nil {
		return Device{}, err
	}
	if err = tx.Commit(); err != nil {
		return Device{}, err
	}
	created, _ := time.Parse(time.RFC3339Nano, now)
	return Device{ID: id, WorkspaceID: workspaceID, Owner: unlinkedOwner, Name: name, CreatedAt: created, LastSeen: created}, nil
}

type deviceRow struct {
	Device
	revoked bool
}

func scanDevice(row rowScanner) (deviceRow, error) {
	var value deviceRow
	var created, seen string
	var revoked sql.NullString
	if err := row.Scan(&value.ID, &value.WorkspaceID, &value.Owner, &value.Name, &created, &seen, &revoked); err != nil {
		return deviceRow{}, err
	}
	value.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	value.LastSeen, _ = time.Parse(time.RFC3339Nano, seen)
	value.revoked = revoked.Valid
	return value, nil
}

// DeviceInfo returns the bridge's view of one active device for its own agent.
func (s *Store) DeviceInfo(deviceID string) (Device, error) {
	value, err := scanDevice(s.db.QueryRow(`SELECT id,workspace_id,owner,name,created_at,last_seen,revoked_at FROM devices WHERE id=?`, deviceID))
	if err != nil || value.revoked {
		return Device{}, errors.New("device not found")
	}
	return value.Device, nil
}

// CreateDeviceLink stores the hash of a link code the device generated and shows.
func (s *Store) CreateDeviceLink(deviceID, codeHash string, expiresAt time.Time) error {
	if !supportcode.ValidHash(codeHash) {
		return errors.New("invalid link code hash")
	}
	now := time.Now().UTC()
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(linkLifetime+time.Minute)) {
		return errors.New("link code expiry must be within ten minutes")
	}
	var active int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM device_links WHERE device_id=? AND used_at IS NULL AND expires_at>?`, deviceID, now.Format(time.RFC3339Nano)).Scan(&active); err != nil {
		return err
	}
	if active >= maxActiveLinks {
		return errors.New("too many active link codes; wait for one to expire")
	}
	result, err := s.db.Exec(`INSERT INTO device_links(code_hash,device_id,expires_at,created_at)
		SELECT ?,id,?,? FROM devices WHERE id=? AND revoked_at IS NULL`, codeHash, expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), deviceID)
	if err != nil {
		return fmt.Errorf("create device link: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("device not found")
	}
	return nil
}

// ClaimDeviceLink links the device that showed code to owner, placing it in
// the owner's personal workspace or a workspace the owner controls. A device
// linked to a different owner can never be taken over.
func (s *Store) ClaimDeviceLink(owner, code, workspaceID string) (Device, error) {
	owner, err := normalizeEmail(owner)
	if err != nil {
		return Device{}, err
	}
	codeHash, err := supportcode.Hash(supportcode.Link, code)
	if err != nil {
		return Device{}, errors.New("invalid link code")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspace, workspaceErr := s.ensurePersonalWorkspace(context.Background(), owner)
		if workspaceErr != nil {
			return Device{}, workspaceErr
		}
		workspaceID = workspace.ID
	} else if err = s.requireWorkspaceOwner(owner, workspaceID); err != nil {
		return Device{}, err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	var deviceID, expires string
	var used sql.NullString
	err = tx.QueryRow(`SELECT device_id,expires_at,used_at FROM device_links WHERE code_hash=?`, codeHash).Scan(&deviceID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, errors.New("invalid link code")
	}
	if err != nil {
		return Device{}, err
	}
	expiresAt, parseErr := time.Parse(time.RFC3339Nano, expires)
	if parseErr != nil || used.Valid || !time.Now().Before(expiresAt) {
		return Device{}, errors.New("link code is expired or already used")
	}
	device, err := scanDevice(tx.QueryRow(`SELECT id,workspace_id,owner,name,created_at,last_seen,revoked_at FROM devices WHERE id=?`, deviceID))
	if err != nil || device.revoked {
		return Device{}, errors.New("device not found")
	}
	if device.Owner != unlinkedOwner && device.Owner != owner {
		return Device{}, errors.New("device is already linked to another owner")
	}
	previous := device.WorkspaceID
	for _, statement := range []string{
		`UPDATE devices SET owner=?,workspace_id=? WHERE id=?`,
		`UPDATE grants SET workspace_id=? WHERE device_id=?`,
		`UPDATE tasks SET workspace_id=? WHERE device_id=?`,
		`UPDATE invitations SET workspace_id=? WHERE device_id=?`,
		`UPDATE support_sessions SET workspace_id=? WHERE device_id=?`,
		`UPDATE mcp_targets SET workspace_id=? WHERE device_id=?`,
	} {
		arguments := []any{workspaceID, deviceID}
		if strings.HasPrefix(statement, "UPDATE devices") {
			arguments = []any{owner, workspaceID, deviceID}
		}
		if _, err = tx.Exec(statement, arguments...); err != nil {
			return Device{}, err
		}
	}
	if _, err = tx.Exec(`UPDATE device_links SET used_at=? WHERE code_hash=? AND used_at IS NULL`, time.Now().UTC().Format(time.RFC3339Nano), codeHash); err != nil {
		return Device{}, err
	}
	if previous != workspaceID && strings.HasPrefix(previous, unlinkedWorkspace) {
		if _, err = tx.Exec(`DELETE FROM workspaces WHERE id=? AND owner=?`, previous, unlinkedOwner); err != nil {
			return Device{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Device{}, err
	}
	device.Owner, device.WorkspaceID = owner, workspaceID
	return device.Device, nil
}
