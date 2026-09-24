// @module ClaudeTranscript/User turns user records into prompts, steering events and tool results.

package transcript

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

// user handles a user record, which carries either human text, tool results
// returned to the model, or both.
func (st *state) user(rec record, at time.Time, eventID string) {
	msg := decodeMessage(rec.Message)
	text, blocks := decodeContent(msg.Content)
	var parts []string
	if text != "" {
		parts = append(parts, text)
	}
	var results []block
	for _, item := range blocks {
		switch item.Type {
		case "text":
			parts = append(parts, item.Text)
		case "tool_result":
			results = append(results, item)
		}
	}
	if len(results) > 0 {
		st.toolResults(rec, at, results)
	}
	if len(parts) > 0 {
		st.prompt(rec, at, eventID, strings.Join(parts, "\n"))
	}
}

// prompt starts a turn for human text, or records steering when the text
// shares the promptId of a turn already seen. Injected meta messages and
// compaction summaries are not prompts.
func (st *state) prompt(rec record, at time.Time, eventID, raw string) {
	if rec.IsMeta || rec.IsCompactSummary {
		return
	}
	text := cleanPrompt(raw)
	if text == "" {
		return
	}
	if isInterrupt(text) {
		st.event(eventID, at, "message", "interrupted", nil)
		return
	}
	origin := injectedOrigin(rec.Origin, text)
	key := first(rec.PromptID, rec.UUID, eventID)
	if index, ok := st.turns[key]; ok {
		st.current = index
		if origin != "" {
			st.event(eventID, at, "message", origin, nil)
			return
		}
		st.event(eventID, at, "message", "steering_or_queued", map[string]any{"text": redact.Text(text, 2_000)})
		return
	}
	st.turns[key] = len(st.s.Turns)
	st.current = len(st.s.Turns)
	st.s.Turns = append(st.s.Turns, newTurn(key, len(st.s.Turns)+1, at))
	if origin != "" {
		// Harness-injected notifications start a model turn but are not
		// something the user typed, so they never become the prompt or title.
		st.event(eventID, at, "message", origin, nil)
		return
	}
	st.turn().UserPromptRedacted = redact.Text(text, 12_000)
	if st.firstPrompt == "" {
		st.firstPrompt = redact.Title(text, 80)
	}
	st.event(eventID, at, "prompt", "", map[string]any{"text": redact.Text(text, 2_000)})
}

// toolResults pairs tool_result blocks with the tool_use that requested them.
func (st *state) toolResults(rec record, at time.Time, results []block) {
	var meta resultMeta
	single := len(results) == 1 && decodeInto(rec.ToolUseResult, &meta)
	for _, item := range results {
		index, ok := st.tools[item.ToolUseID]
		if !ok {
			continue
		}
		call := &st.s.ToolCalls[index]
		output := resultText(item.Content)
		call.ResultPreviewRedacted = redact.Text(output, 2_000)
		call.ContentFingerprint = redact.Hash(output)
		call.Success = !item.IsError && !(single && meta.Interrupted)
		switch {
		case single && meta.DurationMS > 0:
			call.DurationMS = int64(meta.DurationMS)
		case !at.IsZero() && !call.Timestamp.IsZero() && at.After(call.Timestamp):
			call.DurationMS = at.Sub(call.Timestamp).Milliseconds()
		}
	}
	st.extendTurn(at)
}

// injectedOrigin names the non-human source of a user record, or returns ""
// for text the user typed.
func injectedOrigin(raw json.RawMessage, text string) string {
	var origin struct {
		Kind string `json:"kind"`
	}
	decodeInto(raw, &origin)
	kind := strings.TrimSpace(origin.Kind)
	if kind == "" && strings.HasPrefix(text, "<task-notification>") {
		kind = "task-notification"
	}
	if kind == "" || kind == "human" {
		return ""
	}
	return strings.ReplaceAll(strings.ToLower(redact.Text(kind, 40)), "-", "_")
}
