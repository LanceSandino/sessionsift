// Package hooks normalizes live lifecycle-hook payloads from supported agents.
// @module HookNormalize maps untrusted Codex and Claude Code hook JSON into bounded, redacted events.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

// Provider identifies which agent produced a hook payload.
type Provider string

const (
	Codex      Provider = "codex"
	ClaudeCode Provider = "claude"
)

// metadataKeys are short identifiers kept (redacted and bounded) as-is. Every
// other key is treated as content: redacted, bounded, and flattened to text.
var metadataKeys = map[string]bool{
	"hook_event_name": true, "session_id": true, "turn_id": true, "tool_name": true,
	"tool_use_id": true, "model": true, "source": true, "reason": true, "trigger": true,
	"permission_mode": true, "stop_hook_active": true, "matcher": true,
	"notification_type": true, "agent_type": true, "agent_id": true, "is_interrupt": true,
}

var kindPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// ParseProvider maps a route or spool prefix to a supported provider.
func ParseProvider(value string) (Provider, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "codex":
		return Codex, true
	case "claude", "claude_code", "claude-code":
		return ClaudeCode, true
	default:
		return "", false
	}
}

// SessionID returns the stored session identity so live Claude events merge
// with the transcript import rather than creating a second session.
func (p Provider) SessionID(external string) string {
	if p == ClaudeCode {
		return "claude:" + external
	}
	return external
}

// EventSource is recorded on every event created from a hook payload.
func (p Provider) EventSource(spooled bool) string {
	source := "hook"
	if p == ClaudeCode {
		source = "claude_hook"
	}
	if spooled {
		source += "_spool"
	}
	return source
}

// Normalize validates and redacts one decoded hook payload.
func Normalize(provider Provider, payload map[string]any, at time.Time, identity string, spooled bool) (domain.Event, error) {
	external, _ := payload["session_id"].(string)
	external = strings.TrimSpace(external)
	if external == "" || len(external) > 256 {
		return domain.Event{}, errors.New("session_id is required")
	}
	turnID, _ := payload["turn_id"].(string)
	kind, _ := payload["hook_event_name"].(string)
	kind = kindPattern.ReplaceAllString(strings.TrimSpace(kind), "")
	if kind == "" {
		kind = "hook"
	}
	if len(kind) > 64 {
		kind = kind[:64]
	}
	sessionID := provider.SessionID(external)
	redacted := make(map[string]any, len(payload))
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if droppedKey(key) {
			continue
		}
		value := payload[key]
		if metadataKeys[key] {
			redacted[key] = metadataValue(value)
			continue
		}
		redacted[key] = redact.Text(contentText(value), 2_000)
	}
	redacted["session_id"] = sessionID
	return domain.Event{
		ID:        redact.Hash(fmt.Sprintf("%s:%s:%s", sessionID, kind, identity))[:24],
		SessionID: sessionID, TurnID: redact.Text(turnID, 128), Timestamp: at.UTC(),
		EventType: "hook", EventSubtype: kind, Source: provider.EventSource(spooled), PayloadRedacted: redacted,
	}, nil
}

// droppedKey removes absolute locations that identify the workstation layout.
func droppedKey(key string) bool {
	lower := strings.ToLower(key)
	return lower == "cwd" || strings.HasSuffix(lower, "path") || strings.HasSuffix(lower, "_dir") || strings.HasSuffix(lower, "directory")
}

func metadataValue(value any) any {
	switch typed := value.(type) {
	case string:
		return redact.Text(typed, 200)
	case bool, float64, json.Number, nil:
		return typed
	default:
		return redact.Text(contentText(typed), 200)
	}
}

func contentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}
