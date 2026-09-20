package domain

import (
	"testing"
	"time"
)

func TestCalculateMetrics(t *testing.T) {
	start := time.Now()
	session := Session{StartedAt: start, EndedAt: start.Add(2 * time.Minute), Turns: []Turn{{InputTokens: 100, CachedInputTokens: 60, OutputTokens: 20, ReasoningTokens: 5}}, ToolCalls: []ToolCall{{ToolCategory: "read", Success: true, Timestamp: start.Add(time.Second)}, {ToolCategory: "search", Success: true, Timestamp: start.Add(2 * time.Second)}, {ToolCategory: "edit", Success: false, Timestamp: start.Add(3 * time.Second)}, {ToolCategory: "validation", Success: true, Timestamp: start.Add(4 * time.Second)}}, Events: []Event{{EventType: "compaction"}}, Findings: []Finding{{ID: "f"}}}
	got := CalculateMetrics(session)
	if got.DurationMS != 120000 || got.CacheRatio != .6 || got.FailedToolCalls != 1 || got.Reads != 1 || got.Searches != 1 || got.Edits != 1 || got.Validations != 1 || got.Compactions != 1 || got.ExplorationRatio != .5 || got.FindingCount != 1 || got.TimeToFirstEditMS != 3000 || got.TimeToFirstValidationMS != 4000 {
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
