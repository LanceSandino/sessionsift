// @module ClaudeTranscript/Assistant maps assistant responses, per-response usage, tool requests and system markers.

package transcript

import (
	"fmt"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

// syntheticModel marks assistant lines Claude Code writes itself (API error
// notices, "no response requested"); they are not model output.
const syntheticModel = "<synthetic>"

func newTurn(key string, sequence int, at time.Time) domain.Turn {
	return domain.Turn{ID: key, ExternalTurnID: key, Sequence: sequence, StartedAt: at}
}

// assistant handles one assistant line. Claude Code writes a single API
// response as several lines (one per content block) that share message.id.
func (st *state) assistant(rec record, at time.Time, eventID string) {
	msg := decodeMessage(rec.Message)
	turn := st.turn()
	st.extendTurn(at)
	synthetic := strings.TrimSpace(msg.Model) == syntheticModel || rec.IsAPIErrorMessage
	if !synthetic {
		if model := redact.Text(strings.TrimSpace(msg.Model), 120); model != "" {
			st.s.Model = model
			if turn != nil {
				turn.Model = model
			}
		}
		st.recordUsage(msg, rec, eventID)
	}
	if effort := redact.Text(strings.TrimSpace(rec.Effort), 40); effort != "" {
		st.s.ReasoningEffort = effort
		if turn != nil {
			turn.ReasoningEffort = effort
		}
	}
	_, blocks := decodeContent(msg.Content)
	tools := 0
	for _, item := range blocks {
		switch item.Type {
		case "text":
			// Thinking blocks fall through untouched: their text is never kept.
			if text := strings.TrimSpace(item.Text); text != "" && !synthetic && turn != nil {
				turn.AssistantSummaryRedacted = redact.Text(text, 2_000)
			}
		case "tool_use":
			tools++
			st.toolUse(item, at, eventID, tools)
		}
	}
}

// recordUsage keeps the latest usage object per API response so the
// repeated per-block copies are counted once.
func (st *state) recordUsage(msg message, rec record, eventID string) {
	if msg.Usage == nil {
		return
	}
	key := first(msg.ID, rec.UUID, eventID)
	entry, ok := st.usage[key]
	if !ok {
		entry = &usageEntry{turn: st.current}
		st.usage[key] = entry
	}
	entry.usage = *msg.Usage
}

// applyUsage sums each distinct response into the turn where it first
// appeared. Claude reports input_tokens (uncached), cache_creation and
// cache_read as disjoint counters; SessionSift's InputTokens is total input
// with CachedInputTokens as its cached subset, so all three are added to
// InputTokens and cache reads alone become CachedInputTokens. Cache writes
// are billed as fresh input and therefore are not counted as cached.
func (st *state) applyUsage() {
	for _, entry := range st.usage {
		if entry.turn < 0 || entry.turn >= len(st.s.Turns) {
			continue
		}
		turn := &st.s.Turns[entry.turn]
		value := entry.usage
		turn.InputTokens += int64(value.InputTokens + value.CacheCreationInputTokens + value.CacheReadInputTokens)
		turn.CachedInputTokens += int64(value.CacheReadInputTokens)
		turn.OutputTokens += int64(value.OutputTokens)
		turn.ReasoningTokens += int64(value.OutputTokensDetails.ThinkingTokens)
		turn.TokenSource = "transcript"
	}
}

func (st *state) toolUse(item block, at time.Time, eventID string, ordinal int) {
	id := strings.TrimSpace(item.ID)
	if id == "" {
		id = fmt.Sprintf("%s:tool-%d", eventID, ordinal)
	}
	if _, seen := st.tools[id]; seen {
		return
	}
	call := newToolCall(id, st.turnKey(), at, item.Name, item.Input)
	st.tools[id] = len(st.s.ToolCalls)
	st.s.ToolCalls = append(st.s.ToolCalls, call)
	toolEventID := eventID
	if ordinal > 1 {
		toolEventID = fmt.Sprintf("%s:tool-%d", eventID, ordinal)
	}
	st.event(toolEventID, at, "tool", call.ToolCategory, map[string]any{"tool_name": call.ToolName, "target": call.TargetPathDisplay})
}

// system maps compaction and API-error markers. Error records keep only their
// level: the error object can quote request content and account limits.
func (st *state) system(rec record, at time.Time, eventID string) {
	switch rec.Subtype {
	case "compact_boundary":
		var meta compactMeta
		decodeInto(rec.CompactMetadata, &meta)
		payload := map[string]any{}
		if trigger := redact.Text(strings.TrimSpace(meta.Trigger), 40); trigger != "" {
			payload["trigger"] = trigger
		}
		if meta.PreTokens > 0 {
			payload["pre_tokens"] = int64(meta.PreTokens)
		}
		if meta.PostTokens > 0 {
			payload["post_tokens"] = int64(meta.PostTokens)
		}
		if len(payload) == 0 {
			payload = nil
		}
		st.event(eventID, at, "compaction", "compact_boundary", payload).DurationMS = int64(meta.DurationMS)
	case "api_error":
		st.event(eventID, at, "error", "api_error", map[string]any{"level": redact.Text(strings.TrimSpace(rec.Level), 40)})
	default:
		st.unsupported["system/"+label(rec.Subtype)]++
	}
}
