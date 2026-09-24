// Package codexconfig safely installs SessionSift's local Codex hooks and telemetry exporters.
package codexconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const otelBlock = `[otel]
environment = "local"
log_user_prompt = false
exporter = { otlp-http = { endpoint = "http://127.0.0.1:7777/v1/logs", protocol = "binary" } }
metrics_exporter = { otlp-http = { endpoint = "http://127.0.0.1:7777/v1/metrics", protocol = "binary" } }
`

// Result records which user-owned configuration files changed.
type Result struct {
	HooksChanged bool
	OTelChanged  bool
}

type hookPolicy struct {
	Event     string
	Matcher   string
	Timeout   int
	OmitAsync bool
}

var sessionSiftHookPolicies = []hookPolicy{
	{Event: "SessionStart", Timeout: 10},
	{Event: "UserPromptSubmit", Timeout: 10},
	{Event: "PostToolUse", Matcher: "*", Timeout: 10},
	{Event: "PreCompact", Matcher: "*", Timeout: 10},
	{Event: "PostCompact", Matcher: "*", Timeout: 10},
	{Event: "Stop", Timeout: 10},
	{Event: "SessionEnd", Matcher: "*", Timeout: 3, OmitAsync: true},
	{Event: "Interrupt", Timeout: 3},
}

// Enable merges SessionSift entries without replacing existing Codex settings.
func Enable(codexHome, binaryPath string) (Result, error) {
	if !filepath.IsAbs(binaryPath) {
		return Result{}, errors.New("SessionSift binary path must be absolute")
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return Result{}, err
	}
	configPath := filepath.Join(codexHome, "config.toml")
	hooksPath := filepath.Join(codexHome, "hooks.json")
	if err := rejectSymlink(configPath); err != nil {
		return Result{}, err
	}
	if err := rejectSymlink(hooksPath); err != nil {
		return Result{}, err
	}
	result := Result{}
	config, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	updatedConfig, changed, err := mergeOTel(config)
	if err != nil {
		return result, err
	}
	if changed {
		if err := writeAtomic(configPath, updatedConfig); err != nil {
			return result, err
		}
		result.OTelChanged = true
	}
	hooks, err := os.ReadFile(hooksPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	updatedHooks, changed, err := mergeHooks(hooks, binaryPath)
	if err != nil {
		return result, err
	}
	if changed {
		if err := writeAtomic(hooksPath, updatedHooks); err != nil {
			return result, err
		}
		result.HooksChanged = true
	}
	return result, nil
}

func mergeOTel(body []byte) ([]byte, bool, error) {
	text := string(body)
	hasLogs := strings.Contains(text, "http://127.0.0.1:7777/v1/logs")
	hasMetrics := strings.Contains(text, "http://127.0.0.1:7777/v1/metrics")
	if hasLogs && hasMetrics {
		return body, false, nil
	}
	if strings.Contains(text, "[otel]") {
		return nil, false, errors.New("existing [otel] configuration differs; merge the output of `sessionsift setup otel` manually")
	}
	if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	if len(body) > 0 {
		body = append(body, '\n')
	}
	body = append(body, otelBlock...)
	return body, true, nil
}

func mergeHooks(body []byte, binaryPath string) ([]byte, bool, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &root); err != nil {
			return nil, false, fmt.Errorf("parse hooks.json: %w", err)
		}
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	command := shellQuote(binaryPath) + " hook"
	changed := false
	for _, policy := range sessionSiftHookPolicies {
		entries, _ := hooks[policy.Event].([]any)
		found, repaired := reconcileHookPolicy(entries, command, policy)
		if found {
			changed = changed || repaired
			continue
		}
		handler := map[string]any{"type": "command", "command": command, "timeout": policy.Timeout}
		if !policy.OmitAsync {
			handler["async"] = true
		}
		entry := map[string]any{"hooks": []any{handler}}
		if policy.Matcher != "" {
			entry["matcher"] = policy.Matcher
		}
		hooks[policy.Event] = append(entries, entry)
		changed = true
	}
	if !changed {
		return body, false, nil
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(encoded, '\n'), true, nil
}

func reconcileHookPolicy(entries []any, command string, policy hookPolicy) (bool, bool) {
	found, changed := false, false
	for _, value := range entries {
		entry, _ := value.(map[string]any)
		handlers, _ := entry["hooks"].([]any)
		for _, handlerValue := range handlers {
			handler, _ := handlerValue.(map[string]any)
			if handler["command"] != command {
				continue
			}
			found = true
			if !timeoutEquals(handler["timeout"], policy.Timeout) {
				handler["timeout"] = policy.Timeout
				changed = true
			}
			if policy.OmitAsync {
				if _, exists := handler["async"]; exists {
					delete(handler, "async")
					changed = true
				}
			} else if async, ok := handler["async"].(bool); !ok || !async {
				handler["async"] = true
				changed = true
			}
		}
	}
	return found, changed
}

func timeoutEquals(value any, want int) bool {
	switch number := value.(type) {
	case float64:
		return number == float64(want)
	case int:
		return number == want
	default:
		return false
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
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
	temporary, err := os.CreateTemp(filepath.Dir(path), ".sessionsift-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
