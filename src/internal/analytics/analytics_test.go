// Package analytics tests privacy-safe cross-session aggregates.
package analytics

import (
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestBuildAggregatesObservedSessionData(t *testing.T) {
	start := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	sessions := []domain.Session{
		{
			ID: "codex", Source: domain.SourceCodex, Kind: domain.DeveloperSession,
			RepoDisplayName: "sessionsift", Model: "gpt-6", StartedAt: start, EndedAt: start.Add(10 * time.Minute),
			Turns: []domain.Turn{{StartedAt: start, EndedAt: start.Add(10 * time.Minute), InputTokens: 100, CachedInputTokens: 40, OutputTokens: 20}},
			ToolCalls: []domain.ToolCall{
				{ToolName: "exec", ToolCategory: "validation", ToolTransport: "builtin", Success: false},
				{ToolName: "mcp__markest__fetch", ToolTransport: "mcp", ToolServer: "markest", Success: true},
			},
			Findings: []domain.Finding{{FindingType: "possible_scope_correction", Severity: "warning", Confidence: domain.Possible}},
		},
		{
			ID: "chat", Source: domain.SourceChatGPT, Kind: domain.ChatSession,
			Model: "gpt-6", StartedAt: start.Add(24 * time.Hour), EndedAt: start.Add(25 * time.Hour),
			Turns:     []domain.Turn{{StartedAt: start.Add(24 * time.Hour), EndedAt: start.Add(24*time.Hour + 10*time.Minute), InputTokens: 300, CachedInputTokens: 60, OutputTokens: 50}},
			ToolCalls: []domain.ToolCall{{ToolName: "web", ToolTransport: "app", Success: true}},
		},
		{ID: "internal", Kind: domain.InternalReviewSession, Source: domain.SourceCodex, StartedAt: start},
	}

	report := Build(sessions)
	if report.Summary.Sessions != 2 || report.Summary.InputTokens != 400 || report.Summary.CachedInputTokens != 100 {
		t.Fatalf("summary=%+v", report.Summary)
	}
	if report.Summary.CacheRatio != 0.25 || report.Summary.FailedToolCalls != 1 || report.Summary.MCPCalls != 1 {
		t.Fatalf("weighted summary=%+v", report.Summary)
	}
	if report.Summary.DurationMS != int64(20*time.Minute/time.Millisecond) {
		t.Fatalf("active duration was not aggregated: %+v", report.Summary)
	}
	if len(report.Daily) != 2 || report.Daily[0].Date != "2026-09-19" || report.Daily[1].Date != "2026-09-20" {
		t.Fatalf("daily=%+v", report.Daily)
	}
	if len(report.Sources) != 2 || report.Sources[1].Name != "codex" || report.Sources[1].Sessions != 1 {
		t.Fatalf("sources=%+v", report.Sources)
	}
	if report.Tools[0].Name != "exec" || report.Tools[0].Failed != 1 {
		t.Fatalf("tools=%+v", report.Tools)
	}
	if len(report.Recommendations) == 0 || report.Recommendations[0].Confidence != "derived" {
		t.Fatalf("recommendations=%+v", report.Recommendations)
	}
}

func TestBuildMergesConcurrentSessionActivity(t *testing.T) {
	start := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	report := Build([]domain.Session{
		{ID: "a", Turns: []domain.Turn{{StartedAt: start, EndedAt: start.Add(10 * time.Minute)}}},
		{ID: "b", Turns: []domain.Turn{{StartedAt: start.Add(5 * time.Minute), EndedAt: start.Add(15 * time.Minute)}}},
	})
	if report.Summary.DurationMS != int64(15*time.Minute/time.Millisecond) {
		t.Fatalf("concurrent activity was double counted: %+v", report.Summary)
	}
}

func TestBuildReturnsStableEmptyArrays(t *testing.T) {
	report := Build(nil)
	if report.Daily == nil || report.Sources == nil || report.Models == nil || report.Repositories == nil || report.Tools == nil || report.Findings == nil || report.Recommendations == nil {
		t.Fatalf("empty report must serialize arrays: %+v", report)
	}
}
