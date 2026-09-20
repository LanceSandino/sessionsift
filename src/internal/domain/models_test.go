package domain

import (
	"testing"
	"time"
)

func TestCalculateMetrics(t *testing.T) {
	start := time.Now()
	from, to := 84.0, 94.0
	session := Session{StartedAt: start, EndedAt: start.Add(2 * time.Minute), Turns: []Turn{{StartedAt: start.Add(10 * time.Second), EndedAt: start.Add(70 * time.Second), InputTokens: 100, CachedInputTokens: 60, OutputTokens: 20, ReasoningTokens: 5, RateLimitUsedStart: &from, RateLimitUsedEnd: &to}}, ToolCalls: []ToolCall{{ToolCategory: "read", Success: true, Timestamp: start.Add(time.Second)}, {ToolCategory: "search", Success: true, Timestamp: start.Add(2 * time.Second)}, {ToolCategory: "edit", Success: false, Timestamp: start.Add(3 * time.Second)}, {ToolCategory: "validation", Success: true, Timestamp: start.Add(4 * time.Second)}}, Events: []Event{{EventType: "compaction"}}, Findings: []Finding{{ID: "f"}}}
	got := CalculateMetrics(session)
	if got.DurationMS != 60000 || got.ElapsedDurationMS != 120000 || got.UsageDeltaPercent != 10 || got.UsageMeasuredTurns != 1 || got.CacheRatio != .6 || got.FailedToolCalls != 1 || got.Reads != 1 || got.Searches != 1 || got.Edits != 1 || got.Validations != 1 || got.Compactions != 1 || got.ExplorationRatio != .5 || got.FindingCount != 1 || got.TimeToFirstEditMS != 3000 || got.TimeToFirstValidationMS != 4000 {
		t.Fatalf("metrics=%+v", got)
	}
}

func TestCalculateMetricsCountsToolTransports(t *testing.T) {
	session := Session{ToolCalls: []ToolCall{
		{ToolName: "exec", ToolTransport: "builtin", Success: true},
		{ToolName: "mcp__canva__create", ToolTransport: "mcp", ToolServer: "canva", Success: true},
		{ToolName: "mcp__canva__search", ToolTransport: "mcp", ToolServer: "canva", Success: false},
	}}
	metrics := CalculateMetrics(session)
	if metrics.MCPCalls != 2 || metrics.BuiltinToolCalls != 1 || metrics.UniqueTools != 3 || metrics.UniqueMCPServers != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
}
func TestCalculateMetricsEmpty(t *testing.T) {
	got := CalculateMetrics(Session{})
	if got.CacheRatio != 0 || got.ExplorationRatio != 0 || got.DurationMS != 0 {
		t.Fatalf("metrics=%+v", got)
	}
}

func TestCalculateMetricsMergesOverlappingTurnIntervals(t *testing.T) {
	start := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	session := Session{Turns: []Turn{
		{StartedAt: start, EndedAt: start.Add(10 * time.Minute)},
		{StartedAt: start.Add(5 * time.Minute), EndedAt: start.Add(15 * time.Minute)},
	}}
	if got := CalculateMetrics(session).DurationMS; got != int64(15*time.Minute/time.Millisecond) {
		t.Fatalf("active duration = %d, want %d", got, int64(15*time.Minute/time.Millisecond))
	}
}
