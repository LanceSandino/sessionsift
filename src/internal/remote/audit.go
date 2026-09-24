// @module RemoteAudit keeps a bounded, append-only, metadata-only record of remote operations.
package remote

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// maxAuditRecords bounds retained audit history; compaction runs after 25%
// overflow so each relayed request costs one append, not a file rewrite.
const maxAuditRecords = 5000

func (m *Manager) auditPath() string {
	return strings.TrimSuffix(m.path, filepath.Ext(m.path)) + "-audit.jsonl"
}

// RecordAudit appends one record. Failures are surfaced through AuditHealthy
// (readiness) without exposing paths or record data.
func (m *Manager) RecordAudit(record AuditRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.Timestamp.IsZero() {
		record.Timestamp = m.now()
	}
	m.auditErr = m.appendAudit(record)
	if m.auditErr == nil && m.auditLines > maxAuditRecords+maxAuditRecords/4 {
		m.auditErr = m.compactAudit()
	}
}

func (m *Manager) appendAudit(record AuditRecord) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.auditPath()), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(m.auditPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		m.auditLines++
	}
	return err
}

func (m *Manager) readAudit() []AuditRecord {
	body, err := os.ReadFile(m.auditPath())
	if err != nil {
		return []AuditRecord{}
	}
	values := make([]AuditRecord, 0)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var record AuditRecord
		if json.Unmarshal(scanner.Bytes(), &record) == nil {
			values = append(values, record)
		}
	}
	if len(values) > maxAuditRecords {
		values = values[len(values)-maxAuditRecords:]
	}
	return values
}

func (m *Manager) countAuditLines() int {
	body, err := os.ReadFile(m.auditPath())
	if err != nil {
		return 0
	}
	return bytes.Count(body, []byte{'\n'})
}

func (m *Manager) compactAudit() error {
	values := m.readAudit()
	var buffer bytes.Buffer
	for _, record := range values {
		line, _ := json.Marshal(record)
		buffer.Write(line)
		buffer.WriteByte('\n')
	}
	temporary := m.auditPath() + ".tmp"
	if err := os.WriteFile(temporary, buffer.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, m.auditPath()); err != nil {
		return err
	}
	m.auditLines = len(values)
	return nil
}

// Audit returns the retained history, oldest first.
func (m *Manager) Audit() []AuditRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readAudit()
}

// AuditTail returns at most limit of the newest records, oldest first.
func (m *Manager) AuditTail(limit int) []AuditRecord {
	values := m.Audit()
	if limit > 0 && len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}

// AuditHealthy reports persistence failure without leaking paths or record data.
func (m *Manager) AuditHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.auditErr == nil
}
