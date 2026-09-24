// @module BridgeSupport relays code-based support connections: the source device issues the code,
// a signed-in supporter enters it, and the device itself verifies the code before access starts.
package bridge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
	"github.com/google/uuid"
)

const (
	maxWaitingSupportCodes = 20
	minSupportMinutes      = 5
	maxSupportMinutes      = 8 * 60
	supportCodeWindow      = 11 * time.Minute
)

var supportIDPattern = regexp.MustCompile(`^support_[a-f0-9]{16,64}$`)

// SupportSession is the bridge's routing record for one support code.
type SupportSession struct {
	ID              string               `json:"id"`
	WorkspaceID     string               `json:"workspace_id"`
	DeviceID        string               `json:"device_id"`
	SessionID       string               `json:"session_id"`
	Role            string               `json:"role"`
	Operations      []protocol.Operation `json:"operations"`
	CreatedAt       time.Time            `json:"created_at"`
	CodeExpiresAt   time.Time            `json:"code_expires_at"`
	AccessMinutes   int                  `json:"access_minutes"`
	Supporter       string               `json:"supporter,omitempty"`
	ConnectedAt     *time.Time           `json:"connected_at,omitempty"`
	AccessExpiresAt *time.Time           `json:"access_expires_at,omitempty"`
	LastViewedAt    *time.Time           `json:"last_viewed_at,omitempty"`
	RevokedAt       *time.Time           `json:"revoked_at,omitempty"`
	EndReason       string               `json:"end_reason,omitempty"`
	DeviceName      string               `json:"device_name,omitempty"`
}

const supportColumns = `s.id,s.workspace_id,s.device_id,s.session_id,s.role,s.operations_json,s.created_at,s.code_expires_at,s.access_minutes,
	s.supporter,s.connected_at,s.access_expires_at,s.last_viewed_at,s.revoked_at,s.end_reason,d.name`

func scanSupport(row rowScanner) (SupportSession, error) {
	var value SupportSession
	var operations []byte
	var created, codeExpires string
	var supporter, connected, accessExpires, viewed, revoked, reason sql.NullString
	if err := row.Scan(&value.ID, &value.WorkspaceID, &value.DeviceID, &value.SessionID, &value.Role, &operations, &created, &codeExpires,
		&value.AccessMinutes, &supporter, &connected, &accessExpires, &viewed, &revoked, &reason, &value.DeviceName); err != nil {
		return SupportSession{}, err
	}
	if err := json.Unmarshal(operations, &value.Operations); err != nil {
		return SupportSession{}, err
	}
	value.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	value.CodeExpiresAt, _ = time.Parse(time.RFC3339Nano, codeExpires)
	value.Supporter, value.EndReason = supporter.String, reason.String
	value.ConnectedAt, value.AccessExpiresAt = optionalTime(connected), optionalTime(accessExpires)
	value.LastViewedAt, value.RevokedAt = optionalTime(viewed), optionalTime(revoked)
	return value, nil
}

func optionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

