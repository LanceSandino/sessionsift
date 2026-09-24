// Package domain defines SessionSift's provider-neutral normalized records.
// @module DomainModels defines provider-neutral sessions, turns, events, tool calls, findings, and metrics.
package domain

import (
	"sort"
	"time"
)

const ParserVersion = "codex-jsonl-v13"

const ChatGPTExportVersion = "chatgpt-export-v3"

// ClaudeCodeParserVersion identifies the Claude Code JSONL adapter; it advances
// whenever parsing or redaction policy changes so stored rows are rebuilt.
const ClaudeCodeParserVersion = "claude-code-jsonl-v1"

// ClaudeExportVersion identifies the Claude.ai data-export adapter.
const ClaudeExportVersion = "claude-export-v1"

// SessionSource identifies the product that produced a normalized session.
type SessionSource string

const (
	SourceCodex      SessionSource = "codex"
	SourceChatGPT    SessionSource = "chatgpt"
	SourceClaudeCode SessionSource = "claude_code"
	SourceClaude     SessionSource = "claude"
)

// SessionKind distinguishes developer work, chats, and product-owned supporting agents.
type SessionKind string

const (
	DeveloperSession      SessionKind = "developer"
	InternalReviewSession SessionKind = "internal_review"
	ChatSession           SessionKind = "chat"
)

// Confidence makes the provenance of a finding explicit in the API and UI.
type Confidence string

const (
	Observed Confidence = "observed"
	Derived  Confidence = "derived"
	Possible Confidence = "possible"
)

// Session is a normalized conversation and its related records.
type Session struct {
	ID                string        `json:"id"`
	ExternalSessionID string        `json:"external_session_id"`
	Title             string        `json:"title"`
	TitleSource       string        `json:"title_source,omitempty"`
	Kind              SessionKind   `json:"session_kind"`
	Source            SessionSource `json:"source"`
	Originator        string        `json:"originator,omitempty"`
	Surface           string        `json:"surface,omitempty"`
	ThreadSource      string        `json:"thread_source,omitempty"`
	RuntimeLocation   string        `json:"runtime_location,omitempty"`
	ApprovalMode      string        `json:"approval_mode,omitempty"`
	SandboxPolicy     string        `json:"sandbox_policy,omitempty"`
	NetworkAccess     string        `json:"network_access,omitempty"`
	CWD               string        `json:"-"`
	RepoRootHash      string        `json:"repo_root_hash"`
	RepoDisplayName   string        `json:"repo_display_name"`
	GitBranch         string        `json:"git_branch,omitempty"`
	Model             string        `json:"model,omitempty"`
	ReasoningEffort   string        `json:"reasoning_effort,omitempty"`
	StartedAt         time.Time     `json:"started_at"`
	EndedAt           time.Time     `json:"ended_at"`
	Status            string        `json:"status"`
	SourceVersion     string        `json:"source_version,omitempty"`
	ParserVersion     string        `json:"parser_version"`
	Archived          bool          `json:"archived"`
	Warnings          []string      `json:"warnings,omitempty"`
	Turns             []Turn        `json:"turns,omitempty"`
	Events            []Event       `json:"events,omitempty"`
	ToolCalls         []ToolCall    `json:"tool_calls,omitempty"`
	Findings          []Finding     `json:"findings,omitempty"`
}

// Turn contains prompt and token data for one user-driven turn.
type Turn struct {
	ID                       string     `json:"id"`
	SessionID                string     `json:"session_id"`
	ExternalTurnID           string     `json:"external_turn_id"`
	Sequence                 int        `json:"sequence"`
	StartedAt                time.Time  `json:"started_at"`
	EndedAt                  time.Time  `json:"ended_at"`
	UserPromptRedacted       string     `json:"user_prompt_redacted,omitempty"`
	AssistantSummaryRedacted string     `json:"assistant_summary_redacted,omitempty"`
	InputTokens              int64      `json:"input_tokens"`
	CachedInputTokens        int64      `json:"cached_input_tokens"`
	OutputTokens             int64      `json:"output_tokens"`
	ReasoningTokens          int64      `json:"reasoning_tokens"`
	ContextWindow            int64      `json:"context_window"`
	TokenSource              string     `json:"token_source,omitempty"`
	Model                    string     `json:"model,omitempty"`
	ReasoningEffort          string     `json:"reasoning_effort,omitempty"`
	RateLimitUsedStart       *float64   `json:"rate_limit_used_start,omitempty"`
	RateLimitUsedEnd         *float64   `json:"rate_limit_used_end,omitempty"`
	RateLimitWindowMinutes   int        `json:"rate_limit_window_minutes,omitempty"`
	RateLimitResetsAt        *time.Time `json:"rate_limit_resets_at,omitempty"`
}

