// @module RemoteSupport lets the source machine issue a short code that connects one person to one session.
package remote

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/supportcode"
)

const (
	// SupportCodeLifetime bounds how long a code can wait to be entered.
	SupportCodeLifetime = 10 * time.Minute
	// MinimumSupportAccess and MaximumSupportAccess bound one connection.
	MinimumSupportAccess = 5 * time.Minute
	MaximumSupportAccess = 8 * time.Hour
	maxSupportAttempts   = 5
	supportGrantMethod   = "support"
)

// SupportSession is the source machine's record of one code-based connection.
// The code itself is shown once and only its hash is retained.
type SupportSession struct {
	ID             string               `json:"id"`
	SessionID      string               `json:"session_id"`
	SessionTitle   string               `json:"session_title,omitempty"`
	Role           string               `json:"role"`
	Operations     []protocol.Operation `json:"operations"`
	CreatedAt      time.Time            `json:"created_at"`
	CodeExpiresAt  time.Time            `json:"code_expires_at"`
	AccessMinutes  int                  `json:"access_minutes"`
	Supporter      string               `json:"supporter,omitempty"`
	ConnectedAt    *time.Time           `json:"connected_at,omitempty"`
	ExpiresAt      *time.Time           `json:"expires_at,omitempty"`
	LastActivityAt *time.Time           `json:"last_activity_at,omitempty"`
	GrantID        string               `json:"grant_id,omitempty"`
	FailedAttempts int                  `json:"failed_attempts"`
	EndedAt        *time.Time           `json:"ended_at,omitempty"`
	EndReason      string               `json:"end_reason,omitempty"`
	Status         string               `json:"status"`
	codeHash       string
}

type storedSupport struct {
	SupportSession
	CodeHash string `json:"code_hash"`
}

func storedSupportFrom(value SupportSession) storedSupport {
	value.Status = ""
	return storedSupport{SupportSession: value, CodeHash: value.codeHash}
}

func (value storedSupport) session() SupportSession {
	session := value.SupportSession
	session.codeHash = value.CodeHash
	return session
}

// supportStatus derives the lifecycle state shown to the source owner.
func supportStatus(value SupportSession, now time.Time) string {
	switch {
	case value.EndedAt != nil:
		return "ended"
	case value.ConnectedAt != nil && value.ExpiresAt != nil && !now.Before(*value.ExpiresAt):
		return "expired"
	case value.ConnectedAt != nil:
		return "connected"
	case !now.Before(value.CodeExpiresAt):
		return "expired"
	default:
		return "waiting"
	}
}

// CreateSupportSession issues a single-use code for exactly one session.
func (m *Manager) CreateSupportSession(sessionID, title, role string, access time.Duration) (SupportSession, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncLocked()
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 256 {
		return SupportSession{}, "", errors.New("a session is required")
	}
	operations, ok := protocol.InvitationOperations(role)
	if !ok {
		return SupportSession{}, "", errors.New("role must be view, comment, or review")
	}
	if access < MinimumSupportAccess || access > MaximumSupportAccess {
		return SupportSession{}, "", errors.New("support access must last between 5 minutes and 8 hours")
	}
	code, err := supportcode.Generate()
	if err != nil {
		return SupportSession{}, "", err
	}
	codeHash, err := supportcode.Hash(supportcode.Support, code)
	if err != nil {
		return SupportSession{}, "", err
	}
	identifier := make([]byte, 16)
	if _, err = rand.Read(identifier); err != nil {
		return SupportSession{}, "", err
	}
	now := m.now()
	value := SupportSession{
		ID: "support_" + hex.EncodeToString(identifier), SessionID: sessionID, SessionTitle: redact.Title(title, 80),
		Role: role, Operations: operations, CreatedAt: now, CodeExpiresAt: now.Add(SupportCodeLifetime),
		AccessMinutes: int(access / time.Minute), codeHash: codeHash,
	}
	m.state.Support = append(m.state.Support, value)
	if err = m.persist(); err != nil {
		m.state.Support = m.state.Support[:len(m.state.Support)-1]
		return SupportSession{}, "", err
	}
	value.Status = supportStatus(value, now)
	return value, code, nil
}

// SupportCodeHash exposes the stored hash for bridge registration.
func (m *Manager) SupportCodeHash(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncLocked()
	if index := m.supportIndex(id); index >= 0 {
		return m.state.Support[index].codeHash, nil
	}
	return "", errors.New("support session not found")
}

