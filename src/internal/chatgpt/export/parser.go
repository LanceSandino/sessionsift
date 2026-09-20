// Package export defensively adapts ChatGPT conversation exports into sessions.
package export

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/toolmeta"
)

type conversation struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	CreateTime  json.Number     `json:"create_time"`
	UpdateTime  json.Number     `json:"update_time"`
	CurrentNode string          `json:"current_node"`
	Mapping     map[string]node `json:"mapping"`
}

type node struct {
	ID       string   `json:"id"`
	Parent   string   `json:"parent"`
	Children []string `json:"children"`
	Message  *message `json:"message"`
}

type message struct {
	ID         string         `json:"id"`
	Author     author         `json:"author"`
	CreateTime json.Number    `json:"create_time"`
	Content    content        `json:"content"`
	Metadata   map[string]any `json:"metadata"`
	Recipient  string         `json:"recipient"`
}

type author struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

type content struct {
	Parts []any  `json:"parts"`
	Text  string `json:"text"`
}

// Parse reads an exported conversations JSON document.
func Parse(reader io.Reader) ([]domain.Session, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<30))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("parse ChatGPT export: %w", err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
		return nil, fmt.Errorf("parse ChatGPT export: expected conversation array")
	}
	var sessions []domain.Session
	for decoder.More() {
		var value conversation
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("parse ChatGPT export: %w", err)
		}
		if value.ID == "" {
			continue
		}
		sessions = append(sessions, normalize(value))
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("parse ChatGPT export: %w", err)
	}
	return sessions, nil
}

func normalize(value conversation) domain.Session {
	started := numberTime(value.CreateTime)
	ended := numberTime(value.UpdateTime)
	session := domain.Session{
		ID: "chatgpt:" + value.ID, ExternalSessionID: value.ID, Title: bounded(value.Title, 80),
		TitleSource: "export",
		Kind:        domain.ChatSession, Source: domain.SourceChatGPT, StartedAt: started, EndedAt: ended,
		Status: "completed", SourceVersion: "chatgpt-export", ParserVersion: domain.ChatGPTExportVersion,
	}
	ordered := orderedMessages(value)
	currentTurn := ""
	for _, item := range ordered {
		text := messageText(item.Content)
		at := numberTime(item.CreateTime)
		if at.IsZero() {
			at = started
		}
		if session.StartedAt.IsZero() || (!at.IsZero() && at.Before(session.StartedAt)) {
			session.StartedAt = at
		}
		if at.After(session.EndedAt) {
			session.EndedAt = at
		}
		switch item.Author.Role {
		case "user":
			currentTurn = session.ID + ":" + first(item.ID, fmt.Sprintf("turn-%d", len(session.Turns)+1))
			session.Turns = append(session.Turns, domain.Turn{ID: currentTurn, SessionID: session.ID, ExternalTurnID: item.ID, Sequence: len(session.Turns) + 1, StartedAt: at, UserPromptRedacted: redact.Text(text, 12_000)})
			session.Events = append(session.Events, domain.Event{ID: session.ID + ":event:" + first(item.ID, fmt.Sprint(len(session.Events)+1)), SessionID: session.ID, TurnID: currentTurn, Timestamp: at, EventType: "prompt", Source: "chatgpt_export", PayloadRedacted: map[string]any{"text": redact.Text(text, 2_000)}})
			if session.Title == "" {
				session.Title = bounded(text, 80)
			}
		case "assistant":
			if model, ok := item.Metadata["model_slug"].(string); ok && model != "" {
				session.Model = model
			}
			if isToolRecipient(item.Recipient) {
				call := chatToolCall(session.ID, currentTurn, item, at, text)
				session.ToolCalls = append(session.ToolCalls, call)
				session.Events = append(session.Events, domain.Event{ID: session.ID + ":event:" + call.ID, SessionID: session.ID, TurnID: currentTurn, Timestamp: at, EventType: "tool", EventSubtype: call.ToolCategory, Source: "chatgpt_export", PayloadRedacted: map[string]any{"tool_name": call.ToolName}})
			} else if turn := lastTurn(&session); turn != nil && text != "" {
				turn.AssistantSummaryRedacted = redact.Text(strings.TrimSpace(turn.AssistantSummaryRedacted+"\n"+text), 2_000)
				turn.EndedAt = at
			}
		case "tool":
			attachToolResult(&session, first(item.Author.Name, item.Recipient), text, currentTurn, item, at)
		}
	}
	if session.Title == "" {
		session.Title = "Untitled ChatGPT conversation"
	}
	if session.EndedAt.IsZero() {
		session.EndedAt = session.StartedAt
	}
	return session
}

