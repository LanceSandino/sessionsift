// Package claudeconfig safely installs SessionSift's Claude Code lifecycle hooks.
// @module ClaudeConfig merges SessionSift-owned hook entries into Claude Code settings byte-for-byte.
package claudeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Events are the Claude Code hook events SessionSift listens to.
var Events = []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "PreCompact", "Stop", "SubagentStop", "SessionEnd"}

var plainPath = regexp.MustCompile(`^[A-Za-z0-9_./:\\-]+$`)

// Result records whether Claude Code settings changed.
type Result struct {
	SettingsPath string
	HooksChanged bool
}

// Command returns the hook command for binaryPath, quoted only when needed so
// it runs under the POSIX shells and Windows shells Claude Code uses.
func Command(binaryPath string) string {
	if plainPath.MatchString(binaryPath) {
		return binaryPath + " hook claude"
	}
	return `"` + strings.ReplaceAll(binaryPath, `"`, `\"`) + `" hook claude`
}

// Enable merges SessionSift hook entries into <claudeHome>/settings.json. Only
// the "hooks" value is rewritten; every other byte of the file is preserved.
func Enable(claudeHome, binaryPath string) (Result, error) {
	if !filepath.IsAbs(binaryPath) {
		return Result{}, errors.New("SessionSift binary path must be absolute")
	}
	path := filepath.Join(claudeHome, "settings.json")
	result := Result{SettingsPath: path}
	if err := rejectSymlink(path); err != nil {
		return result, err
	}
	body, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	updated, changed, err := Merge(body, Command(binaryPath))
	if err != nil || !changed {
		return result, err
	}
	if err = os.MkdirAll(claudeHome, 0o700); err != nil {
		return result, err
	}
	if err = writeAtomic(path, updated); err != nil {
		return result, err
	}
	result.HooksChanged = true
	return result, nil
}

// Merge adds command to every SessionSift event that lacks it.
func Merge(body []byte, command string) ([]byte, bool, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}\n")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false, fmt.Errorf("parse Claude Code settings.json: %w", err)
	}
	hooks := map[string]any{}
	if raw, ok := root["hooks"]; ok && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return nil, false, fmt.Errorf("Claude Code \"hooks\" must be an object: %w", err)
		}
	}
	changed := false
	for _, event := range Events {
		entries, isList := hooks[event].([]any)
		if hooks[event] != nil && !isList {
			return nil, false, fmt.Errorf("Claude Code hooks.%s must be a list", event)
		}
		if commandExists(entries, command) {
			continue
		}
		entry := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 10}}}
		if event == "PostToolUse" {
			entry["matcher"] = "*"
		}
		hooks[event] = append(entries, entry)
		changed = true
	}
	if !changed {
		return body, false, nil
	}
	encoded, err := json.MarshalIndent(hooks, "  ", "  ")
	if err != nil {
		return nil, false, err
	}
	spliced, err := spliceHooks(body, encoded)
	return spliced, err == nil, err
}

// spliceHooks replaces the top-level "hooks" value, or inserts it before the
// closing brace, leaving every other byte untouched.
func spliceHooks(body, value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errors.New("Claude Code settings must be a JSON object")
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyToken.(string)
		start := decoder.InputOffset()
		var skipped json.RawMessage
		if err = decoder.Decode(&skipped); err != nil {
			return nil, err
		}
		end := decoder.InputOffset()
		if key != "hooks" {
			continue
		}
		valueStart := start + int64(bytes.IndexFunc(body[start:end], func(r rune) bool { return r != ':' && r != ' ' && r != '\t' && r != '\r' && r != '\n' }))
		return join(body[:valueStart], value, body[end:]), nil
	}
	closing := bytes.LastIndexByte(body, '}')
	if closing < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	prefix := bytes.TrimRight(body[:closing], " \t\r\n")
	separator := ","
	if bytes.HasSuffix(prefix, []byte("{")) {
		separator = ""
	}
	insertion := []byte(separator + "\n  \"hooks\": " + string(value) + "\n")
	return join(prefix, insertion, body[closing:]), nil
}

func join(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func commandExists(entries []any, command string) bool {
	for _, value := range entries {
		entry, _ := value.(map[string]any)
		handlers, _ := entry["hooks"].([]any)
		for _, handlerValue := range handlers {
			handler, _ := handlerValue.(map[string]any)
			if handler["command"] == command {
				return true
			}
		}
	}
	return false
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink: %s", path)
	}
	return nil
}

func writeAtomic(path string, body []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".sessionsift-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err = temporary.Write(body); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