// RedeemSupport verifies a presented code end to end on the source machine and
// binds the connection to the supporter's verified identity. The bridge only
// relays the attempt; it cannot redeem a code without knowing it.
func (m *Manager) RedeemSupport(id, code, principal string) (SupportSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncLocked()
	if m.stateErr != nil {
		return SupportSession{}, errors.New("remote state is unavailable")
	}
	principal, err := normalizePrincipal(principal)
	if err != nil {
		return SupportSession{}, err
	}
	index := m.supportIndex(id)
	if index < 0 {
		return SupportSession{}, errors.New("support code is not valid")
	}
	value := &m.state.Support[index]
	now := m.now()
	if supportStatus(*value, now) != "waiting" {
		return SupportSession{}, errors.New("support code is expired, used, or ended")
	}
	if !supportcode.Matches(supportcode.Support, code, value.codeHash) {
		value.FailedAttempts++
		if value.FailedAttempts >= maxSupportAttempts {
			value.EndedAt, value.EndReason = &now, "too_many_attempts"
		}
		_ = m.persist()
		return SupportSession{}, errors.New("support code is not valid")
	}
	expires := now.Add(time.Duration(value.AccessMinutes) * time.Minute)
	grant := Grant{
		ID: "grant_" + shortHash(value.ID+principal), Reviewer: principal, AuthMethod: supportGrantMethod,
		Principal: principal, Role: value.Role, SessionIDs: []string{value.SessionID},
		Operations: append([]protocol.Operation(nil), value.Operations...), CreatedAt: now, ExpiresAt: expires,
	}
	m.state.Grants = append(m.state.Grants, grant)
	m.state.Connections = append(m.state.Connections, Connection{ID: "connection_" + shortHash(grant.ID), GrantID: grant.ID, Reviewer: principal, ConnectedAt: now, LastSeenAt: now, Active: true})
	value.Supporter, value.ConnectedAt, value.ExpiresAt, value.GrantID = principal, &now, &expires, grant.ID
	if err = m.persist(); err != nil {
		return SupportSession{}, err
	}
	result := *value
	result.Status = supportStatus(result, now)
	return result, nil
}

// EndSupport ends a code or connection immediately and revokes its grant.
func (m *Manager) EndSupport(id, reason string) (SupportSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncLocked()
	index := m.supportIndex(id)
	if index < 0 {
		return SupportSession{}, errors.New("support session not found")
	}
	value := &m.state.Support[index]
	now := m.now()
	if value.EndedAt == nil {
		if reason == "" {
			reason = "ended_by_owner"
		}
		value.EndedAt, value.EndReason = &now, reason
	}
	m.revokeGrantLocked(value.GrantID)
	if err := m.persist(); err != nil {
		return SupportSession{}, err
	}
	result := *value
	result.Status = supportStatus(result, now)
	return result, nil
}

// SupportSessions returns support records newest first with live status.
func (m *Manager) SupportSessions() []SupportSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncLocked()
	now := m.now()
	values := make([]SupportSession, 0, len(m.state.Support))
	for _, value := range m.state.Support {
		value.Status = supportStatus(value, now)
		for _, connection := range m.state.Connections {
			if value.GrantID != "" && connection.GrantID == value.GrantID {
				seen := connection.LastSeenAt
				value.LastActivityAt = &seen
			}
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.After(values[j].CreatedAt) })
	return values
}

func (m *Manager) supportIndex(id string) int {
	for index := range m.state.Support {
		if m.state.Support[index].ID == id {
			return index
		}
	}
	return -1
}

func (m *Manager) revokeGrantLocked(grantID string) {
	if grantID == "" {
		return
	}
	for index := range m.state.Grants {
		if m.state.Grants[index].ID == grantID {
			m.state.Grants[index].Revoked = true
		}
	}
	for index := range m.state.Connections {
		if m.state.Connections[index].GrantID == grantID {
			m.state.Connections[index].Active = false
		}
	}
}

// endSupportIfActive adopts an end recorded elsewhere (for example the
// supporter leaving on the bridge) without rewriting an existing end.
func (m *Manager) endSupportIfActive(id, reason string) (SupportSession, error) {
	m.mu.Lock()
	index := m.supportIndex(id)
	ended := index >= 0 && m.state.Support[index].EndedAt != nil
	m.mu.Unlock()
	if index < 0 {
		return SupportSession{}, errors.New("support session not found")
	}
	if ended {
		return SupportSession{}, nil
	}
	return m.EndSupport(id, reason)
}