// Event preserves a redacted, provider-neutral timeline record.
type Event struct {
	ID              string         `json:"id"`
	SessionID       string         `json:"session_id"`
	TurnID          string         `json:"turn_id,omitempty"`
	Timestamp       time.Time      `json:"timestamp"`
	EventType       string         `json:"event_type"`
	EventSubtype    string         `json:"event_subtype,omitempty"`
	Source          string         `json:"source"`
	PayloadRedacted map[string]any `json:"payload_redacted,omitempty"`
	DurationMS      int64          `json:"duration_ms,omitempty"`
	Success         *bool          `json:"success,omitempty"`
}

// ToolCall describes an observed tool execution without retaining sensitive output.
type ToolCall struct {
	ID                    string    `json:"id"`
	SessionID             string    `json:"session_id"`
	TurnID                string    `json:"turn_id,omitempty"`
	Timestamp             time.Time `json:"timestamp"`
	ToolName              string    `json:"tool_name"`
	ToolCategory          string    `json:"tool_category"`
	ToolTransport         string    `json:"tool_transport,omitempty"`
	ToolServer            string    `json:"tool_server,omitempty"`
	ArgumentsRedacted     string    `json:"arguments_redacted,omitempty"`
	ResultPreviewRedacted string    `json:"result_preview_redacted,omitempty"`
	DurationMS            int64     `json:"duration_ms,omitempty"`
	Success               bool      `json:"success"`
	ExitCode              *int      `json:"exit_code,omitempty"`
	TargetPathHash        string    `json:"target_path_hash,omitempty"`
	TargetPathDisplay     string    `json:"target_path_display,omitempty"`
	CommandSignature      string    `json:"command_signature,omitempty"`
	ContentFingerprint    string    `json:"content_fingerprint,omitempty"`
}

