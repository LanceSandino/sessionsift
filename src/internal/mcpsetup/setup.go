// @module MCPSetup manages explicit, reversible ownership of local stdio client entries.
package mcpsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Options struct {
	Client, ConfigPath, StateDir, Command string
	Args                                  []string
}
type PreviewResult struct{ Client, ConfigPath, Snippet string }
type Result struct {
	Changed                bool
	ConfigPath, BackupPath string
}
type receipt struct {
	Version                                                        int
	Client, ConfigPath, BeforeHash, AfterHash, Snippet, BackupPath string
	Entry                                                          json.RawMessage
	Existed, Active                                                bool
	UndoHash                                                       string
}

// Preview returns only the proposed entry, never the existing config or its secrets.
func Preview(o Options) (PreviewResult, error) {
	if e := validateOptions(o); e != nil {
		return PreviewResult{}, e
	}
	before, _, e := readPrivate(o.ConfigPath)
	if e != nil {
		return PreviewResult{}, e
	}
	snippet, _, _, e := proposed(o, before)
	if e != nil {
		return PreviewResult{}, e
	}
	return PreviewResult{Client: o.Client, ConfigPath: o.ConfigPath, Snippet: snippet}, nil
}

// Apply requires the caller's explicit opt-in; its durable receipt precedes config mutation.
func Apply(o Options) (Result, error) { return change(o, false) }

// Undo removes only the unchanged entry this module owns, preserving unrelated user edits.
func Undo(o Options) (Result, error) { return change(o, true) }

func change(o Options, undo bool) (Result, error) {
	result := Result{ConfigPath: o.ConfigPath}
	validation := validateOptions
	if undo {
		validation = validateProfile
	}
	if e := validation(o); e != nil {
		return result, e
	}
	if e := privateDir(o.StateDir); e != nil {
		return result, e
	}
	if info, e := os.Stat(o.StateDir); e != nil || info.Mode().Perm()&0077 != 0 {
		return result, errors.New("ownership directory must have private 0700 permissions")
	}
	state, e := openDirectory(o.StateDir)
	if e != nil {
		return result, e
	}
	defer state.Close()
	unlock, e := lock(state)
	if e != nil {
		return result, e
	}
	defer unlock()
	if e = privateDir(filepath.Dir(o.ConfigPath)); e != nil {
		return result, e
	}
	before, existed, e := readPrivate(o.ConfigPath)
	if e != nil {
		return result, e
	}
	recordBytes, recordExists, e := readPrivate(filepath.Join(o.StateDir, "ownership.json"))
	if e != nil {
		return result, e
	}
	var owned receipt
	if recordExists {
		if e = json.Unmarshal(recordBytes, &owned); e != nil {
			return result, errors.New("invalid ownership receipt")
		}
		if owned.Version != 1 || owned.Client != o.Client || owned.ConfigPath != o.ConfigPath {
			return result, errors.New("ownership receipt belongs to another profile")
		}
		result.BackupPath = owned.BackupPath
	}
	if undo {
		if !recordExists || !owned.Active {
			return result, nil
		}
		var after []byte
		if owned.UndoHash != "" && digest(before) == owned.UndoHash {
			owned.Active = false
			encoded, _ := json.MarshalIndent(owned, "", "  ")
			return result, replaceChecked(filepath.Join(o.StateDir, "ownership.json"), recordBytes, true, encoded)
		}
		// An exact original checksum proves a prepared transaction never took effect.
		if digest(before) == owned.BeforeHash && existed == owned.Existed {
			after = before
		} else {
			after, e = removeOwned(o, before, owned)
			if e != nil {
				return result, e
			}
			if digest(before) == owned.AfterHash && owned.Existed {
				original, exists, err := readPrivate(owned.BackupPath)
				if err != nil || !exists || digest(original) != owned.BeforeHash {
					return result, errors.New("original backup missing or changed")
				}
				after = original
			}
			// Record the exact undo outcome before mutation for interrupted retries.
			owned.UndoHash = digest(after)
			intent, _ := json.MarshalIndent(owned, "", "  ")
			if e = replaceChecked(filepath.Join(o.StateDir, "ownership.json"), recordBytes, true, intent); e != nil {
				return result, e
			}
			recordBytes = intent
			if e = replaceChecked(o.ConfigPath, before, existed, after); e != nil {
				return result, e
			}
			result.Changed = true
		}
		owned.Active = false
		encoded, _ := json.MarshalIndent(owned, "", "  ")
		if e = replaceChecked(filepath.Join(o.StateDir, "ownership.json"), recordBytes, true, encoded); e != nil {
			return result, e
		}
		return result, nil
	}
	if recordExists && owned.Active {
		if digest(before) == owned.BeforeHash && existed == owned.Existed {
			// Resume a prepared transaction only against its exact original bytes.
			snippet, entry, after, err := proposed(o, before)
			if err != nil {
				return result, err
			}
			if snippet != owned.Snippet || !equalJSON(entry, owned.Entry) || digest(after) != owned.AfterHash {
				return result, errors.New("pending setup uses different command or arguments")
			}
			if e = replaceChecked(o.ConfigPath, before, existed, after); e != nil {
				return result, e
			}
			result.Changed = true
			return result, nil
		}
		if _, e = removeOwned(o, before, owned); e != nil {
			return result, e
		}
		snippet, entry := entryFor(o)
		if snippet != owned.Snippet || !equalJSON(entry, owned.Entry) {
			return result, errors.New("existing owned setup differs; undo it before changing command")
		}
		return result, nil
	}
	snippet, entry, after, e := proposed(o, before)
	if e != nil {
		return result, e
	}
	backup, e := writeBackup(state, before)
	if e != nil {
		return result, e
	}
	result.BackupPath = filepath.Join(o.StateDir, backup)
	owned = receipt{Version: 1, Client: o.Client, ConfigPath: o.ConfigPath, BeforeHash: digest(before), AfterHash: digest(after), Snippet: snippet, Entry: entry, BackupPath: result.BackupPath, Existed: existed, Active: true}
	encoded, _ := json.MarshalIndent(owned, "", "  ")
	if e = replaceChecked(filepath.Join(o.StateDir, "ownership.json"), recordBytes, recordExists, encoded); e != nil {
		return result, e
	}
	if e = replaceChecked(o.ConfigPath, before, existed, after); e != nil {
		return result, e
	}
	result.Changed = true
	return result, nil
}

