// @module RemoteManager persists pairing, grants, connections, audits, annotations, and reviews.
package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	protocol "github.com/LanceSandino/sessionsift/src/internal/protocol/v1"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

type Pairing struct {
	Code      string               `json:"code"`
	DeviceID  string               `json:"device_id"`
	Reviewer  string               `json:"reviewer,omitempty"`
	Requested []protocol.Operation `json:"requested,omitempty"`
	CreatedAt time.Time            `json:"created_at"`
	ExpiresAt time.Time            `json:"expires_at"`
	Claimed   bool                 `json:"claimed"`
	Approved  bool                 `json:"approved"`
	Used      bool                 `json:"used"`
	Denied    bool                 `json:"denied"`
	Attempts  int                  `json:"attempts"`
}
type Grant struct {
	ID         string               `json:"id"`
	TokenHash  string               `json:"-"`
	Reviewer   string               `json:"reviewer"`
	AuthMethod string               `json:"auth_method,omitempty"`
	Principal  string               `json:"principal,omitempty"`
	Operations []protocol.Operation `json:"operations"`
	CreatedAt  time.Time            `json:"created_at"`
	ExpiresAt  time.Time            `json:"expires_at"`
	Revoked    bool                 `json:"revoked"`
}
type Connection struct {
	ID          string    `json:"id"`
	GrantID     string    `json:"grant_id"`
	Reviewer    string    `json:"reviewer"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Active      bool      `json:"active"`
}
type AuditRecord struct {
	Timestamp     time.Time          `json:"timestamp"`
	ClientID      string             `json:"client_id"`
	GrantID       string             `json:"grant_id,omitempty"`
	Operation     protocol.Operation `json:"operation"`
	TargetID      string             `json:"target_id,omitempty"`
	Allowed       bool               `json:"allowed"`
	ResponseBytes int                `json:"response_bytes"`
	Status        string             `json:"status"`
}
type Annotation struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TurnID    string    `json:"turn_id,omitempty"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}
type Review struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Reviewer  string    `json:"reviewer"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type managerState struct {
	Pairings    []Pairing     `json:"pairings"`
	Grants      []Grant       `json:"grants"`
	Connections []Connection  `json:"connections"`
	Audit       []AuditRecord `json:"audit"`
	Annotations []Annotation  `json:"annotations"`
	Reviews     []Review      `json:"reviews"`
	BadAttempts int           `json:"bad_attempts"`
}

type storedGrant struct {
	ID         string               `json:"id"`
	TokenHash  string               `json:"token_hash,omitempty"`
	Reviewer   string               `json:"reviewer"`
	AuthMethod string               `json:"auth_method,omitempty"`
	Principal  string               `json:"principal,omitempty"`
	Operations []protocol.Operation `json:"operations"`
	CreatedAt  time.Time            `json:"created_at"`
	ExpiresAt  time.Time            `json:"expires_at"`
	Revoked    bool                 `json:"revoked"`
}

type storedManagerState struct {
	Pairings    []Pairing     `json:"pairings"`
	Grants      []storedGrant `json:"grants"`
	Connections []Connection  `json:"connections"`
	Audit       []AuditRecord `json:"audit"`
	Annotations []Annotation  `json:"annotations"`
	Reviews     []Review      `json:"reviews"`
	BadAttempts int           `json:"bad_attempts"`
}
type Manager struct {
	mu           sync.Mutex
	path         string
	state        managerState
	now          func() time.Time
	claimBuckets map[string]rateBucket
	grantBuckets map[string]rateBucket
}

type rateBucket struct {
	Started time.Time
	Count   int
}

func OpenManager(path string) (*Manager, error) {
	m := &Manager{path: path, now: func() time.Time { return time.Now().UTC() }, claimBuckets: map[string]rateBucket{}, grantBuckets: map[string]rateBucket{}}
	body, err := os.ReadFile(path)
	if err == nil && len(body) > 0 {
		var stored storedManagerState
		if json.Unmarshal(body, &stored) != nil {
			return nil, errors.New("invalid remote state")
		}
		m.state = managerState{Pairings: stored.Pairings, Connections: stored.Connections, Audit: stored.Audit, Annotations: stored.Annotations, Reviews: stored.Reviews, BadAttempts: stored.BadAttempts}
		for _, grant := range stored.Grants {
			m.state.Grants = append(m.state.Grants, Grant(grant))
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return m, nil
}

func (m *Manager) CreatePairing(deviceID string) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if deviceID == "" {
		return Pairing{}, errors.New("device id required")
	}
	now := m.now()
	code, err := humanCode()
	if err != nil {
		return Pairing{}, err
	}
	pairing := Pairing{Code: code, DeviceID: deviceID, CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	m.state.Pairings = append(m.state.Pairings, pairing)
	return pairing, m.persist()
}
func (m *Manager) ClaimPairing(code, reviewer string, requested []protocol.Operation) (Pairing, error) {
	return m.ClaimPairingFrom(code, reviewer, requested, "local")
}

func (m *Manager) ClaimPairingFrom(code, reviewer string, requested []protocol.Operation, source string) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.allow(m.claimBuckets, source, 5, 10*time.Minute) {
		return Pairing{}, errors.New("pairing attempts temporarily locked")
	}
	index := m.pairingIndex(code)
	if index < 0 {
		return Pairing{}, errors.New("invalid pairing code")
	}
	pairing := &m.state.Pairings[index]
	pairing.Attempts++
	if pairing.Attempts > 5 {
		_ = m.persist()
		return Pairing{}, errors.New("pairing attempts temporarily locked")
	}
	if pairing.Used || pairing.Approved || pairing.Denied || m.now().After(pairing.ExpiresAt) {
		return Pairing{}, errors.New("pairing code expired or used")
	}
	if reviewer == "" || len(requested) == 0 {
		return Pairing{}, errors.New("reviewer and capabilities required")
	}
	for _, operation := range requested {
		if _, ok := protocol.DefinitionFor(operation); !ok {
			return Pairing{}, errors.New("capability is not allowlisted")
		}
	}
	pairing.Reviewer = reviewer
	pairing.Requested = append([]protocol.Operation(nil), requested...)
	pairing.Claimed = true
	m.state.BadAttempts = 0
	return *pairing, m.persist()
}

func (m *Manager) DenyPairing(code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.pairingIndex(code)
	if index < 0 {
		return errors.New("pairing not found")
	}
	pairing := &m.state.Pairings[index]
	if !pairing.Claimed || pairing.Approved || pairing.Used || pairing.Denied || m.now().After(pairing.ExpiresAt) {
		return errors.New("pairing is not deniable")
	}
	pairing.Denied = true
	pairing.Used = true
	return m.persist()
}
func (m *Manager) ApprovePairing(code string, duration time.Duration) (Grant, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.pairingIndex(code)
	if index < 0 {
		return Grant{}, "", errors.New("pairing not found")
	}
	pairing := &m.state.Pairings[index]
	if !pairing.Claimed || pairing.Approved || pairing.Used || pairing.Denied || m.now().After(pairing.ExpiresAt) {
		return Grant{}, "", errors.New("pairing is not approvable")
	}
	if duration <= 0 || duration > 24*time.Hour {
		return Grant{}, "", errors.New("grant duration must be within 24 hours")
	}
	token, err := randomToken(32)
	if err != nil {
		return Grant{}, "", err
	}
	now := m.now()
	grant := Grant{ID: "grant_" + shortHash(token), TokenHash: hash(token), Reviewer: pairing.Reviewer, AuthMethod: "bearer", Operations: pairing.Requested, CreatedAt: now, ExpiresAt: now.Add(duration)}
	connection := Connection{ID: "connection_" + shortHash(token+pairing.Code), GrantID: grant.ID, Reviewer: grant.Reviewer, ConnectedAt: now, LastSeenAt: now, Active: true}
	m.state.Grants = append(m.state.Grants, grant)
	m.state.Connections = append(m.state.Connections, connection)
	pairing.Approved = true
	pairing.Used = true
	return grant, token, m.persist()
}

// ApproveOAuthPrincipal creates a local capability grant for an identity already authenticated by Access.
func (m *Manager) ApproveOAuthPrincipal(principal string, operations []protocol.Operation, duration time.Duration) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	principal, err := normalizePrincipal(principal)
	if err != nil {
		return Grant{}, err
	}
	if duration <= 0 || duration > 30*24*time.Hour {
		return Grant{}, errors.New("OAuth grant duration must be within 30 days")
	}
	if len(operations) == 0 {
		return Grant{}, errors.New("at least one capability is required")
	}
	for _, operation := range operations {
		if _, ok := protocol.DefinitionFor(operation); !ok {
			return Grant{}, errors.New("capability is not allowlisted")
		}
	}
	now := m.now()
	seed, err := randomToken(32)
	if err != nil {
		return Grant{}, err
	}
	grant := Grant{ID: "grant_" + shortHash(principal+seed), Reviewer: principal, AuthMethod: "oauth", Principal: principal, Operations: append([]protocol.Operation(nil), operations...), CreatedAt: now, ExpiresAt: now.Add(duration)}
	connection := Connection{ID: "connection_" + shortHash(grant.ID), GrantID: grant.ID, Reviewer: principal, ConnectedAt: now, LastSeenAt: now, Active: true}
	m.state.Grants = append(m.state.Grants, grant)
	m.state.Connections = append(m.state.Connections, connection)
	return grant, m.persist()
}
func (m *Manager) Authorize(_ context.Context, token string, operation protocol.Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	grant, err := m.authenticateLocked(token)
	if err != nil {
		return err
	}
	for index := range m.state.Grants {
		if m.state.Grants[index].ID != grant.ID {
			continue
		}
		grant := &m.state.Grants[index]
		for _, allowed := range grant.Operations {
			if allowed == operation {
				m.touchConnection(grant.ID)
				_ = m.persist()
				return nil
			}
		}
		return errors.New("capability denied")
	}
	return errors.New("invalid grant")
}

// AuthorizePrincipal applies a locally approved OAuth identity grant.
func (m *Manager) AuthorizePrincipal(_ context.Context, principal string, operation protocol.Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	grant, err := m.principalGrantLocked(principal)
	if err != nil {
		return err
	}
	for _, allowed := range grant.Operations {
		if allowed == operation {
			m.touchConnection(grant.ID)
			_ = m.persist()
			return nil
		}
	}
	return errors.New("capability denied")
}

func (m *Manager) Authenticate(token string) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.authenticateLocked(token)
}

func (m *Manager) AllowRequest(token string) (Grant, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	grant, err := m.authenticateLocked(token)
	if err != nil {
		return Grant{}, 0, err
	}
	if !m.allow(m.grantBuckets, grant.ID, 120, time.Minute) {
		bucket := m.grantBuckets[grant.ID]
		return grant, time.Until(bucket.Started.Add(time.Minute)), errors.New("rate limit exceeded")
	}
	return grant, 0, nil
}

// AllowPrincipalRequest authenticates and rate limits an OAuth identity.
func (m *Manager) AllowPrincipalRequest(principal string) (Grant, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	grant, err := m.principalGrantLocked(principal)
	if err != nil {
		return Grant{}, 0, err
	}
	if !m.allow(m.grantBuckets, grant.ID, 120, time.Minute) {
		bucket := m.grantBuckets[grant.ID]
		return grant, time.Until(bucket.Started.Add(time.Minute)), errors.New("rate limit exceeded")
	}
	return grant, 0, nil
}
func (m *Manager) GrantForToken(token string) (Grant, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, grant := range m.state.Grants {
		if constantHashEqual(grant.TokenHash, hash(token)) {
			return grant, true
		}
	}
	return Grant{}, false
}

// OAuthGrants returns OAuth-backed grants without bearer token material.
func (m *Manager) OAuthGrants() []Grant {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := make([]Grant, 0)
	for _, grant := range m.state.Grants {
		if grant.AuthMethod == "oauth" {
			values = append(values, grant)
		}
	}
	return values
}
func (m *Manager) Revoke(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index := range m.state.Grants {
		if m.state.Grants[index].ID == id {
			m.state.Grants[index].Revoked = true
			for c := range m.state.Connections {
				if m.state.Connections[c].GrantID == id {
					m.state.Connections[c].Active = false
				}
			}
			return m.persist()
		}
	}
	return errors.New("grant not found")
}
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for index := range m.state.Connections {
		if m.state.Connections[index].ID == id {
			m.state.Connections[index].Active = false
			for grantIndex := range m.state.Grants {
				if m.state.Grants[grantIndex].ID == m.state.Connections[index].GrantID {
					m.state.Grants[grantIndex].Revoked = true
				}
			}
			return m.persist()
		}
	}
	return errors.New("connection not found")
}
func (m *Manager) Connections() []Connection {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Connection{}, m.state.Connections...)
}
func (m *Manager) PendingPairings() []Pairing {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := make([]Pairing, 0)
	for _, pairing := range m.state.Pairings {
		if pairing.Claimed && !pairing.Approved && !pairing.Used && !pairing.Denied && !m.now().After(pairing.ExpiresAt) {
			values = append(values, pairing)
		}
	}
	return values
}

func (m *Manager) authenticateLocked(token string) (Grant, error) {
	tokenHash := hash(token)
	for _, grant := range m.state.Grants {
		if !constantHashEqual(grant.TokenHash, tokenHash) {
			continue
		}
		if grant.Revoked || m.now().After(grant.ExpiresAt) {
			return Grant{}, errors.New("grant expired or revoked")
		}
		return grant, nil
	}
	return Grant{}, errors.New("invalid grant")
}

func (m *Manager) principalGrantLocked(principal string) (Grant, error) {
	principal, err := normalizePrincipal(principal)
	if err != nil {
		return Grant{}, err
	}
	for index := len(m.state.Grants) - 1; index >= 0; index-- {
		grant := m.state.Grants[index]
		if grant.AuthMethod != "oauth" || grant.Principal != principal {
			continue
		}
		if grant.Revoked || m.now().After(grant.ExpiresAt) {
			return Grant{}, errors.New("grant expired or revoked")
		}
		return grant, nil
	}
	return Grant{}, errors.New("OAuth principal is not locally approved")
}

func (m *Manager) allow(buckets map[string]rateBucket, key string, maximum int, window time.Duration) bool {
	if key == "" {
		key = "unknown"
	}
	now := m.now()
	bucket := buckets[key]
	if bucket.Started.IsZero() || now.Sub(bucket.Started) >= window {
		buckets[key] = rateBucket{Started: now, Count: 1}
		return true
	}
	if bucket.Count >= maximum {
		return false
	}
	bucket.Count++
	buckets[key] = bucket
	return true
}
func (m *Manager) RecordAudit(record AuditRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.Timestamp.IsZero() {
		record.Timestamp = m.now()
	}
	m.state.Audit = append(m.state.Audit, record)
	_ = m.persist()
}
func (m *Manager) Audit() []AuditRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]AuditRecord{}, m.state.Audit...)
}
func (m *Manager) AddAnnotation(sessionID, turnID, author, body string) (Annotation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sessionID == "" || author == "" || body == "" {
		return Annotation{}, errors.New("session, author, and body required")
	}
	now := m.now()
	value := Annotation{ID: "annotation_" + shortHash(fmt.Sprintf("%s:%s:%d", sessionID, author, now.UnixNano())), SessionID: sessionID, TurnID: turnID, Author: author, Body: redact.Text(body, 2000), CreatedAt: now}
	m.state.Annotations = append(m.state.Annotations, value)
	return value, m.persist()
}
func (m *Manager) Annotations(sessionID string) []Annotation {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := make([]Annotation, 0)
	for _, value := range m.state.Annotations {
		if value.SessionID == sessionID {
			values = append(values, value)
		}
	}
	return values
}
func (m *Manager) AddReview(sessionID, reviewer, status, summary string) (Review, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sessionID == "" || reviewer == "" || summary == "" {
		return Review{}, errors.New("session, reviewer, and summary required")
	}
	now := m.now()
	value := Review{ID: "review_" + shortHash(fmt.Sprintf("%s:%s:%d", sessionID, reviewer, now.UnixNano())), SessionID: sessionID, Reviewer: reviewer, Status: status, Summary: redact.Text(summary, 4000), CreatedAt: now}
	m.state.Reviews = append(m.state.Reviews, value)
	return value, m.persist()
}
func (m *Manager) Reviews(sessionID string) []Review {
	m.mu.Lock()
	defer m.mu.Unlock()
	values := make([]Review, 0)
	for _, value := range m.state.Reviews {
		if value.SessionID == sessionID {
			values = append(values, value)
		}
	}
	return values
}
func (m *Manager) pairingIndex(code string) int {
	for index := range m.state.Pairings {
		if m.state.Pairings[index].Code == code {
			return index
		}
	}
	return -1
}
func (m *Manager) touchConnection(grantID string) {
	for index := range m.state.Connections {
		if m.state.Connections[index].GrantID == grantID && m.state.Connections[index].Active {
			m.state.Connections[index].LastSeenAt = m.now()
		}
	}
}
func (m *Manager) persist() error {
	stored := storedManagerState{Pairings: m.state.Pairings, Connections: m.state.Connections, Audit: m.state.Audit, Annotations: m.state.Annotations, Reviews: m.state.Reviews, BadAttempts: m.state.BadAttempts}
	for _, grant := range m.state.Grants {
		stored.Grants = append(stored.Grants, storedGrant(grant))
	}
	body, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	temporary := m.path + ".tmp"
	if err = os.WriteFile(temporary, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, m.path)
}

func normalizePrincipal(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 320 {
		return "", errors.New("valid OAuth principal email required")
	}
	return value, nil
}
func humanCode() (string, error) {
	var body [4]byte
	if _, err := rand.Read(body[:]); err != nil {
		return "", err
	}
	number := int(body[0])<<16 | int(body[1])<<8 | int(body[2])
	return fmt.Sprintf("%03d-%03d", number%1000, (number/1000)%1000), nil
}
func randomToken(size int) (string, error) {
	body := make([]byte, size)
	if _, err := rand.Read(body); err != nil {
		return "", err
	}
	return hex.EncodeToString(body), nil
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func constantHashEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
func shortHash(value string) string { return hash(value)[:24] }
