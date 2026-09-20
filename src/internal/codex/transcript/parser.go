// Package transcript defensively adapts unstable Codex JSONL into domain records.
package transcript

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/toolmeta"
)

type envelope struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// Parser converts transcript records while preserving unsupported records as warnings.
type Parser struct{}

// New returns the current versioned Codex transcript adapter.
func New() *Parser { return &Parser{} }

// ParseFile parses one current or archived transcript without loading it wholly into memory.
func (p *Parser) ParseFile(path string, archived bool) (domain.Session, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.Session{}, "", err
	}
	defer file.Close()
	hash := sha256.New()
	session, err := p.Parse(io.TeeReader(file, hash), archived)
	if err != nil {
		return domain.Session{}, "", err
	}
	return session, hex.EncodeToString(hash.Sum(nil)), nil
}

// Parse consumes a JSONL transcript and tolerates malformed individual lines.
func (p *Parser) Parse(reader io.Reader, archived bool) (domain.Session, error) {
	s := domain.Session{ParserVersion: domain.ParserVersion, Source: domain.SourceCodex, Kind: domain.DeveloperSession, Status: "completed", Archived: archived}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	turnIndex := map[string]int{}
	toolIndex := map[string][]int{}
	unsupported := map[string]int{}
	currentTurn := ""
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		var record envelope
		if err := json.Unmarshal(line, &record); err != nil {
			s.Warnings = append(s.Warnings, fmt.Sprintf("line %d: malformed JSON", lineNumber))
			continue
		}
		if s.StartedAt.IsZero() || (!record.Timestamp.IsZero() && record.Timestamp.Before(s.StartedAt)) {
			s.StartedAt = record.Timestamp
		}
		if record.Timestamp.After(s.EndedAt) {
			s.EndedAt = record.Timestamp
		}
		eventID := fmt.Sprintf("evt-%06d", lineNumber)
		switch record.Type {
		case "session_meta":
			parseSessionMeta(&s, record.Payload)
		case "turn_context":
			currentTurn = parseTurn(&s, record, turnIndex)
		case "response_item":
			parseResponse(&s, record, currentTurn, eventID, toolIndex)
		case "event_msg":
			parseEventMessage(&s, record, currentTurn, eventID, turnIndex)
		case "token_usage_record":
			parseUsageRecord(&s, record.Payload, currentTurn, turnIndex)
		case "compacted":
			s.Events = append(s.Events, domain.Event{ID: eventID, SessionID: s.ID, TurnID: currentTurn, Timestamp: record.Timestamp, EventType: "compaction", EventSubtype: "compacted", Source: "transcript"})
		case "world_state":
			parseWorldState(&s, record, currentTurn, eventID)
		default:
			unsupported[record.Type]++
			preserveUnknown(&s, record, currentTurn, eventID)
		}
	}
	if err := scanner.Err(); err != nil {
		return domain.Session{}, err
	}
	for kind, count := range unsupported {
		s.Warnings = append(s.Warnings, fmt.Sprintf("preserved %d unsupported %s records", count, kind))
	}
	if s.ExternalSessionID == "" {
		s.ExternalSessionID = fallbackID(s.StartedAt, lineNumber)
	}
	s.ID = s.ExternalSessionID
	for index := range s.Turns {
		s.Turns[index].SessionID = s.ID
	}
	for index := range s.Events {
		s.Events[index].SessionID = s.ID
	}
	for index := range s.ToolCalls {
		s.ToolCalls[index].SessionID = s.ID
	}
	if s.Title == "" {
		s.Title = "Untitled Codex session"
		s.TitleSource = "fallback"
	}
	if s.Kind == domain.InternalReviewSession {
		s.Title = "Auto-review approval assessment"
	}
	if s.EndedAt.IsZero() {
		s.EndedAt = s.StartedAt
	}
	return s, nil
}