func validateProfile(o Options) error {
	if o.Client != "codex" && o.Client != "claude-code" {
		return errors.New("client must be codex or claude-code")
	}
	for _, path := range []string{o.ConfigPath, o.StateDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || !safeText(path) {
			return errors.New("paths and executable must be clean absolute paths")
		}
		if e := noSymlinks(path); e != nil {
			return e
		}
	}
	if o.ConfigPath == o.StateDir || strings.HasPrefix(o.ConfigPath, o.StateDir+string(os.PathSeparator)) {
		return errors.New("config must be outside ownership directory")
	}
	return nil
}
func validateOptions(o Options) error {
	if e := validateProfile(o); e != nil {
		return e
	}
	if !filepath.IsAbs(o.Command) || filepath.Clean(o.Command) != o.Command || !safeText(o.Command) {
		return errors.New("executable must be a clean absolute path")
	}
	if e := noSymlinks(o.Command); e != nil {
		return e
	}
	name := filepath.Base(o.Command)
	switch name {
	case "sh", "bash", "zsh", "fish", "dash", "env", "cmd", "powershell", "python", "python3", "node":
		return errors.New("shells and general-purpose interpreters are not local MCP launchers")
	}
	info, e := os.Stat(o.Command)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return errors.New("command must name an existing executable")
	}
	if len(o.Args) == 0 || len(o.Args) > 32 {
		return errors.New("provide bounded explicit MCP arguments")
	}
	for _, a := range o.Args {
		if len(a) > 4096 || !safeText(a) || strings.Contains(strings.ToLower(a), "http://") || strings.Contains(strings.ToLower(a), "https://") {
			return errors.New("invalid MCP argument")
		}
	}
	return nil
}

// Reject all controls, invisible formatting controls and invalid UTF-8 before
// JSON quoting can produce an invalid TOML block or alter executable identity.
func safeText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func equalJSON(a, b []byte) bool {
	if string(a) == "null" {
		a = nil
	}
	if string(b) == "null" {
		b = nil
	}
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	xx, _ := json.Marshal(x)
	yy, _ := json.Marshal(y)
	return string(xx) == string(yy)
}
func entryFor(o Options) (string, json.RawMessage) {
	command, _ := json.Marshal(o.Command)
	args, _ := json.Marshal(o.Args)
	if o.Client == "codex" {
		return fmt.Sprintf("# BEGIN SessionSift managed local MCP\n[mcp_servers.sessionsift]\ncommand = %s\nargs = %s\n# END SessionSift managed local MCP\n", command, args), nil
	}
	entry, _ := json.Marshal(map[string]any{"type": "stdio", "command": o.Command, "args": o.Args})
	return string(entry), entry
}
