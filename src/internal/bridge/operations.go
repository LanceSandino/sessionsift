// @module BridgeOperations provides bounded durable replay receipts, retention and paired recovery snapshots.
package bridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Persisted verifier makes loss/replacement of the KEK detectable even with an empty queue.
func (s *Store) verifyEncryptionKey() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS bridge_key_check (id INTEGER PRIMARY KEY CHECK(id=1), envelope BLOB NOT NULL)`); err != nil {
		return err
	}
	var encrypted []byte
	err := s.db.QueryRow(`SELECT envelope FROM bridge_key_check WHERE id=1`).Scan(&encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		encrypted, err = s.cipher.Seal([]byte("sessionsift.key-check.v1"))
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`INSERT INTO bridge_key_check(id,envelope) VALUES(1,?)`, encrypted)
		return err
	}
	if err != nil {
		return err
	}
	plain, err := s.cipher.Open(encrypted)
	if err != nil || string(plain) != "sessionsift.key-check.v1" {
		return errors.New("bridge key does not match database: restore the matching key")
	}
	return nil
}

// ConsumeSignature atomically rejects an already authenticated request, including after restart.
// Nanosecond timestamps are signed by current clients; no wire-compatibility downgrade is needed.
func (s *Store) ConsumeSignature(deviceID string, signature []byte, expires time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`DELETE FROM request_receipts WHERE expires_at<?`, now); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM request_receipts WHERE device_id=?`, deviceID).Scan(&count); err != nil {
		return err
	}
	if count >= 2048 {
		return errors.New("device request budget exceeded")
	}
	digest := sha256.Sum256(signature)
	if _, err = tx.Exec(`INSERT INTO request_receipts(device_id,digest,expires_at) VALUES(?,?,?)`, deviceID, hex.EncodeToString(digest[:]), expires.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

// Maintain limits orphaned payload lifetime and retains metadata-only audit for 30 days.
func (s *Store) Maintain(ctx context.Context) error {
	now := time.Now().UTC()
	for _, rule := range []struct {
		query  string
		cutoff time.Time
	}{
		{`DELETE FROM tasks WHERE created_at<?`, now.Add(-time.Hour)},
		{`DELETE FROM request_receipts WHERE expires_at<?`, now},
		{`DELETE FROM bridge_audit WHERE created_at<?`, now.Add(-30 * 24 * time.Hour)},
		{`DELETE FROM device_links WHERE expires_at<?`, now.Add(-24 * time.Hour)},
		{`DELETE FROM support_sessions WHERE created_at<?`, now.Add(-30 * 24 * time.Hour)},
	} {
		if _, err := s.db.ExecContext(ctx, rule.query, rule.cutoff.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}

// Backup publishes a consistent SQLite snapshot and its exact loaded KEK as one private directory.
// It never follows existing backup symlinks or writes key bytes into logs.
func (s *Store) Backup(ctx context.Context, directory string) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup directory must be a real directory")
	}
	if err = os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(directory, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	database := filepath.Join(staging, "bridge.db")
	if _, err = s.db.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(database, "'", "''")+`'`); err != nil {
		return "", err
	}
	if err = os.Chmod(database, 0600); err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(staging, "bridge.db.key"), []byte(base64.RawURLEncoding.EncodeToString(s.cipher.backupKey)+"\n"), 0600); err != nil {
		return "", err
	}
	name := filepath.Join(directory, "snapshot-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err = os.Rename(staging, name); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	snapshots := []string{}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "snapshot-") {
			snapshots = append(snapshots, entry.Name())
		}
	}
	sort.Strings(snapshots)
	for len(snapshots) > 7 {
		if err = os.RemoveAll(filepath.Join(directory, snapshots[0])); err != nil {
			return name, fmt.Errorf("snapshot retention: %w", err)
		}
		snapshots = snapshots[1:]
	}
	return name, nil
}

const mcpDocumentation = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>SessionSift MCP setup</title><body><main><h1>Connect SessionSift</h1><p>This is a private beta. Your Local Agent keeps your session database; the hosted bridge relays only authorized, redacted requests.</p><h2>Get help with one session (support code)</h2><ol><li>On the computer with the sessions, run <code>sessionsift agent connect &lt;bridge-url&gt;</code> once (or use Remote access in the app).</li><li>Open the conversation and choose <b>Generate support code</b>, or run <code>sessionsift support start &lt;session-id&gt;</code>.</li><li>The supporter signs in at <a href="/support">/support</a> and enters the code. They see that one session only; the code expires after ten minutes and works once.</li></ol><h2>Use your own sessions from ChatGPT or Claude (MCP)</h2><ol><li>Run <code>sessionsift agent link</code> on your computer and enter the code it shows under <b>Add a device</b> in the control center.</li><li>Approve additional identities locally with <code>sessionsift agent share</code>.</li><li>Select the workspace and device in the control center when more than one is available.</li><li>Add <a href="/mcp">/mcp</a> as a custom connector in ChatGPT or Claude. Authentication does not itself grant session access.</li></ol><p>Email invitations for one session are also available once a device is linked. Never share support codes, link codes, invitation links, keys, or service tokens publicly.</p><a href="/">Open control center</a></main></body></html>`
