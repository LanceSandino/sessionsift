// Package bridge provides the durable, metadata-only relay used by the hosted
// SessionSift connection service. Session content is retained only long enough
// to complete a request and is never copied into the local SessionSift store.
// @module BridgeStore persists workspaces, devices, grants, invitations, and encrypted transient relay tasks.
package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	enrollmentLifetime = 10 * time.Minute
	claimLifetime      = 30 * time.Second
)

type Store struct {
	db     *sql.DB
	cipher *envelopeCipher
}

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Membership struct {
	WorkspaceID string    `json:"workspace_id"`
	Principal   string    `json:"principal"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

type MCPTarget struct {
	WorkspaceID string `json:"workspace_id"`
	DeviceID    string `json:"device_id"`
}

type Device struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Owner       string    `json:"owner"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen"`
}

type Grant struct {
	WorkspaceID string               `json:"workspace_id"`
	DeviceID    string               `json:"device_id"`
	Principal   string               `json:"principal"`
	Operations  []protocol.Operation `json:"operations"`
	ExpiresAt   time.Time            `json:"expires_at"`
	RevokedAt   *time.Time           `json:"revoked_at,omitempty"`
}

type Invitation struct {
	ID           string               `json:"id"`
	WorkspaceID  string               `json:"workspace_id"`
	DeviceID     string               `json:"device_id"`
	Owner        string               `json:"owner"`
	Invitee      string               `json:"invitee"`
	SessionID    string               `json:"session_id"`
	Role         string               `json:"role"`
	Operations   []protocol.Operation `json:"operations"`
	CreatedAt    time.Time            `json:"created_at"`
	ExpiresAt    time.Time            `json:"expires_at"`
	AcceptedAt   *time.Time           `json:"accepted_at,omitempty"`
	LastViewedAt *time.Time           `json:"last_viewed_at,omitempty"`
	RevokedAt    *time.Time           `json:"revoked_at,omitempty"`
}

type Task struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	DeviceID    string           `json:"device_id"`
	Principal   string           `json:"principal"`
	Request     protocol.Request `json:"request"`
	CreatedAt   time.Time        `json:"created_at"`
}

func Open(path string) (*Store, error) {
	envelope, err := loadOrCreateBridgeCipher(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, cipher: envelope}
	if err = store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure bridge database: %w", err)
	}
	if err = store.encryptLegacyTaskPayloads(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("encrypt legacy bridge tasks: %w", err)
	}
	if err = store.verifyEncryptionKey(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Health(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("bridge database ping: %w", err)
	}
	var result string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("bridge database quick check: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("bridge database quick check: %s", result)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS request_receipts (device_id TEXT NOT NULL, digest TEXT NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY(device_id,digest))`,
		`CREATE INDEX IF NOT EXISTS idx_receipt_expiry ON request_receipts(expires_at)`,
		`CREATE TABLE IF NOT EXISTS bridge_audit (id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, device_id TEXT NOT NULL, principal TEXT NOT NULL, operation TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_workspace ON bridge_audit(workspace_id,created_at)`,
		`CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, owner TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS memberships (
			workspace_id TEXT NOT NULL REFERENCES workspaces(id), principal TEXT NOT NULL,
			role TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(workspace_id,principal)
		)`,
		`CREATE TABLE IF NOT EXISTS enrollments (
			code_hash TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			owner TEXT NOT NULL, expires_at TEXT NOT NULL,
			used_at TEXT, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			owner TEXT NOT NULL, name TEXT NOT NULL,
			public_key BLOB NOT NULL, created_at TEXT NOT NULL, last_seen TEXT NOT NULL,
			revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS grants (
			workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			device_id TEXT NOT NULL REFERENCES devices(id), principal TEXT NOT NULL,
			operations_json TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT,
			created_at TEXT NOT NULL, PRIMARY KEY(device_id, principal)
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			device_id TEXT NOT NULL REFERENCES devices(id),
			principal TEXT NOT NULL, request_json BLOB NOT NULL, response_json BLOB,
			status TEXT NOT NULL, created_at TEXT NOT NULL, claimed_at TEXT,
			completed_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS invitations (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			device_id TEXT NOT NULL REFERENCES devices(id),
			owner TEXT NOT NULL, invitee TEXT NOT NULL, session_id TEXT NOT NULL,
			role TEXT NOT NULL, operations_json TEXT NOT NULL, code_hash TEXT NOT NULL UNIQUE,
			session_token_hash TEXT, created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
			accepted_at TEXT, revoked_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_targets (
			principal TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id),
			device_id TEXT NOT NULL REFERENCES devices(id), updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("bridge migration: %w", err)
		}
	}
	for _, column := range []struct{ table, definition string }{
		{"enrollments", "workspace_id TEXT NOT NULL DEFAULT ''"},
		{"devices", "workspace_id TEXT NOT NULL DEFAULT ''"},
		{"grants", "workspace_id TEXT NOT NULL DEFAULT ''"},
		{"tasks", "workspace_id TEXT NOT NULL DEFAULT ''"},
		{"invitations", "workspace_id TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureColumn(ctx, column.table, "workspace_id", column.definition); err != nil {
			return err
		}
	}
	if err := s.backfillWorkspaceScopes(ctx); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "invitations", "last_viewed_at", "last_viewed_at TEXT"); err != nil {
		return err
	}
	if err := s.migrateConnections(ctx); err != nil {
		return err
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_tasks_workspace_device_status ON tasks(workspace_id,device_id,status,created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_grants_workspace_principal ON grants(workspace_id,principal,expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_invitations_workspace_device ON invitations(workspace_id,device_id,created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_invitations_invitee ON invitations(invitee,expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memberships_principal ON memberships(principal,workspace_id)`,
	}
	for _, statement := range indexes {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("bridge index migration: %w", err)
		}
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, name, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var column, kind string
		var notNull, primary int
		var defaultValue any
		if err = rows.Scan(&cid, &column, &kind, &notNull, &defaultValue, &primary); err != nil {
			return err
		}
		if column == name {
			found = true
		}
	}
	if err = rows.Err(); err != nil || found {
		return err
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+definition)
	return err
}