func orderedMessages(value conversation) []*message {
	if value.CurrentNode != "" {
		var reversed []*message
		seen := map[string]bool{}
		for id := value.CurrentNode; id != "" && !seen[id]; {
			seen[id] = true
			item, ok := value.Mapping[id]
			if !ok {
				break
			}
			if item.Message != nil {
				reversed = append(reversed, item.Message)
			}
			id = item.Parent
		}
		for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
			reversed[left], reversed[right] = reversed[right], reversed[left]
		}
		if len(reversed) > 0 {
			return reversed
		}
	}
	items := make([]*message, 0, len(value.Mapping))
	for _, item := range value.Mapping {
		if item.Message != nil {
			items = append(items, item.Message)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := numberTime(items[i].CreateTime), numberTime(items[j].CreateTime)
		if left.Equal(right) {
			return items[i].ID < items[j].ID
		}
		return left.Before(right)
	})
	return items
}

func chatToolCall(sessionID, turnID string, item *message, at time.Time, arguments string) domain.ToolCall {
	id := sessionID + ":tool:" + first(item.ID, fmt.Sprint(at.UnixNano()))
	transport, server := toolmeta.Describe(item.Recipient)
	return domain.ToolCall{ID: id, SessionID: sessionID, TurnID: turnID, Timestamp: at, ToolName: item.Recipient, ToolCategory: "other", ToolTransport: transport, ToolServer: server, ArgumentsRedacted: redact.Text(arguments, 2_000), Success: true}
}

func attachToolResult(session *domain.Session, name, output, turnID string, item *message, at time.Time) {
	_, resultServer := toolmeta.Describe(name)
	for index := len(session.ToolCalls) - 1; index >= 0; index-- {
		call := &session.ToolCalls[index]
		if name == "" || call.ToolName == name || (resultServer != "" && call.ToolServer == resultServer) || strings.HasPrefix(call.ToolName, name+".") {
			call.ResultPreviewRedacted = redact.Text(output, 2_000)
			return
		}
	}
	transport, server := toolmeta.Describe(name)
	session.ToolCalls = append(session.ToolCalls, domain.ToolCall{ID: session.ID + ":tool:" + first(item.ID, fmt.Sprint(at.UnixNano())), SessionID: session.ID, TurnID: turnID, Timestamp: at, ToolName: first(name, "unknown_tool"), ToolCategory: "other", ToolTransport: transport, ToolServer: server, ResultPreviewRedacted: redact.Text(output, 2_000), Success: true})
}

func messageText(value content) string {
	var parts []string
	if value.Text != "" {
		parts = append(parts, value.Text)
	}
	for _, part := range value.Parts {
		switch item := part.(type) {
		case string:
			parts = append(parts, item)
		case map[string]any:
			if text, ok := item["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func numberTime(value json.Number) time.Time {
	seconds, err := value.Float64()
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	whole := int64(seconds)
	return time.Unix(whole, int64((seconds-float64(whole))*1e9)).UTC()
}

func isToolRecipient(value string) bool { return value != "" && value != "all" && value != "none" }
func lastTurn(session *domain.Session) *domain.Turn {
	if len(session.Turns) == 0 {
		return nil
	}
	return &session.Turns[len(session.Turns)-1]
}
func bounded(value string, limit int) string {
	value = strings.Join(strings.Fields(redact.Text(value, limit*2)), " ")
	if len(value) > limit {
		return value[:limit-3] + "…"
	}
	return value
}
func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