func parseWorldState(s *domain.Session, record envelope, turnID, eventID string) {
	var value struct {
		Full  bool                       `json:"full"`
		State map[string]json.RawMessage `json:"state"`
	}
	if json.Unmarshal(record.Payload, &value) != nil {
		return
	}
	keys := make([]string, 0, len(value.State))
	for key := range value.State {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	s.Events = append(s.Events, domain.Event{
		ID: eventID, SessionID: s.ID, TurnID: turnID, Timestamp: record.Timestamp,
		EventType: "context", EventSubtype: "world_state", Source: "transcript",
		PayloadRedacted: map[string]any{"full": value.Full, "changed_keys": keys},
	})
}

func preserveUnknown(s *domain.Session, record envelope, turnID, eventID string) {
	var payload any
	if json.Unmarshal(record.Payload, &payload) != nil {
		return
	}
	s.Events = append(s.Events, domain.Event{ID: eventID, SessionID: s.ID, TurnID: turnID, Timestamp: record.Timestamp, EventType: "unknown", EventSubtype: record.Type, Source: "transcript", PayloadRedacted: map[string]any{"preview": redact.Text(fmt.Sprint(payload), 2_000)}})
}

func parseSessionMeta(s *domain.Session, raw json.RawMessage) {
	var value struct {
		ID           string          `json:"id"`
		SessionID    string          `json:"session_id"`
		CWD          string          `json:"cwd"`
		Timestamp    time.Time       `json:"timestamp"`
		CLIVersion   string          `json:"cli_version"`
		Originator   string          `json:"originator"`
		ThreadSource string          `json:"thread_source"`
		Source       json.RawMessage `json:"source"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	// Codex subagent transcripts can retain the parent session_id while using a
	// unique transcript id. Prefer id so imports never overwrite the parent.
	s.ExternalSessionID = first(value.ID, value.SessionID)
	s.CWD = value.CWD
	s.SourceVersion = value.CLIVersion
	s.Originator = redact.Text(value.Originator, 120)
	s.ThreadSource = redact.Text(value.ThreadSource, 80)
	var surface string
	if json.Unmarshal(value.Source, &surface) == nil {
		s.Surface = redact.Text(surface, 80)
	}
	var source struct {
		Subagent map[string]any `json:"subagent"`
	}
	if json.Unmarshal(value.Source, &source) == nil {
		if guardian, ok := source.Subagent["other"].(string); ok && guardian == "guardian" {
			s.Kind = domain.InternalReviewSession
		}
	}
	if !value.Timestamp.IsZero() {
		s.StartedAt = value.Timestamp
	}
	base := filepath.Base(filepath.Clean(value.CWD))
	if base != "." && base != string(filepath.Separator) {
		s.RepoDisplayName = base
		s.RepoRootHash = redact.Hash(filepath.Clean(value.CWD))
	}
}

func parseTurn(s *domain.Session, record envelope, indexes map[string]int) string {
	var value struct {
		TurnID     string `json:"turn_id"`
		RootTurnID string `json:"root_turn_id"`
		Model      string `json:"model"`
		Effort     string `json:"effort"`
		CWD        string `json:"cwd"`
	}
	if json.Unmarshal(record.Payload, &value) != nil {
		return ""
	}
	id := first(value.TurnID, value.RootTurnID)
	if id == "" {
		return ""
	}
	if _, ok := indexes[id]; !ok {
		indexes[id] = len(s.Turns)
		s.Turns = append(s.Turns, domain.Turn{ID: id, SessionID: s.ID, ExternalTurnID: id, Sequence: len(s.Turns) + 1, StartedAt: record.Timestamp})
	}
	s.Model, s.ReasoningEffort = value.Model, value.Effort
	if value.Model == "codex-auto-review" {
		s.Kind = domain.InternalReviewSession
	}
	if s.CWD == "" {
		s.CWD = value.CWD
		s.RepoDisplayName = filepath.Base(filepath.Clean(value.CWD))
		s.RepoRootHash = redact.Hash(filepath.Clean(value.CWD))
	}
	return id
}

func parseResponse(s *domain.Session, record envelope, turnID, eventID string, toolIndexes map[string][]int) {
	var value map[string]any
	if json.Unmarshal(record.Payload, &value) != nil {
		return
	}
	kind, _ := value["type"].(string)
	switch kind {
	case "message":
		role, _ := value["role"].(string)
		text := contentText(value["content"])
		if text == "" {
			return
		}
		if role == "user" {
			text = cleanUserContent(text)
			if text == "" {
				return
			}
			eventType, eventSubtype := "prompt", ""
			if turn := findTurn(s, turnID); turn != nil {
				if turn.UserPromptRedacted == "" {
					turn.UserPromptRedacted = redact.Text(text, 12_000)
				} else {
					eventType, eventSubtype = "message", "steering_or_queued"
				}
			}
			if s.Title == "" {
				s.Title = titleFrom(text)
				s.TitleSource = "first_prompt"
			}
			s.Events = append(s.Events, domain.Event{ID: eventID, SessionID: s.ID, TurnID: turnID, Timestamp: record.Timestamp, EventType: eventType, EventSubtype: eventSubtype, Source: "transcript", PayloadRedacted: map[string]any{"text": redact.Text(text, 2_000)}})
		} else if role == "assistant" {
			if turn := findTurn(s, turnID); turn != nil {
				turn.AssistantSummaryRedacted = redact.Text(text, 2_000)
				turn.EndedAt = record.Timestamp
			}
		}
	case "function_call", "custom_tool_call":
		callID := stringValue(value, "call_id", "id")
		name := stringValue(value, "name")
		arguments := anyString(value["arguments"])
		if arguments == "" {
			arguments = anyString(value["input"])
		}
		names := []string{name}
		if kind == "custom_tool_call" && name == "exec" {
			if nested := nestedToolNames(arguments); len(nested) > 0 {
				names = nested
			}
		}
		for index, exactName := range names {
			exactID := callID
			exactEventID := eventID
			if len(names) > 1 || exactName != name {
				exactID = fmt.Sprintf("%s:%d", callID, index+1)
				exactEventID = fmt.Sprintf("%s:tool-%d", eventID, index+1)
			}
			call := newToolCall(exactID, s.ID, turnID, record.Timestamp, exactName, arguments)
			toolIndexes[callID] = append(toolIndexes[callID], len(s.ToolCalls))
			s.ToolCalls = append(s.ToolCalls, call)
			s.Events = append(s.Events, domain.Event{ID: exactEventID, SessionID: s.ID, TurnID: turnID, Timestamp: record.Timestamp, EventType: "tool", EventSubtype: call.ToolCategory, Source: "transcript", PayloadRedacted: map[string]any{"tool_name": exactName, "target": call.TargetPathDisplay}})
		}
	case "function_call_output", "custom_tool_call_output":
		callID := stringValue(value, "call_id", "id")
		if indexes, ok := toolIndexes[callID]; ok {
			output := anyString(value["output"])
			for _, index := range indexes {
				updateToolResult(&s.ToolCalls[index], output)
			}
		}
	}
}

var nestedToolPattern = regexp.MustCompile(`\btools\.([A-Za-z][A-Za-z0-9_]*)\s*\(`)

func nestedToolNames(input string) []string {
	matches := nestedToolPattern.FindAllStringSubmatch(input, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			names = append(names, match[1])
		}
	}
	return names
}

func parseEventMessage(s *domain.Session, record envelope, turnID, eventID string, turnIndexes map[string]int) {
	var value map[string]any
	if json.Unmarshal(record.Payload, &value) != nil {
		return
	}
	kind, _ := value["type"].(string)
	if kind == "token_count" {
		info, _ := value["info"].(map[string]any)
		usage, _ := info["last_token_usage"].(map[string]any)
		applyUsage(findTurn(s, turnID), usage, "transcript")
		return
	}
	if kind == "task_complete" || kind == "turn_aborted" {
		if turn := findTurn(s, turnID); turn != nil {
			turn.EndedAt = record.Timestamp
		}
	}
	if strings.Contains(strings.ToLower(kind), "compact") {
		s.Events = append(s.Events, domain.Event{ID: eventID, SessionID: s.ID, TurnID: turnID, Timestamp: record.Timestamp, EventType: "compaction", EventSubtype: kind, Source: "transcript"})
	}
}

func parseUsageRecord(s *domain.Session, raw json.RawMessage, fallbackTurn string, indexes map[string]int) {
	var value struct {
		TurnID         string         `json:"turn_id"`
		Usage          map[string]any `json:"usage"`
		TurnTokenUsage map[string]any `json:"turn_token_usage"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	id := first(value.TurnID, fallbackTurn)
	usage := value.Usage
	if len(value.TurnTokenUsage) > 0 {
		usage = value.TurnTokenUsage
	}
	applyUsage(findTurn(s, id), usage, "transcript")
}

func applyUsage(turn *domain.Turn, usage map[string]any, source string) {
	if turn == nil || usage == nil {
		return
	}
	turn.InputTokens = intValue(usage, "input_tokens")
	turn.CachedInputTokens = intValue(usage, "cached_input_tokens")
	turn.OutputTokens = intValue(usage, "output_tokens")
	turn.ReasoningTokens = intValue(usage, "reasoning_output_tokens", "reasoning_tokens")
	turn.TokenSource = source
}

func newToolCall(id, sessionID, turnID string, at time.Time, name, arguments string) domain.ToolCall {
	redactedArgs := redact.Text(arguments, 4_000)
	category := classify(name, arguments)
	target := extractTarget(arguments)
	call := domain.ToolCall{ID: id, SessionID: sessionID, TurnID: turnID, Timestamp: at, ToolName: name, ToolCategory: category, ArgumentsRedacted: redactedArgs, Success: true}
	call.ToolTransport, call.ToolServer = toolmeta.Describe(name)
	if target != "" {
		call.TargetPathHash = redact.Hash(target)
		call.TargetPathDisplay = redact.DisplayPath(target)
	}
	if category == "execute" || category == "validation" {
		call.CommandSignature = commandSignature(arguments)
	}
	return call
}

func updateToolResult(call *domain.ToolCall, output string) {
	call.ResultPreviewRedacted = redact.Text(output, 2_000)
	call.ContentFingerprint = redact.Hash(output)
	var value map[string]any
	if json.Unmarshal([]byte(output), &value) == nil {
		if exit, ok := numeric(value["exit_code"]); ok {
			n := int(exit)
			call.ExitCode = &n
			call.Success = n == 0
		}
		if errValue, ok := value["isError"].(bool); ok && errValue {
			call.Success = false
		}
	}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "exit code: 1") || strings.Contains(lower, "iserror\":true") {
		call.Success = false
	}
}

func classify(name, args string) string {
	lower := strings.ToLower(name + " " + args)
	switch {
	case strings.Contains(lower, "apply_patch") || strings.Contains(lower, "write_file") || strings.Contains(lower, "edit"):
		return "edit"
	case strings.Contains(lower, "read_file") || strings.Contains(lower, "view_image") || regexp.MustCompile(`(^|\s)(cat|sed|head|tail)\s`).MatchString(lower):
		return "read"
	case strings.Contains(lower, "search") || regexp.MustCompile(`(^|\s)(rg|grep|find)\s`).MatchString(lower):
		return "search"
	case regexp.MustCompile(`(^|\s)(test|go test|npm test|pnpm test|cargo test|build|lint)\b`).MatchString(lower):
		return "validation"
	case strings.Contains(lower, "web") || strings.Contains(lower, "http"):
		return "network"
	default:
		return "execute"
	}
}

func extractTarget(arguments string) string {
	var value any
	if json.Unmarshal([]byte(arguments), &value) == nil {
		if got := findStringKey(value, map[string]bool{"path": true, "file_path": true, "target": true}); got != "" {
			return got
		}
	}
	re := regexp.MustCompile(`(?:^|\s)((?:\.?\.?\/|\/)[^\s"']+\.[a-zA-Z0-9]+)`)
	match := re.FindStringSubmatch(arguments)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func findStringKey(value any, keys map[string]bool) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if keys[key] {
				if text, ok := item.(string); ok {
					return text
				}
			}
		}
		for _, item := range typed {
			if found := findStringKey(item, keys); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range typed {
			if found := findStringKey(item, keys); found != "" {
				return found
			}
		}
	}
	return ""
}

func commandSignature(arguments string) string {
	value := strings.ToLower(strings.Join(strings.Fields(arguments), " "))
	value = regexp.MustCompile(`\b[0-9a-f]{8,}\b`).ReplaceAllString(value, "<id>")
	value = regexp.MustCompile(`\b\d{4,}\b`).ReplaceAllString(value, "<n>")
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func contentText(value any) string {
	items, _ := value.([]any)
	var parts []string
	for _, item := range items {
		object, _ := item.(map[string]any)
		if text, ok := object["text"].(string); ok {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func cleanUserContent(value string) string {
	for _, tag := range []string{"recommended_plugins", "environment_context"} {
		pattern := regexp.MustCompile(`(?s)<` + tag + `>.*?</` + tag + `>`)
		value = pattern.ReplaceAllString(value, "")
	}
	value = regexp.MustCompile(`(?s)<image\b[^>]*>.*?</image>`).ReplaceAllString(value, "")
	for _, marker := range []string{"## My request:", "# My request:"} {
		if index := strings.LastIndex(value, marker); index >= 0 {
			value = value[index+len(marker):]
			break
		}
	}
	return strings.TrimSpace(value)
}

func titleFrom(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 80 {
		value = value[:77] + "…"
	}
	return value
}

func fallbackID(start time.Time, lines int) string {
	return fmt.Sprintf("session-%d-%d", start.Unix(), lines)
}
func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok {
			return text
		}
	}
	return ""
}
func anyString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if value == nil {
		return ""
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
func intValue(value map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if number, ok := numeric(value[key]); ok {
			return number
		}
	}
	return 0
}
func numeric(value any) (int64, bool) {
	switch n := value.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		v, e := n.Int64()
		return v, e == nil
	case string:
		v, e := strconv.ParseInt(n, 10, 64)
		return v, e == nil
	}
	return 0, false
}
func findTurn(s *domain.Session, id string) *domain.Turn {
	for index := range s.Turns {
		if s.Turns[index].ExternalTurnID == id {
			return &s.Turns[index]
		}
	}
	if len(s.Turns) > 0 {
		return &s.Turns[len(s.Turns)-1]
	}
	return nil
}