func workspaceIDFor(owner string) string {
	digest := sha256.Sum256([]byte("personal-workspace:" + owner))
	return "workspace_" + hex.EncodeToString(digest[:12])
}

func workspaceName(owner string) string {
	name := strings.SplitN(owner, "@", 2)[0]
	if name == "" {
		name = "Personal"
	}
	return name + "'s workspace"
}

func (s *Store) ensurePersonalWorkspace(ctx context.Context, owner string) (Workspace, error) {
	owner, err := normalizeEmail(owner)
	if err != nil {
		return Workspace{}, err
	}
	now := time.Now().UTC()
	workspace := Workspace{ID: workspaceIDFor(owner), Name: workspaceName(owner), Owner: owner, Role: "owner", CreatedAt: now}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO workspaces(id,name,owner,created_at) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET owner=excluded.owner`, workspace.ID, workspace.Name, owner, now.Format(time.RFC3339Nano)); err != nil {
		return Workspace{}, err
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO memberships(workspace_id,principal,role,created_at) VALUES(?,?,?,?)
		ON CONFLICT(workspace_id,principal) DO UPDATE SET role='owner'`, workspace.ID, owner, "owner", now.Format(time.RFC3339Nano)); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

// CreateWorkspace creates an isolated tenant boundary owned by one verified identity.
func (s *Store) CreateWorkspace(owner, name string) (Workspace, error) {
	owner, err := normalizeEmail(owner)
	if err != nil {
		return Workspace{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return Workspace{}, errors.New("workspace name must be between 1 and 80 characters")
	}
	now := time.Now().UTC()
	workspace := Workspace{ID: "workspace_" + uuid.NewString(), Name: name, Owner: owner, Role: "owner", CreatedAt: now}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO workspaces(id,name,owner,created_at) VALUES(?,?,?,?)`, workspace.ID, name, owner, now.Format(time.RFC3339Nano)); err != nil {
		return Workspace{}, err
	}
	if _, err = tx.Exec(`INSERT INTO memberships(workspace_id,principal,role,created_at) VALUES(?,?,?,?)`, workspace.ID, owner, "owner", now.Format(time.RFC3339Nano)); err != nil {
		return Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (s *Store) requireWorkspaceOwner(requester, workspaceID string) error {
	requester, err := normalizeEmail(requester)
	if err != nil {
		return err
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE id=? AND owner=?`, strings.TrimSpace(workspaceID), requester).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("workspace owner access is required")
	}
	return nil
}

// AddMembership is intentionally owner-only. Membership does not itself grant session operations.
func (s *Store) AddMembership(requester, workspaceID, principal, role string) (Membership, error) {
	if err := s.requireWorkspaceOwner(requester, workspaceID); err != nil {
		return Membership{}, err
	}
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Membership{}, err
	}
	role = strings.TrimSpace(strings.ToLower(role))
	if role != "member" && role != "admin" {
		return Membership{}, errors.New("membership role must be member or admin")
	}
	var owner string
	if err = s.db.QueryRow(`SELECT owner FROM workspaces WHERE id=?`, workspaceID).Scan(&owner); err != nil {
		return Membership{}, err
	}
	if principal == owner {
		return Membership{}, errors.New("workspace owner role cannot be changed")
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(`INSERT INTO memberships(workspace_id,principal,role,created_at) VALUES(?,?,?,?)
		ON CONFLICT(workspace_id,principal) DO UPDATE SET role=excluded.role`, workspaceID, principal, role, now.Format(time.RFC3339Nano))
	if err != nil {
		return Membership{}, err
	}
	return Membership{WorkspaceID: workspaceID, Principal: principal, Role: role, CreatedAt: now}, nil
}

