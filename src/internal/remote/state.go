// @module RemoteState persists remote authority atomically and keeps concurrent processes consistent.
package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// fileStamp identifies the on-disk revision this process last read or wrote.
type fileStamp struct {
	size     int64
	modified time.Time
}

func stampOf(info os.FileInfo) fileStamp {
	return fileStamp{size: info.Size(), modified: info.ModTime()}
}

// load reads remote-state.json. Legacy embedded audit records move to the
// append-only audit log so the state file stays small.
func (m *Manager) load() error {
	info, err := os.Stat(m.path)
	if errors.Is(err, os.ErrNotExist) {
		m.state, m.stamp = managerState{}, fileStamp{}
		return nil
	}
	if err != nil {
		return err
	}
	body, err := os.ReadFile(m.path)
	if err != nil {
		return err
	}
	var stored storedManagerState
	if len(body) > 0 && json.Unmarshal(body, &stored) != nil {
		return errors.New("invalid remote state")
	}
	state := managerState{Pairings: stored.Pairings, Connections: stored.Connections, Annotations: stored.Annotations, Reviews: stored.Reviews, BadAttempts: stored.BadAttempts}
	for _, grant := range stored.Grants {
		state.Grants = append(state.Grants, Grant(grant))
	}
	for _, support := range stored.Support {
		state.Support = append(state.Support, support.session())
	}
	m.state, m.stamp = state, stampOf(info)
	if len(stored.Audit) > 0 {
		for _, record := range stored.Audit {
			if err = m.appendAudit(record); err != nil {
				return fmt.Errorf("migrate remote audit: %w", err)
			}
		}
		return m.persist()
	}
	return nil
}

// syncLocked reloads state another process replaced since this process last
// touched it. Revocations issued by the CLI therefore bind the running agent
// immediately, and stale memory can never overwrite them.
func (m *Manager) syncLocked() {
	info, err := os.Stat(m.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		m.stateErr = nil
		return
	case err != nil:
		m.stateErr = err
		return
	case stampOf(info) == m.stamp:
		m.stateErr = nil
		return
	}
	m.stateErr = m.load()
}

// persist writes the complete state atomically. It refuses to write while the
// on-disk state is unreadable, so a corrupt or foreign file is never clobbered.
func (m *Manager) persist() error {
	if m.stateErr != nil {
		return m.stateErr
	}
	stored := storedManagerState{Pairings: m.state.Pairings, Connections: m.state.Connections, Annotations: m.state.Annotations, Reviews: m.state.Reviews, BadAttempts: m.state.BadAttempts}
	for _, grant := range m.state.Grants {
		stored.Grants = append(stored.Grants, storedGrant(grant))
	}
	for _, support := range m.state.Support {
		stored.Support = append(stored.Support, storedSupportFrom(support))
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
	if err = os.Rename(temporary, m.path); err != nil {
		return err
	}
	if info, statErr := os.Stat(m.path); statErr == nil {
		m.stamp = stampOf(info)
	}
	return nil
}
