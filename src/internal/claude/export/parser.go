// Package export defensively adapts Claude.ai data exports into chat sessions.
// @module ClaudeExport streams conversations.json without importing account or profile files.
package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/toolmeta"
)

// conversation declares only what SessionSift keeps. The account object,
// the generated summary and project links are deliberately absent, so they
// are never decoded.
type conversation struct {
	UUID         string        `json:"uuid"`
	Name         string        `json:"name"`
	CreatedAt    string        `json:"created_at"`
	UpdatedAt    string        `json:"updated_at"`
	ChatMessages []chatMessage `json:"chat_messages"`
}

type chatMessage struct {
	UUID        string            `json:"uuid"`
	Text        string            `json:"text"`
	Content     []json.RawMessage `json:"content"`
	Sender      string            `json:"sender"`
	CreatedAt   string            `json:"created_at"`
	Attachments []fileRef         `json:"attachments"`
	Files       []fileRef         `json:"files"`
}

// fileRef keeps only the file name; extracted_content (the full text of an
// uploaded document) has no field and is never decoded.
type fileRef struct {
	FileName string `json:"file_name"`
}

// contentBlock is one message content block. A thinking block decodes to its
// type alone: the thinking text has no field to land in.
type contentBlock struct {
	Type           string          `json:"type"`
	Text           string          `json:"text"`
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Input          json.RawMessage `json:"input"`
	Content        json.RawMessage `json:"content"`
	IsError        bool            `json:"is_error"`
	ToolUseID      string          `json:"tool_use_id"`
	StartTimestamp string          `json:"start_timestamp"`
}

// Parse streams an exported conversations.json array one conversation at a
// time. Elements that are not conversations, or that lack a uuid, are skipped.
func Parse(reader io.Reader) ([]domain.Session, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<30))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("parse Claude export: %w", err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
		return nil, fmt.Errorf("parse Claude export: expected conversation array")
	}
	var sessions []domain.Session
	for decoder.More() {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("parse Claude export: %w", err)
		}
		var value conversation
		if !decodeLenient(raw, &value) || value.UUID == "" {
			continue
		}
		sessions = append(sessions, normalize(value))
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("parse Claude export: %w", err)
	}
	return sessions, nil
}

// decodeLenient tolerates a field whose type drifted: the field is dropped
// and the rest of the object is kept.
func decodeLenient(raw json.RawMessage, target any) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var typeErr *json.UnmarshalTypeError
	err := json.Unmarshal(raw, target)
	return err == nil || errors.As(err, &typeErr)
}

func normalize(value conversation) domain.Session {
	session := domain.Session{
		ID: "claude:" + value.UUID, ExternalSessionID: value.UUID,
		Kind: domain.ChatSession, Source: domain.SourceClaude, StartedAt: parseTime(value.CreatedAt), EndedAt: parseTime(value.UpdatedAt),
		Status: "completed", SourceVersion: "claude-export", ParserVersion: domain.ClaudeExportVersion,
	}
	if title := redact.Title(value.Name, 80); title != "" {
		session.Title, session.TitleSource = title, "export"
	}
	builder := &sessionBuilder{session: &session, turnKeys: map[string]bool{}, calls: map[string]int{}}
	for index, item := range value.ChatMessages {
		builder.add(index, item)
	}
	if session.Title == "" {
		session.Title, session.TitleSource = "Untitled Claude conversation", "fallback"
	}
	if session.EndedAt.IsZero() {
		session.EndedAt = session.StartedAt
	}
	return session
}

// sessionBuilder folds messages into one session. Turn keys and call IDs are
// de-duplicated because storage keys them per session.
type sessionBuilder struct {
	session  *domain.Session
	turnKeys map[string]bool
	calls    map[string]int
	current  string
}

func (b *sessionBuilder) add(index int, item chatMessage) {
	s := b.session
	at := parseTime(item.CreatedAt)
	if at.IsZero() {
		at = s.StartedAt
	}
	if !at.IsZero() && (s.StartedAt.IsZero() || at.Before(s.StartedAt)) {
		s.StartedAt = at
	}
	if at.After(s.EndedAt) {
		s.EndedAt = at
	}
	blocks := decodeBlocks(item.Content)
	switch item.Sender {
	case "human":
		b.human(index, item, at, messageText(item, blocks))
	case "assistant":
		b.assistant(index, item, at, blocks)
	}
}

func (b *sessionBuilder) human(index int, item chatMessage, at time.Time, text string) {
	s := b.session
	key := first(item.UUID, fmt.Sprintf("turn-%d", len(s.Turns)+1))
	if b.turnKeys[key] {
		key = fmt.Sprintf("%s:%d", key, index+1)
	}
	b.turnKeys[key] = true
	id := s.ID + ":" + key
	b.current = id
	s.Turns = append(s.Turns, domain.Turn{ID: id, SessionID: s.ID, ExternalTurnID: item.UUID, Sequence: len(s.Turns) + 1, StartedAt: at, UserPromptRedacted: redact.Text(text, 12_000)})
	payload := map[string]any{"text": redact.Text(text, 2_000)}
	if count := len(item.Attachments) + len(item.Files); count > 0 {
		payload["attachments"] = map[string]any{"count": count, "file_names": attachmentNames(item)}
	}
	s.Events = append(s.Events, domain.Event{ID: s.ID + ":event:" + key, SessionID: s.ID, TurnID: id, Timestamp: at, EventType: "prompt", Source: "claude_export", PayloadRedacted: payload})
	if s.Title == "" {
		if title := redact.Title(text, 80); title != "" {
			s.Title, s.TitleSource = title, "first_prompt"
		}
	}
}