// Finding is an explainable analyzer output with evidence references.
type Finding struct {
	ID          string         `json:"id"`
	SessionID   string         `json:"session_id"`
	TurnID      string         `json:"turn_id,omitempty"`
	FindingType string         `json:"finding_type"`
	Severity    string         `json:"severity"`
	Confidence  Confidence     `json:"confidence"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Evidence    []string       `json:"evidence"`
	Metric      map[string]any `json:"metric"`
	RuleVersion string         `json:"rule_version"`
	CreatedAt   time.Time      `json:"created_at"`
}

// Metrics contains observable per-session aggregates.
type Metrics struct {
	DurationMS              int64   `json:"duration_ms"`
	ElapsedDurationMS       int64   `json:"elapsed_duration_ms"`
	UsageDeltaPercent       float64 `json:"usage_delta_percent"`
	UsageMeasuredTurns      int     `json:"usage_measured_turns"`
	TurnCount               int     `json:"turn_count"`
	InputTokens             int64   `json:"input_tokens"`
	CachedInputTokens       int64   `json:"cached_input_tokens"`
	OutputTokens            int64   `json:"output_tokens"`
	ReasoningTokens         int64   `json:"reasoning_tokens"`
	CacheRatio              float64 `json:"cache_ratio"`
	ToolCalls               int     `json:"tool_calls"`
	BuiltinToolCalls        int     `json:"builtin_tool_calls"`
	MCPCalls                int     `json:"mcp_calls"`
	AppToolCalls            int     `json:"app_tool_calls"`
	UniqueTools             int     `json:"unique_tools"`
	UniqueMCPServers        int     `json:"unique_mcp_servers"`
	FailedToolCalls         int     `json:"failed_tool_calls"`
	Reads                   int     `json:"reads"`
	Searches                int     `json:"searches"`
	Edits                   int     `json:"edits"`
	Validations             int     `json:"validations"`
	Compactions             int     `json:"compactions"`
	ExplorationRatio        float64 `json:"exploration_ratio"`
	FindingCount            int     `json:"finding_count"`
	TimeToFirstEditMS       int64   `json:"time_to_first_edit_ms"`
	TimeToFirstValidationMS int64   `json:"time_to_first_validation_ms"`
}

// CalculateMetrics computes descriptive values without assigning a productivity score.
func CalculateMetrics(s Session) Metrics {
	m := Metrics{TurnCount: len(s.Turns), ToolCalls: len(s.ToolCalls), FindingCount: len(s.Findings)}
	tools := map[string]struct{}{}
	mcpServers := map[string]struct{}{}
	if !s.EndedAt.IsZero() && !s.StartedAt.IsZero() && s.EndedAt.After(s.StartedAt) {
		m.ElapsedDurationMS = s.EndedAt.Sub(s.StartedAt).Milliseconds()
	}
	activeRanges := make([][2]time.Time, 0, len(s.Turns))
	for _, turn := range s.Turns {
		if !turn.StartedAt.IsZero() && !turn.EndedAt.IsZero() && turn.EndedAt.After(turn.StartedAt) {
			activeRanges = append(activeRanges, [2]time.Time{turn.StartedAt, turn.EndedAt})
		}
		if turn.RateLimitUsedStart != nil && turn.RateLimitUsedEnd != nil && *turn.RateLimitUsedEnd >= *turn.RateLimitUsedStart {
			m.UsageDeltaPercent += *turn.RateLimitUsedEnd - *turn.RateLimitUsedStart
			m.UsageMeasuredTurns++
		}
		m.InputTokens += turn.InputTokens
		m.CachedInputTokens += turn.CachedInputTokens
		m.OutputTokens += turn.OutputTokens
		m.ReasoningTokens += turn.ReasoningTokens
	}
	m.DurationMS = mergedDuration(activeRanges)
	if m.InputTokens > 0 {
		m.CacheRatio = float64(m.CachedInputTokens) / float64(m.InputTokens)
	}
	for _, call := range s.ToolCalls {
		tools[call.ToolName] = struct{}{}
		switch call.ToolTransport {
		case "mcp":
			m.MCPCalls++
			if call.ToolServer != "" {
				mcpServers[call.ToolServer] = struct{}{}
			}
		case "app":
			m.AppToolCalls++
		default:
			m.BuiltinToolCalls++
		}
		if !call.Success {
			m.FailedToolCalls++
		}
		switch call.ToolCategory {
		case "read":
			m.Reads++
		case "search":
			m.Searches++
		case "edit":
			m.Edits++
			if m.TimeToFirstEditMS == 0 && !s.StartedAt.IsZero() && !call.Timestamp.Before(s.StartedAt) {
				m.TimeToFirstEditMS = call.Timestamp.Sub(s.StartedAt).Milliseconds()
			}
		case "validation":
			m.Validations++
			if call.Success && m.TimeToFirstValidationMS == 0 && !s.StartedAt.IsZero() && !call.Timestamp.Before(s.StartedAt) {
				m.TimeToFirstValidationMS = call.Timestamp.Sub(s.StartedAt).Milliseconds()
			}
		}
	}
	m.UniqueTools = len(tools)
	m.UniqueMCPServers = len(mcpServers)
	for _, event := range s.Events {
		if event.EventType == "compaction" {
			m.Compactions++
		}
	}
	if m.ToolCalls > 0 {
		m.ExplorationRatio = float64(m.Reads+m.Searches) / float64(m.ToolCalls)
	}
	return m
}

// mergedDuration returns the union of observed turn intervals so overlapping
// records never inflate active work time.
func mergedDuration(ranges [][2]time.Time) int64 {
	if len(ranges) == 0 {
		return 0
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0].Before(ranges[j][0]) })
	start, end := ranges[0][0], ranges[0][1]
	var total time.Duration
	for _, current := range ranges[1:] {
		if !current[0].After(end) {
			if current[1].After(end) {
				end = current[1]
			}
			continue
		}
		total += end.Sub(start)
		start, end = current[0], current[1]
	}
	return (total + end.Sub(start)).Milliseconds()
}