func (s *Store) MembersFor(requester, workspaceID string) ([]Membership, error) {
	if err := s.requireWorkspaceOwner(requester, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT workspace_id,principal,role,created_at FROM memberships WHERE workspace_id=? ORDER BY role,principal`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Membership{}
	for rows.Next() {
		var value Membership
		var created string
		if err = rows.Scan(&value.WorkspaceID, &value.Principal, &value.Role, &created); err != nil {
			return nil, err
		}
		value.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) RemoveMembership(requester, workspaceID, principal string) error {
	if err := s.requireWorkspaceOwner(requester, workspaceID); err != nil {
		return err
	}
	principal, err := normalizeEmail(principal)
	if err != nil {
		return err
	}
	var owner string
	if err = s.db.QueryRow(`SELECT owner FROM workspaces WHERE id=?`, workspaceID).Scan(&owner); err != nil {
		return err
	}
	if principal == owner {
		return errors.New("workspace owner membership cannot be removed")
	}
	result, err := s.db.Exec(`DELETE FROM memberships WHERE workspace_id=? AND principal=?`, workspaceID, principal)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("workspace membership not found")
	}
	_, _ = s.db.Exec(`DELETE FROM mcp_targets WHERE principal=? AND workspace_id=?`, principal, workspaceID)
	return nil
}

func (s *Store) backfillWorkspaceScopes(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT owner FROM (
		SELECT owner FROM enrollments UNION SELECT owner FROM devices UNION SELECT owner FROM invitations
	) WHERE owner<>''`)
	if err != nil {
		return err
	}
	owners := []string{}
	for rows.Next() {
		var owner string
		if err = rows.Scan(&owner); err != nil {
			_ = rows.Close()
			return err
		}
		owners = append(owners, owner)
	}
	_ = rows.Close()
	for _, owner := range owners {
		workspace, workspaceErr := s.ensurePersonalWorkspace(ctx, owner)
		if workspaceErr != nil {
			return workspaceErr
		}
		for _, statement := range []string{
			`UPDATE enrollments SET workspace_id=? WHERE owner=? AND workspace_id=''`,
			`UPDATE devices SET workspace_id=? WHERE owner=? AND workspace_id=''`,
			`UPDATE invitations SET workspace_id=? WHERE owner=? AND workspace_id=''`,
		} {
			if _, err = s.db.ExecContext(ctx, statement, workspace.ID, owner); err != nil {
				return err
			}
		}
	}
	for _, statement := range []string{
		`UPDATE grants SET workspace_id=(SELECT workspace_id FROM devices WHERE devices.id=grants.device_id) WHERE workspace_id=''`,
		`UPDATE tasks SET workspace_id=(SELECT workspace_id FROM devices WHERE devices.id=tasks.device_id) WHERE workspace_id=''`,
		`UPDATE invitations SET workspace_id=(SELECT workspace_id FROM devices WHERE devices.id=invitations.device_id) WHERE workspace_id=''`,
	} {
		if _, err = s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateInvitation(deviceID, owner, invitee, sessionID, role string, expiresAt time.Time) (Invitation, string, error) {
	if strings.TrimSpace(owner) == "" {
		return Invitation{}, "", errors.New("link this device to an owner account before sending email invitations, or share a support code")
	}
	owner, err := normalizeEmail(owner)
	if err != nil {
		return Invitation{}, "", err
	}
	invitee, err = normalizeEmail(invitee)
	if err != nil {
		return Invitation{}, "", err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 256 {
		return Invitation{}, "", errors.New("valid session is required")
	}
	operations, ok := protocol.InvitationOperations(role)
	if !ok {
		return Invitation{}, "", errors.New("invalid invitation role")
	}
	now := time.Now().UTC()
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(30*24*time.Hour)) {
		return Invitation{}, "", errors.New("invitation expiry must be within 30 days")
	}
	code, err := randomCode()
	if err != nil {
		return Invitation{}, "", err
	}
	encoded, _ := json.Marshal(operations)
	invitation := Invitation{ID: "invite_" + uuid.NewString(), DeviceID: deviceID, Owner: owner, Invitee: invitee, SessionID: sessionID, Role: role, Operations: operations, CreatedAt: now, ExpiresAt: expiresAt}
	result, err := s.db.Exec(`INSERT INTO invitations(id,workspace_id,device_id,owner,invitee,session_id,role,operations_json,code_hash,created_at,expires_at)
		SELECT ?,workspace_id,id,?,?,?,?,?,?,?,? FROM devices WHERE id=? AND owner=? AND revoked_at IS NULL`,
		invitation.ID, owner, invitee, sessionID, role, encoded, hashCode(code), now.Format(time.RFC3339Nano), expiresAt.Format(time.RFC3339Nano), deviceID, owner)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("create invitation: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Invitation{}, "", errors.New("active owned device not found")
	}
	if err = s.db.QueryRow(`SELECT workspace_id FROM invitations WHERE id=?`, invitation.ID).Scan(&invitation.WorkspaceID); err != nil {
		return Invitation{}, "", err
	}
	return invitation, code, nil
}

func (s *Store) AcceptInvitation(code, principal string) (Invitation, string, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Invitation{}, "", err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Invitation{}, "", err
	}
	defer tx.Rollback()
	invitation, codeHash, err := scanInvitation(tx.QueryRow(`SELECT id,workspace_id,device_id,owner,invitee,session_id,role,operations_json,created_at,expires_at,accepted_at,revoked_at,last_viewed_at,code_hash
		FROM invitations WHERE code_hash=?`, hashCode(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, "", errors.New("invalid invitation")
	}
	if err != nil {
		return Invitation{}, "", err
	}
	if invitation.Invitee != principal {
		return Invitation{}, "", errors.New("invitation is assigned to another identity")
	}
	if invitation.AcceptedAt != nil || invitation.RevokedAt != nil || !time.Now().Before(invitation.ExpiresAt) {
		return Invitation{}, "", errors.New("invitation is expired, revoked, or already used")
	}
	token, err := randomSessionToken()
	if err != nil {
		return Invitation{}, "", err
	}
	now := time.Now().UTC()
	result, err := tx.Exec(`UPDATE invitations SET accepted_at=?,session_token_hash=? WHERE code_hash=? AND accepted_at IS NULL AND revoked_at IS NULL`, now.Format(time.RFC3339Nano), hashSecret(token), codeHash)
	if err != nil {
		return Invitation{}, "", err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Invitation{}, "", errors.New("invitation was already used")
	}
	if err = tx.Commit(); err != nil {
		return Invitation{}, "", err
	}
	invitation.AcceptedAt = &now
	return invitation, token, nil
}

// InvitationForAcceptance validates an invitation link without consuming its
// single-use secret. GET requests use this to present an explicit confirmation
// step so link previews and prefetchers cannot accept an invitation.
func (s *Store) InvitationForAcceptance(code, principal string) (Invitation, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Invitation{}, err
	}
	invitation, _, err := scanInvitation(s.db.QueryRow(`SELECT id,workspace_id,device_id,owner,invitee,session_id,role,operations_json,created_at,expires_at,accepted_at,revoked_at,last_viewed_at,code_hash
		FROM invitations WHERE code_hash=?`, hashCode(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, errors.New("invalid invitation")
	}
	if err != nil {
		return Invitation{}, err
	}
	if invitation.Invitee != principal {
		return Invitation{}, errors.New("invitation is assigned to another identity")
	}
	if invitation.RevokedAt != nil || !time.Now().Before(invitation.ExpiresAt) {
		return Invitation{}, errors.New("invitation is expired or revoked")
	}
	return invitation, nil
}

func (s *Store) InvitationsForDevice(deviceID string) ([]Invitation, error) {
	rows, err := s.db.Query(`SELECT id,workspace_id,device_id,owner,invitee,session_id,role,operations_json,created_at,expires_at,accepted_at,revoked_at,last_viewed_at,''
		FROM invitations WHERE device_id=? ORDER BY created_at DESC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Invitation, 0)
	for rows.Next() {
		value, _, scanErr := scanInvitation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) RevokeInvitation(deviceID, invitationID string) error {
	result, err := s.db.Exec(`UPDATE invitations SET revoked_at=? WHERE id=? AND device_id=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339Nano), invitationID, deviceID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("invitation not found")
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanInvitation(row rowScanner) (Invitation, string, error) {
	var invitation Invitation
	var operations []byte
	var created, expires string
	var accepted, revoked, viewed sql.NullString
	var codeHash string
	if err := row.Scan(&invitation.ID, &invitation.WorkspaceID, &invitation.DeviceID, &invitation.Owner, &invitation.Invitee, &invitation.SessionID, &invitation.Role, &operations, &created, &expires, &accepted, &revoked, &viewed, &codeHash); err != nil {
		return Invitation{}, "", err
	}
	if err := json.Unmarshal(operations, &invitation.Operations); err != nil {
		return Invitation{}, "", err
	}
	invitation.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	invitation.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	if viewed.Valid {
		value, _ := time.Parse(time.RFC3339Nano, viewed.String)
		invitation.LastViewedAt = &value
	}
	if accepted.Valid {
		value, _ := time.Parse(time.RFC3339Nano, accepted.String)
		invitation.AcceptedAt = &value
	}
	if revoked.Valid {
		value, _ := time.Parse(time.RFC3339Nano, revoked.String)
		invitation.RevokedAt = &value
	}
	return invitation, codeHash, nil
}

func (s *Store) RegisterGrant(deviceID, principal string, operations []protocol.Operation, expiresAt time.Time) (Grant, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Grant{}, err
	}
	if expiresAt.Before(time.Now()) {
		return Grant{}, errors.New("grant expiry must be in the future")
	}
	validated, err := validateOperations(operations)
	if err != nil {
		return Grant{}, err
	}
	encoded, _ := json.Marshal(validated)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`INSERT INTO grants(workspace_id,device_id,principal,operations_json,expires_at,created_at,revoked_at)
		SELECT workspace_id,id,?,?,?,?,NULL FROM devices WHERE id=? AND revoked_at IS NULL
		ON CONFLICT(device_id,principal) DO UPDATE SET workspace_id=excluded.workspace_id,operations_json=excluded.operations_json,expires_at=excluded.expires_at,revoked_at=NULL`,
		principal, encoded, expiresAt.UTC().Format(time.RFC3339Nano), now, deviceID)
	if err != nil {
		return Grant{}, fmt.Errorf("register grant: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return Grant{}, errors.New("device not found")
	}
	var workspaceID string
	if err = s.db.QueryRow(`SELECT workspace_id FROM devices WHERE id=?`, deviceID).Scan(&workspaceID); err != nil {
		return Grant{}, err
	}
	return Grant{WorkspaceID: workspaceID, DeviceID: deviceID, Principal: principal, Operations: validated, ExpiresAt: expiresAt.UTC()}, nil
}

func (s *Store) RevokeGrant(deviceID, principal string) error {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE grants SET revoked_at=? WHERE device_id=? AND principal=?`, time.Now().UTC().Format(time.RFC3339Nano), deviceID, principal)
	return err
}

func (s *Store) DevicesFor(principal string) ([]Device, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT d.id,d.workspace_id,d.owner,d.name,d.created_at,d.last_seen FROM devices d
		JOIN grants g ON g.device_id=d.id WHERE g.principal=? AND g.revoked_at IS NULL
		AND g.expires_at>? AND d.revoked_at IS NULL ORDER BY d.last_seen DESC`, principal, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]Device, 0)
	for rows.Next() {
		var device Device
		var created, seen string
		if err = rows.Scan(&device.ID, &device.WorkspaceID, &device.Owner, &device.Name, &created, &seen); err != nil {
			return nil, err
		}
		device.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		device.LastSeen, _ = time.Parse(time.RFC3339Nano, seen)
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) OwnedDevices(owner string) ([]Device, error) {
	owner, err := normalizeEmail(owner)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,workspace_id,owner,name,created_at,last_seen FROM devices
		WHERE owner=? AND revoked_at IS NULL ORDER BY last_seen DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]Device, 0)
	for rows.Next() {
		var device Device
		var created, seen string
		if err = rows.Scan(&device.ID, &device.WorkspaceID, &device.Owner, &device.Name, &created, &seen); err != nil {
			return nil, err
		}
		device.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		device.LastSeen, _ = time.Parse(time.RFC3339Nano, seen)
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

// WorkspacesFor returns only workspaces reached through membership or an active device grant.
func (s *Store) WorkspacesFor(principal string) ([]Workspace, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return nil, err
	}
	if _, err = s.ensurePersonalWorkspace(context.Background(), principal); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT DISTINCT w.id,w.name,w.owner,
		CASE WHEN m.role IS NOT NULL THEN m.role ELSE 'guest' END,w.created_at
		FROM workspaces w
		LEFT JOIN memberships m ON m.workspace_id=w.id AND m.principal=?
		LEFT JOIN grants g ON g.workspace_id=w.id AND g.principal=? AND g.revoked_at IS NULL AND g.expires_at>?
		LEFT JOIN devices d ON d.id=g.device_id AND d.revoked_at IS NULL
		WHERE m.principal IS NOT NULL OR d.id IS NOT NULL ORDER BY w.created_at`,
		principal, principal, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Workspace{}
	for rows.Next() {
		var value Workspace
		var created string
		if err = rows.Scan(&value.ID, &value.Name, &value.Owner, &value.Role, &created); err != nil {
			return nil, err
		}
		value.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		values = append(values, value)
	}
	return values, rows.Err()
}

// SetMCPTarget persists an explicit eligible workspace/device target for one identity.
func (s *Store) SetMCPTarget(principal, workspaceID, deviceID string) error {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return err
	}
	var eligible int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM devices d JOIN grants g ON g.device_id=d.id AND g.workspace_id=d.workspace_id
		WHERE d.id=? AND d.workspace_id=? AND d.revoked_at IS NULL AND g.principal=?
		AND g.revoked_at IS NULL AND g.expires_at>?`, deviceID, workspaceID, principal, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&eligible)
	if err != nil {
		return err
	}
	if eligible != 1 {
		return errors.New("workspace device is not granted to this identity")
	}
	_, err = s.db.Exec(`INSERT INTO mcp_targets(principal,workspace_id,device_id,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(principal) DO UPDATE SET workspace_id=excluded.workspace_id,device_id=excluded.device_id,updated_at=excluded.updated_at`,
		principal, workspaceID, deviceID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ResolveMCPTarget fails closed when more than one eligible device lacks an explicit selection.
func (s *Store) ResolveMCPTarget(principal string) (MCPTarget, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return MCPTarget{}, err
	}
	devices, err := s.DevicesFor(principal)
	if err != nil {
		return MCPTarget{}, err
	}
	if len(devices) == 0 {
		return MCPTarget{}, errors.New("no connected device grants this identity access")
	}
	var selected MCPTarget
	err = s.db.QueryRow(`SELECT workspace_id,device_id FROM mcp_targets WHERE principal=?`, principal).Scan(&selected.WorkspaceID, &selected.DeviceID)
	if err == nil {
		for _, device := range devices {
			if device.ID == selected.DeviceID && device.WorkspaceID == selected.WorkspaceID {
				return selected, nil
			}
		}
		_, _ = s.db.Exec(`DELETE FROM mcp_targets WHERE principal=?`, principal)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return MCPTarget{}, err
	}
	if len(devices) != 1 {
		return MCPTarget{}, errors.New("multiple devices are available; choose an MCP target first")
	}
	selected = MCPTarget{WorkspaceID: devices[0].WorkspaceID, DeviceID: devices[0].ID}
	if err = s.SetMCPTarget(principal, selected.WorkspaceID, selected.DeviceID); err != nil {
		return MCPTarget{}, err
	}
	return selected, nil
}

func (s *Store) DeviceKey(deviceID string) (ed25519.PublicKey, error) {
	var key []byte
	if err := s.db.QueryRow(`SELECT public_key FROM devices WHERE id=? AND revoked_at IS NULL`, deviceID).Scan(&key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("device not found")
		}
		return nil, err
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, errors.New("stored device key is invalid")
	}
	return ed25519.PublicKey(key), nil
}

func (s *Store) RevokeDevice(deviceID, owner string) error {
	owner, err := normalizeEmail(owner)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`UPDATE devices SET revoked_at=? WHERE id=? AND owner=? AND revoked_at IS NULL`, now, deviceID, owner)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("device not found")
	}
	_, _ = s.db.Exec(`UPDATE grants SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, now, deviceID)
	_, _ = s.db.Exec(`DELETE FROM mcp_targets WHERE device_id=?`, deviceID)
	return nil
}

func (s *Store) Submit(principal, deviceID string, request protocol.Request) (Task, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Task{}, err
	}
	if err = request.Validate(); err != nil {
		return Task{}, err
	}
	if !protocol.Grantable(request.Operation) {
		return Task{}, errors.New("operation is not available to clients")
	}
	if err = s.authorize(deviceID, principal, request.Operation); err != nil {
		return Task{}, err
	}
	return s.queueTask(principal, deviceID, request)
}

func (s *Store) SubmitInvitation(principal, invitationID, token string, request protocol.Request) (Task, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Task{}, err
	}
	if err = request.Validate(); err != nil {
		return Task{}, err
	}
	invitation, err := s.authorizeInvitation(principal, invitationID, token, request)
	if err != nil {
		return Task{}, err
	}
	task, err := s.queueTask(principal, invitation.DeviceID, request)
	if err == nil && request.Operation == protocol.SessionGet {
		_, err = s.db.Exec(`UPDATE invitations SET last_viewed_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), invitation.ID)
	}
	return task, err
}