func (b *sessionBuilder) assistant(index int, item chatMessage, at time.Time, blocks []contentBlock) {
	s := b.session
	turn := lastTurn(s)
	var texts []string
	if len(blocks) == 0 && strings.TrimSpace(item.Text) != "" {
		texts = append(texts, item.Text)
	}
	for position, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
			}
		case "tool_use":
			b.toolUse(index, position, item, at, block)
		case "tool_result":
			b.toolResult(index, position, item, at, block)
		}
		// Thinking blocks are skipped: their text is never decoded or stored.
	}
	if turn == nil {
		return
	}
	if text := strings.TrimSpace(strings.Join(texts, "\n")); text != "" {
		turn.AssistantSummaryRedacted = redact.Text(strings.TrimSpace(turn.AssistantSummaryRedacted+"\n"+text), 2_000)
	}
	if at.After(turn.EndedAt) {
		turn.EndedAt = at
	}
}

func (b *sessionBuilder) toolUse(index, position int, item chatMessage, at time.Time, block contentBlock) {
	s := b.session
	name := first(strings.TrimSpace(block.Name), "unknown_tool")
	id := s.ID + ":tool:" + first(block.ID, localID(index, position, item))
	if _, seen := b.calls[id]; seen {
		return
	}
	if started := parseTime(block.StartTimestamp); !started.IsZero() {
		at = started
	}
	transport, server := toolmeta.Describe(name)
	call := domain.ToolCall{ID: id, SessionID: s.ID, TurnID: b.current, Timestamp: at, ToolName: name, ToolCategory: "other", ToolTransport: transport, ToolServer: server, ArgumentsRedacted: redact.Text(compactJSON(block.Input), 2_000), Success: true}
	b.calls[id] = len(s.ToolCalls)
	s.ToolCalls = append(s.ToolCalls, call)
	s.Events = append(s.Events, domain.Event{ID: s.ID + ":event:" + id, SessionID: s.ID, TurnID: b.current, Timestamp: at, EventType: "tool", EventSubtype: call.ToolCategory, Source: "claude_export", PayloadRedacted: map[string]any{"tool_name": name}})
}

// toolResult attaches a result to its call by tool_use_id, else to the latest
// call with the same name; an unmatched result is kept as its own call.
func (b *sessionBuilder) toolResult(index, position int, item chatMessage, at time.Time, block contentBlock) {
	s := b.session
	output := contentText(block.Content)
	if target := b.matchTool(block); target >= 0 {
		call := &s.ToolCalls[target]
		call.ResultPreviewRedacted = redact.Text(output, 2_000)
		call.Success = !block.IsError
		return
	}
	name := first(strings.TrimSpace(block.Name), "unknown_tool")
	transport, server := toolmeta.Describe(name)
	id := s.ID + ":tool:" + first(block.ToolUseID, localID(index, position, item)) + ":result"
	if _, seen := b.calls[id]; seen {
		id += ":" + localID(index, position, item)
	}
	b.calls[id] = len(s.ToolCalls)
	s.ToolCalls = append(s.ToolCalls, domain.ToolCall{ID: id, SessionID: s.ID, TurnID: b.current, Timestamp: at, ToolName: name, ToolCategory: "other", ToolTransport: transport, ToolServer: server, ResultPreviewRedacted: redact.Text(output, 2_000), Success: !block.IsError})
}

func (b *sessionBuilder) matchTool(block contentBlock) int {
	if block.ToolUseID != "" {
		if index, ok := b.calls[b.session.ID+":tool:"+block.ToolUseID]; ok {
			return index
		}
	}
	name := strings.TrimSpace(block.Name)
	if name == "" {
		return -1
	}
	for index := len(b.session.ToolCalls) - 1; index >= 0; index-- {
		if b.session.ToolCalls[index].ToolName == name {
			return index
		}
	}
	return -1
}

// localID names a block that carries no id of its own by its position.
func localID(index, position int, item chatMessage) string {
	return fmt.Sprintf("%s-%d", first(item.UUID, fmt.Sprint(index+1)), position+1)
}

// attachmentNames returns only redacted, bounded file names; the count of
// attachments is reported separately and their contents never are.
func attachmentNames(item chatMessage) []string {
	names := []string{}
	for _, ref := range append(append([]fileRef{}, item.Attachments...), item.Files...) {
		if name := redact.Text(strings.TrimSpace(ref.FileName), 120); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func decodeBlocks(items []json.RawMessage) []contentBlock {
	blocks := make([]contentBlock, 0, len(items))
	for _, item := range items {
		var block contentBlock
		if decodeLenient(item, &block) {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// messageText prefers text content blocks and falls back to the flat text
// field that older exports carry alone.
func messageText(item chatMessage, blocks []contentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(item.Text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// contentText flattens tool_result content given as a string or text blocks.
func contentText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, block := range decodeBlocks(items) {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

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

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func lastTurn(session *domain.Session) *domain.Turn {
	if len(session.Turns) == 0 {
		return nil
	}
	return &session.Turns[len(session.Turns)-1]
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