// RegisterSupport records a code the device generated for one session.
func (s *Store) RegisterSupport(deviceID, id, codeHash, sessionID, role string, codeExpiresAt time.Time, accessMinutes int) (SupportSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	switch {
	case !supportIDPattern.MatchString(id):
		return SupportSession{}, errors.New("invalid support session id")
	case !supportcode.ValidHash(codeHash):
		return SupportSession{}, errors.New("invalid support code hash")
	case sessionID == "" || len(sessionID) > 256:
		return SupportSession{}, errors.New("valid session is required")
	case accessMinutes < minSupportMinutes || accessMinutes > maxSupportMinutes:
		return SupportSession{}, errors.New("support access must last between 5 minutes and 8 hours")
	}
	operations, ok := protocol.InvitationOperations(role)
	if !ok {
		return SupportSession{}, errors.New("invalid support role")
	}
	now := time.Now().UTC()
	codeExpiresAt = codeExpiresAt.UTC()
	if !codeExpiresAt.After(now) || codeExpiresAt.After(now.Add(supportCodeWindow)) {
		return SupportSession{}, errors.New("support code expiry must be within ten minutes")
	}
	var waiting int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM support_sessions WHERE device_id=? AND connected_at IS NULL AND revoked_at IS NULL AND code_expires_at>?`,
		deviceID, now.Format(time.RFC3339Nano)).Scan(&waiting); err != nil {
		return SupportSession{}, err
	}
	if waiting >= maxWaitingSupportCodes {
		return SupportSession{}, errors.New("too many waiting support codes; end or let one expire")
	}
	encoded, _ := json.Marshal(operations)
	result, err := s.db.Exec(`INSERT INTO support_sessions(id,workspace_id,device_id,code_hash,session_id,role,operations_json,created_at,code_expires_at,access_minutes)
		SELECT ?,workspace_id,id,?,?,?,?,?,?,? FROM devices WHERE id=? AND revoked_at IS NULL`,
		id, codeHash, sessionID, role, encoded, now.Format(time.RFC3339Nano), codeExpiresAt.Format(time.RFC3339Nano), accessMinutes, deviceID)
	if err != nil {
		return SupportSession{}, fmt.Errorf("register support code: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return SupportSession{}, errors.New("active device not found")
	}
	return s.supportByID(id)
}

func (s *Store) supportByID(id string) (SupportSession, error) {
	value, err := scanSupport(s.db.QueryRow(`SELECT `+supportColumns+` FROM support_sessions s JOIN devices d ON d.id=s.device_id WHERE s.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return SupportSession{}, errors.New("support session not found")
	}
	return value, err
}