func (s *Store) queueTask(principal, deviceID string, request protocol.Request) (Task, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return Task{}, err
	}
	sealed, err := s.cipher.Seal(encoded)
	if err != nil {
		return Task{}, err
	}
	now := time.Now().UTC()
	var workspaceID string
	if err = s.db.QueryRow(`SELECT workspace_id FROM devices WHERE id=? AND revoked_at IS NULL`, deviceID).Scan(&workspaceID); err != nil {
		return Task{}, errors.New("active workspace device not found")
	}
	task := Task{ID: uuid.NewString(), WorkspaceID: workspaceID, DeviceID: deviceID, Principal: principal, Request: request, CreatedAt: now}
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var pending, principalPending int
	if err = tx.QueryRow(`SELECT count(*),COALESCE(sum(CASE WHEN principal=? THEN 1 ELSE 0 END),0) FROM tasks WHERE device_id=?`, principal, deviceID).Scan(&pending, &principalPending); err != nil {
		return Task{}, err
	}
	if pending >= 64 || principalPending >= 16 {
		return Task{}, errors.New("device request queue is busy; retry later")
	}
	_, err = tx.Exec(`INSERT INTO tasks(id,workspace_id,device_id,principal,request_json,status,created_at) VALUES(?,?,?,?,?,?,?)`,
		task.ID, workspaceID, deviceID, principal, sealed, "queued", now.Format(time.RFC3339Nano))
	if err != nil {
		return Task{}, err
	}
	// Audit and relay acceptance are atomic. No arguments, responses or bearer secrets enter this table.
	if _, err = tx.Exec(`INSERT INTO bridge_audit(id,workspace_id,device_id,principal,operation,status,created_at) VALUES(?,?,?,?,?,?,?)`, task.ID, workspaceID, deviceID, principal, string(request.Operation), "queued", now.Format(time.RFC3339Nano)); err != nil {
		return Task{}, err
	}
	return task, tx.Commit()
}

