// @module ClaudeTranscript/Records declares the decoded JSONL shapes and tolerant decoding helpers.

package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// record is the union of the fields SessionSift reads from any Claude Code
// line. Fields holding content SessionSift never keeps (thinking text and
// signatures, attachments, hook output, API error text, queued prompts) are
// deliberately not declared, so they are never decoded into memory.
type record struct {
	Type              string          `json:"type"`
	Subtype           string          `json:"subtype"`
	UUID              string          `json:"uuid"`
	SessionID         string          `json:"sessionId"`
	Timestamp         string          `json:"timestamp"`
	CWD               string          `json:"cwd"`
	GitBranch         string          `json:"gitBranch"`
	Version           string          `json:"version"`
	Entrypoint        string          `json:"entrypoint"`
	IsSidechain       bool            `json:"isSidechain"`
	IsMeta            bool            `json:"isMeta"`
	IsCompactSummary  bool            `json:"isCompactSummary"`
	IsAPIErrorMessage bool            `json:"isApiErrorMessage"`
	AgentID           string          `json:"agentId"`
	PermissionMode    string          `json:"permissionMode"`
	PromptID          string          `json:"promptId"`
	Effort            string          `json:"effort"`
	Level             string          `json:"level"`
	Origin            json.RawMessage `json:"origin"`
	Message           json.RawMessage `json:"message"`
	ToolUseResult     json.RawMessage `json:"toolUseResult"`
	CompactMetadata   json.RawMessage `json:"compactMetadata"`
	Summary           string          `json:"summary"`
	CustomTitle       string          `json:"customTitle"`
	AITitle           string          `json:"aiTitle"`
}

// message is the Anthropic Messages API object embedded in user and
// assistant records.
type message struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

// usage mirrors the API usage object. Claude reports cache reads and cache
// writes as separate counters next to uncached input_tokens.
type usage struct {
	InputTokens              count `json:"input_tokens"`
	CacheCreationInputTokens count `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     count `json:"cache_read_input_tokens"`
	OutputTokens             count `json:"output_tokens"`
	OutputTokensDetails      struct {
		ThinkingTokens count `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

// block is one message content block. A thinking block decodes to its type
// alone: the thinking text and signature have no field to land in.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// resultMeta is the only part of toolUseResult SessionSift reads; stdout,
// file contents and patches in the same object are never decoded.
type resultMeta struct {
	DurationMS  count `json:"durationMs"`
	Interrupted bool  `json:"interrupted"`
}

type compactMeta struct {
	Trigger    string `json:"trigger"`
	PreTokens  count  `json:"preTokens"`
	PostTokens count  `json:"postTokens"`
	DurationMS count  `json:"durationMs"`
}

// count decodes a non-negative integer from a JSON number or numeric string
// and treats anything else as zero, so one odd counter never drops a record.
type count int64

func (c *count) UnmarshalJSON(data []byte) error {
	var number json.Number
	if json.Unmarshal(data, &number) != nil {
		*c = 0
		return nil
	}
	if value, err := number.Int64(); err == nil {
		*c = count(max(value, 0))
		return nil
	}
	if value, err := number.Float64(); err == nil && value > 0 {
		*c = count(int64(value))
		return nil
	}
	*c = 0
	return nil
}

// decodeRecord accepts any JSON object. A field whose type changed in a newer
// Claude Code release is dropped rather than discarding the whole line.
func decodeRecord(line []byte) (record, bool) {
	if len(line) == 0 || line[0] != '{' {
		return record{}, false
	}
	var rec record
	return rec, lenient(json.Unmarshal(line, &rec))
}

func decodeInto(raw json.RawMessage, target any) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	return lenient(json.Unmarshal(raw, target))
}

func lenient(err error) bool {
	var typeErr *json.UnmarshalTypeError
	return err == nil || errors.As(err, &typeErr)
}

func decodeMessage(raw json.RawMessage) message {
	var value message
	decodeInto(raw, &value)
	return value
}

// decodeContent returns string content, or the blocks of array content.
func decodeContent(raw json.RawMessage) (string, []block) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return "", nil
	}
	blocks := make([]block, 0, len(items))
	for _, item := range items {
		var value block
		if decodeInto(item, &value) {
			blocks = append(blocks, value)
		}
	}
	return "", blocks
}

// resultText flattens tool_result content; images and documents contribute
// nothing because only text is previewed.
func resultText(raw json.RawMessage) string {
	text, blocks := decodeContent(raw)
	if text != "" {
		return text
	}
	parts := make([]string, 0, len(blocks))
	for _, item := range blocks {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// compactJSON normalizes tool input whitespace before it is redacted.
func compactJSON(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var buffer bytes.Buffer
	if json.Compact(&buffer, raw) != nil {
		return string(raw)
	}
	return buffer.String()
}