// SupportForDevice lists a device's support records for its owner UI.
func (s *Store) SupportForDevice(deviceID string) ([]SupportSession, error) {
	rows, err := s.db.Query(`SELECT `+supportColumns+` FROM support_sessions s JOIN devices d ON d.id=s.device_id WHERE s.device_id=? ORDER BY s.created_at DESC LIMIT 200`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]SupportSession, 0)
	for rows.Next() {
		value, scanErr := scanSupport(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// RevokeSupport ends a code or connection on behalf of the issuing device.
func (s *Store) RevokeSupport(deviceID, id, reason string) error {
	if reason == "" {
		reason = "ended_by_owner"
	}
	result, err := s.db.Exec(`UPDATE support_sessions SET revoked_at=?,end_reason=?,token_hash=NULL WHERE id=? AND device_id=? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), reason, id, deviceID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("support session not found")
	}
	return nil
}

// FindWaitingSupport resolves an entered code to a waiting support session.
// A miss reveals nothing about which codes exist.
func (s *Store) FindWaitingSupport(code string) (SupportSession, error) {
	codeHash, err := supportcode.Hash(supportcode.Support, code)
	if err != nil {
		return SupportSession{}, errors.New("support code is not valid")
	}
	value, err := scanSupport(s.db.QueryRow(`SELECT `+supportColumns+` FROM support_sessions s JOIN devices d ON d.id=s.device_id
		WHERE s.code_hash=? AND d.revoked_at IS NULL`, codeHash))
	if err != nil {
		return SupportSession{}, errors.New("support code is not valid")
	}
	if value.ConnectedAt != nil || value.RevokedAt != nil || !time.Now().Before(value.CodeExpiresAt) {
		return SupportSession{}, errors.New("support code is not valid")
	}
	return value, nil
}

// QueueSupportRedeem asks the issuing device to verify the code itself. The
// request carries the code the supporter typed; like every relay payload it
// is envelope-encrypted at rest and deleted after completion or timeout.
func (s *Store) QueueSupportRedeem(principal string, support SupportSession, code string) (Task, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return Task{}, err
	}
	arguments, _ := json.Marshal(map[string]string{"support_id": support.ID, "code": code})
	request := protocol.Request{Version: protocol.Version, RequestID: "support_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Operation: protocol.SupportRedeem, Arguments: arguments}
	if err = request.Validate(); err != nil {
		return Task{}, err
	}
	return s.queueTask(principal, support.DeviceID, request)
}

// ActivateSupport binds a device-verified redemption to the supporter and
// stores only the hash of the browser session token.
func (s *Store) ActivateSupport(id, principal string, response protocol.Response) (SupportSession, string, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return SupportSession{}, "", err
	}
	if response.Status != protocol.StatusOK {
		message := "the source device refused the code"
		if response.Error != nil && response.Error.Message != "" {
			message = response.Error.Message
		}
		return SupportSession{}, "", errors.New(message)
	}
	encoded, _ := json.Marshal(response.Data)
	var confirmed struct {
		SupportID string    `json:"id"`
		SessionID string    `json:"session_id"`
		Role      string    `json:"role"`
		Supporter string    `json:"supporter"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err = json.Unmarshal(encoded, &confirmed); err != nil {
		return SupportSession{}, "", errors.New("the source device returned an invalid confirmation")
	}
	support, err := s.supportByID(id)
	if err != nil {
		return SupportSession{}, "", err
	}
	now := time.Now().UTC()
	limit := now.Add(time.Duration(support.AccessMinutes)*time.Minute + time.Minute)
	if confirmed.SupportID != support.ID || confirmed.SessionID != support.SessionID || confirmed.Role != support.Role ||
		confirmed.Supporter != principal || !confirmed.ExpiresAt.After(now) || confirmed.ExpiresAt.After(limit) {
		return SupportSession{}, "", errors.New("the source device confirmation does not match this code")
	}
	token, err := randomSessionToken()
	if err != nil {
		return SupportSession{}, "", err
	}
	result, err := s.db.Exec(`UPDATE support_sessions SET supporter=?,connected_at=?,access_expires_at=?,token_hash=?
		WHERE id=? AND connected_at IS NULL AND revoked_at IS NULL`, principal, now.Format(time.RFC3339Nano),
		confirmed.ExpiresAt.UTC().Format(time.RFC3339Nano), hashSecret(token), id)
	if err != nil {
		return SupportSession{}, "", err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return SupportSession{}, "", errors.New("support code was already used")
	}
	support, err = s.supportByID(id)
	return support, token, err
}

// supportSession authenticates the supporter's browser session for one record.
func (s *Store) supportSession(principal, id, token string) (SupportSession, error) {
	principal, err := normalizeEmail(principal)
	if err != nil {
		return SupportSession{}, err
	}
	value, err := scanSupport(s.db.QueryRow(`SELECT `+supportColumns+` FROM support_sessions s JOIN devices d ON d.id=s.device_id
		WHERE s.id=? AND s.supporter=? AND s.token_hash=? AND d.revoked_at IS NULL`, id, principal, hashSecret(token)))
	if err != nil {
		return SupportSession{}, errors.New("invalid support session")
	}
	if value.RevokedAt != nil || value.AccessExpiresAt == nil || !time.Now().Before(*value.AccessExpiresAt) {
		return SupportSession{}, errors.New("support session has ended")
	}
	return value, nil
}

// SubmitSupport queues one supporter request after checking role and session scope.
func (s *Store) SubmitSupport(principal, id, token string, request protocol.Request) (Task, error) {
	if err := request.Validate(); err != nil {
		return Task{}, err
	}
	support, err := s.supportSession(principal, id, token)
	if err != nil {
		return Task{}, err
	}
	if !protocol.Grantable(request.Operation) || !operationIn(support.Operations, request.Operation) || !protocol.SessionScoped(request.Arguments, support.SessionID) {
		return Task{}, errors.New("support capability or session denied")
	}
	task, err := s.queueTask(support.Supporter, support.DeviceID, request)
	if err == nil && request.Operation == protocol.SessionGet {
		_, err = s.db.Exec(`UPDATE support_sessions SET last_viewed_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id)
	}
	return task, err
}

// EndSupportBySupporter lets the connected person leave; the device learns of it on its next sync.
func (s *Store) EndSupportBySupporter(principal, id, token string) error {
	support, err := s.supportSession(principal, id, token)
	if err != nil {
		return err
	}
	return s.RevokeSupport(support.DeviceID, support.ID, "supporter_disconnected")
}
