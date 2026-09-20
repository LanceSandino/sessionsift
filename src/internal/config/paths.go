// Package config resolves SessionSift and Codex local-only state paths.
package config

import (
	"os"
	"path/filepath"
)

// Paths contains product-owned local state locations.
type Paths struct {
	Home           string
	Database       string
	Logs           string
	RawEvents      string
	Exports        string
	PID            string
	ChatGPTExport  string
	DeviceIdentity string
	RemoteState    string
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
	return Paths{Home: home, Database: filepath.Join(home, "sessionsift.db"), Logs: filepath.Join(home, "logs"), RawEvents: filepath.Join(home, "raw", "events"), Exports: filepath.Join(home, "exports"), PID: filepath.Join(home, "sessionsift.pid"), ChatGPTExport: filepath.Join(home, "chatgpt-export.path"), DeviceIdentity: filepath.Join(home, "device-identity.json"), RemoteState: filepath.Join(home, "remote-state.json")}, nil
}

// Ensure creates only product-owned state directories with private permissions.
func (p Paths) Ensure() error {
	for _, path := range []string{p.Home, p.Logs, p.RawEvents, p.Exports} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}
