// Package config resolves SessionSift and Codex local-only state paths.
// @module ConfigPaths resolves private product-state locations and the installation secret.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Paths contains product-owned local state locations.
type Paths struct {
	Home           string
	Database       string
	Logs           string
	RawEvents      string
	Exports        string
	Backups        string
	PID            string
	ChatGPTExport  string
	DeviceIdentity string
	RemoteState    string
	HostedBridge   string
	ClaudeExport   string
	PathKey        string
	SyncMasterKey  string
	VersionMarker  string
}

// Resolve returns SESSIONSIFT_HOME or the standard ~/.sessionsift directory.
func Resolve() (Paths, error) {
	home := os.Getenv("SESSIONSIFT_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		home = filepath.Join(userHome, ".sessionsift")
	}
	home = filepath.Clean(home)
	return Paths{Home: home, Database: filepath.Join(home, "sessionsift.db"), Logs: filepath.Join(home, "logs"), RawEvents: filepath.Join(home, "raw", "events"), Exports: filepath.Join(home, "exports"), Backups: filepath.Join(home, "backups"), PID: filepath.Join(home, "sessionsift.pid"), ChatGPTExport: filepath.Join(home, "chatgpt-export.path"), DeviceIdentity: filepath.Join(home, "device-identity.json"), RemoteState: filepath.Join(home, "remote-state.json"), HostedBridge: filepath.Join(home, "hosted-bridge.json"), ClaudeExport: filepath.Join(home, "claude-export.path"), PathKey: filepath.Join(home, "path-hash.key"), SyncMasterKey: filepath.Join(home, "sync-master.key"), VersionMarker: filepath.Join(home, "schema-version")}, nil
}

// Ensure creates only product-owned state directories with private permissions.
func (p Paths) Ensure() error {
	for _, path := range []string{p.Home, p.Logs, p.RawEvents, p.Exports, p.Backups} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// LoadOrCreateSecret returns a 32-byte private installation secret, creating
// it with owner-only permissions on first use. A malformed file is an error so
// a replaced secret never silently changes shared identities.
func LoadOrCreateSecret(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err == nil {
		key, decodeErr := hex.DecodeString(strings.TrimSpace(string(body)))
		if decodeErr != nil || len(key) != 32 {
			return nil, errors.New("installation secret is malformed")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return LoadOrCreateSecret(path)
	}
	if err != nil {
		return nil, err
	}
	_, err = file.WriteString(hex.EncodeToString(key) + "\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return key, err
}