func (s *Store) Next(deviceID string) (Task, bool, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback()
	var task Task
	var requestJSON []byte
	var created string
	leaseCutoff := time.Now().UTC().Add(-claimLifetime).Format(time.RFC3339Nano)
	err = tx.QueryRow(`SELECT id,workspace_id,principal,request_json,created_at FROM tasks WHERE device_id=?
		AND (status='queued' OR (status='claimed' AND claimed_at<?)) ORDER BY created_at LIMIT 1`, deviceID, leaseCutoff).
		Scan(&task.ID, &task.WorkspaceID, &task.Principal, &requestJSON, &created)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.Exec(`UPDATE devices SET last_seen=? WHERE id=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339Nano), deviceID); err != nil {
			return Task{}, false, err
		}
		return Task{}, false, tx.Commit()
	}
	if err != nil {
		return Task{}, false, err
	}
	plainRequest, err := s.cipher.Open(requestJSON)
	if err != nil {
		return Task{}, false, err
	}
	if err = json.Unmarshal(plainRequest, &task.Request); err != nil {
		return Task{}, false, err
	}
	task.DeviceID = deviceID
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`UPDATE tasks SET status='claimed',claimed_at=? WHERE id=?`, now, task.ID); err != nil {
		return Task{}, false, err
	}
	if _, err = tx.Exec(`UPDATE devices SET last_seen=? WHERE id=?`, now, deviceID); err != nil {
		return Task{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Task{}, false, err
	}
	return task, true, nil
}

func (s *Store) Complete(deviceID, taskID string, responseJSON, signature []byte) error {
	if !json.Valid(responseJSON) {
		return errors.New("invalid response JSON")
	}
	var publicKey []byte
	var status string
	err := s.db.QueryRow(`SELECT d.public_key,t.status FROM tasks t JOIN devices d ON d.id=t.device_id
		WHERE t.id=? AND t.device_id=? AND d.revoked_at IS NULL`, taskID, deviceID).Scan(&publicKey, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("task not found")
	}
	if err != nil {
		return err
	}
	if status != "claimed" {
		return errors.New("task is not awaiting completion")
	}
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(publicKey), responseJSON, signature) {
		return errors.New("invalid device signature")
	}
	var response protocol.Response
	if err = json.Unmarshal(responseJSON, &response); err != nil {
		return errors.New("invalid protocol response")
	}
	var requestEnvelope []byte
	if err = s.db.QueryRow(`SELECT request_json FROM tasks WHERE id=?`, taskID).Scan(&requestEnvelope); err != nil {
		return err
	}
	requestJSON, err := s.cipher.Open(requestEnvelope)
	if err != nil {
		return err
	}
	var request protocol.Request
	if err = json.Unmarshal(requestJSON, &request); err != nil {
		return errors.New("invalid stored protocol request")
	}
	if definition, ok := protocol.DefinitionFor(request.Operation); !ok || len(responseJSON) > definition.MaximumResponseBytes {
		return errors.New("response exceeds the operation limit")
	}
	if response.Version != protocol.Version || response.RequestID != request.RequestID {
		return errors.New("response does not match request")
	}
	sealedResponse, err := s.cipher.Seal(responseJSON)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE tasks SET response_json=?,status='complete',completed_at=? WHERE id=? AND status='claimed'`,
		sealedResponse, time.Now().UTC().Format(time.RFC3339Nano), taskID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("task completion was replayed")
	}
	if _, err = tx.Exec(`UPDATE bridge_audit SET status=? WHERE id=?`, string(response.Status), taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) encryptLegacyTaskPayloads(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,request_json,response_json FROM tasks`)
	if err != nil {
		return err
	}
	type legacyTask struct {
		id       string
		request  []byte
		response []byte
	}
	values := []legacyTask{}
	for rows.Next() {
		var value legacyTask
		if err = rows.Scan(&value.id, &value.request, &value.response); err != nil {
			_ = rows.Close()
			return err
		}
		values = append(values, value)
	}
	_ = rows.Close()
	for _, value := range values {
		request := value.request
		response := value.response
		for _, encrypted := range [][]byte{request, response} {
			if s.cipher.IsEnvelope(encrypted) {
				if _, err = s.cipher.Open(encrypted); err != nil {
					return errors.New("bridge encryption key does not match stored payloads")
				}
			}
		}
		changed := false
		if len(request) > 0 && !s.cipher.IsEnvelope(request) {
			if !json.Valid(request) {
				return errors.New("legacy bridge task request is invalid")
			}
			request, err = s.cipher.Seal(request)
			if err != nil {
				return err
			}
			changed = true
		}
		if len(response) > 0 && !s.cipher.IsEnvelope(response) {
			if !json.Valid(response) {
				return errors.New("legacy bridge task response is invalid")
			}
			response, err = s.cipher.Seal(response)
			if err != nil {
				return err
			}
			changed = true
		}
		if changed {
			if _, err = s.db.ExecContext(ctx, `UPDATE tasks SET request_json=?,response_json=? WHERE id=?`, request, nullableBytes(response), value.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func (s *Store) Wait(ctx context.Context, taskID string, timeout time.Duration) (protocol.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status string
		var encoded []byte
		err := s.db.QueryRowContext(ctx, `SELECT status,response_json FROM tasks WHERE id=?`, taskID).Scan(&status, &encoded)
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Response{}, errors.New("task not found")
		}
		if err != nil {
			return protocol.Response{}, err
		}
		if status == "complete" {
			plainResponse, openErr := s.cipher.Open(encoded)
			if openErr != nil {
				return protocol.Response{}, openErr
			}
			var response protocol.Response
			if err = json.Unmarshal(plainResponse, &response); err != nil {
				return protocol.Response{}, err
			}
			_, _ = s.db.Exec(`DELETE FROM tasks WHERE id=?`, taskID)
			return response, nil
		}
		select {
		case <-ctx.Done():
			_, _ = s.db.Exec(`DELETE FROM tasks WHERE id=?`, taskID)
			return protocol.Response{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Store) authorize(deviceID, principal string, operation protocol.Operation) error {
	var encoded []byte
	var expires string
	err := s.db.QueryRow(`SELECT g.operations_json,g.expires_at FROM grants g JOIN devices d
		ON d.id=g.device_id AND d.workspace_id=g.workspace_id WHERE g.device_id=? AND g.principal=?
		AND g.revoked_at IS NULL AND d.revoked_at IS NULL`, deviceID, principal).Scan(&encoded, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("principal has no active grant for this device")
	}
	if err != nil {
		return err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !time.Now().Before(expiresAt) {
		return errors.New("principal grant has expired")
	}
	var operations []protocol.Operation
	if err = json.Unmarshal(encoded, &operations); err != nil {
		return err
	}
	for _, allowed := range operations {
		if allowed == operation {
			return nil
		}
	}
	return fmt.Errorf("operation %s is not granted", operation)
}

func (s *Store) authorizeInvitation(principal, invitationID, token string, request protocol.Request) (Invitation, error) {
	invitation, err := s.invitationSession(principal, invitationID, token)
	if err != nil {
		return Invitation{}, err
	}
	if !protocol.Grantable(request.Operation) || !operationIn(invitation.Operations, request.Operation) || !protocol.SessionScoped(request.Arguments, invitation.SessionID) {
		return Invitation{}, errors.New("invitation capability or session denied")
	}
	return invitation, nil
}

func (s *Store) invitationSession(principal, invitationID, token string) (Invitation, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Invitation{}, err
	}
	invitation, _, err := scanInvitation(s.db.QueryRow(`SELECT id,workspace_id,device_id,owner,invitee,session_id,role,operations_json,created_at,expires_at,accepted_at,revoked_at,last_viewed_at,''
		FROM invitations WHERE id=? AND invitee=? AND session_token_hash=?`, invitationID, principal, hashSecret(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, errors.New("invalid invitation session")
	}
	if err != nil {
		return Invitation{}, err
	}
	if invitation.AcceptedAt == nil || invitation.RevokedAt != nil || !time.Now().Before(invitation.ExpiresAt) {
		return Invitation{}, errors.New("invitation session is expired or revoked")
	}
	return invitation, nil
}

func operationIn(operations []protocol.Operation, operation protocol.Operation) bool {
	for _, allowed := range operations {
		if allowed == operation {
			return true
		}
	}
	return false
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || !strings.Contains(value, "@") {
		return "", errors.New("invalid email address")
	}
	return value, nil
}

func validateOperations(operations []protocol.Operation) ([]protocol.Operation, error) {
	if len(operations) == 0 {
		return nil, errors.New("grant requires at least one operation")
	}
	seen := make(map[protocol.Operation]struct{}, len(operations))
	result := make([]protocol.Operation, 0, len(operations))
	for _, operation := range operations {
		if !protocol.Grantable(operation) {
			return nil, fmt.Errorf("unknown operation %q", operation)
		}
		if _, ok := seen[operation]; ok {
			continue
		}
		seen[operation] = struct{}{}
		result = append(result, operation)
	}
	return result, nil
}

func randomCode() (string, error) {
	buffer := make([]byte, 15)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer)
	return encoded[:8] + "-" + encoded[8:16] + "-" + encoded[16:], nil
}

func hashCode(code string) string {
	digest := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(digest[:])
}

func randomSessionToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashSecret(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}
