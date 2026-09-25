// Package analytics produces privacy-safe, cross-session observed aggregates.
// @module Analytics never retains prompt, response, argument, or tool-result text.
package analytics

import (
	"sort"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// Summary contains weighted totals across visible sessions.
type Summary struct {
	Sessions          int     `json:"sessions"`
	Turns             int     `json:"turns"`
	DurationMS        int64   `json:"duration_ms"`
	InputTokens       int64   `json:"input_tokens"`
	CachedInputTokens int64   `json:"cached_input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	ReasoningTokens   int64   `json:"reasoning_tokens"`
	CacheRatio        float64 `json:"cache_ratio"`
	ToolCalls         int     `json:"tool_calls"`
	MCPCalls          int     `json:"mcp_calls"`
	AppToolCalls      int     `json:"app_tool_calls"`
	FailedToolCalls   int     `json:"failed_tool_calls"`
	Findings          int     `json:"findings"`
}

// Daily is one calendar day's observed activity.
type Daily struct {
	Date            string `json:"date"`
	Sessions        int    `json:"sessions"`
	InputTokens     int64  `json:"input_tokens"`
	CachedTokens    int64  `json:"cached_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ToolCalls       int    `json:"tool_calls"`
	FailedToolCalls int    `json:"failed_tool_calls"`
}

// Breakdown groups sessions by an observed categorical field.
type Breakdown struct {
	Name            string `json:"name"`
	Sessions        int    `json:"sessions"`
	InputTokens     int64  `json:"input_tokens"`
	ToolCalls       int    `json:"tool_calls"`
	FailedToolCalls int    `json:"failed_tool_calls"`
}

// ToolAggregate groups exact observed tool identities.
type ToolAggregate struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Server    string `json:"server,omitempty"`
	Calls     int    `json:"calls"`
	Failed    int    `json:"failed"`
}

// FindingAggregate groups deterministic findings without session content.
type FindingAggregate struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	Count      int    `json:"count"`
	Sessions   int    `json:"sessions"`
}

// Recommendation is a deterministic cross-session suggestion with aggregate evidence.
type Recommendation struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Confidence string   `json:"confidence"`
	Evidence   []string `json:"evidence"`
}

// Inventory explains which local records contribute to Analytics and why the
// stored-record total can be larger than the analyzed-conversation total.
type Inventory struct {
	StoredRecords         int `json:"stored_records"`
	IncludedSessions      int `json:"included_sessions"`
	InternalReviewRecords int `json:"internal_review_records"`
	TelemetryOnlyRecords  int `json:"telemetry_only_records"`
	ParserWarningSessions int `json:"parser_warning_sessions"`
	ParserWarnings        int `json:"parser_warnings"`
	SkippedRecordWarnings int `json:"skipped_record_warnings"`
}

// Report is the complete local analytics response.
type Report struct {
	Summary         Summary            `json:"summary"`
	Inventory       Inventory          `json:"inventory"`
	Daily           []Daily            `json:"daily"`
	Sources         []Breakdown        `json:"sources"`
	Models          []Breakdown        `json:"models"`
	Repositories    []Breakdown        `json:"repositories"`
	Tools           []ToolAggregate    `json:"tools"`
	Findings        []FindingAggregate `json:"findings"`
	Recommendations []Recommendation   `json:"recommendations"`
}

// Build calculates aggregates from normalized records and excludes internal-review sessions.
func Build(sessions []domain.Session) Report {
	report := Report{
		Daily: []Daily{}, Sources: []Breakdown{}, Models: []Breakdown{}, Repositories: []Breakdown{},
		Tools: []ToolAggregate{}, Findings: []FindingAggregate{}, Recommendations: []Recommendation{},
	}
	daily := map[string]*Daily{}
	sources, models, repos := map[string]*Breakdown{}, map[string]*Breakdown{}, map[string]*Breakdown{}
	tools := map[string]*ToolAggregate{}
	type findingState struct {
		value    FindingAggregate
		sessions map[string]struct{}
	}
	findings := map[string]*findingState{}
	activeRanges := [][2]time.Time{}

	report.Inventory.StoredRecords = len(sessions)
	for _, session := range sessions {
		if len(session.Warnings) > 0 {
			report.Inventory.ParserWarningSessions++
		}
		for _, warning := range session.Warnings {
			report.Inventory.ParserWarnings++
			if WarningMayHaveSkippedRecords(warning) {
				report.Inventory.SkippedRecordWarnings++
			}
		}
		if session.Kind == domain.InternalReviewSession {
			report.Inventory.InternalReviewRecords++
			continue
		}
		if telemetryOnly(session) {
			report.Inventory.TelemetryOnlyRecords++
			continue
		}
		report.Inventory.IncludedSessions++
		metrics := domain.CalculateMetrics(session)
		report.Summary.Sessions++
		report.Summary.Turns += metrics.TurnCount
		for _, turn := range session.Turns {
			if turn.EndedAt.After(turn.StartedAt) {
				activeRanges = append(activeRanges, [2]time.Time{turn.StartedAt, turn.EndedAt})
			}
		}
		report.Summary.InputTokens += metrics.InputTokens
		report.Summary.CachedInputTokens += metrics.CachedInputTokens
		report.Summary.OutputTokens += metrics.OutputTokens
		report.Summary.ReasoningTokens += metrics.ReasoningTokens
		report.Summary.ToolCalls += metrics.ToolCalls
		report.Summary.MCPCalls += metrics.MCPCalls
		report.Summary.AppToolCalls += metrics.AppToolCalls
		report.Summary.FailedToolCalls += metrics.FailedToolCalls
		report.Summary.Findings += metrics.FindingCount

		date := session.StartedAt.UTC().Format("2006-01-02")
		if date != "0001-01-01" {
			item := daily[date]
			if item == nil {
				item = &Daily{Date: date}
				daily[date] = item
			}
			item.Sessions++
			item.InputTokens += metrics.InputTokens
			item.CachedTokens += metrics.CachedInputTokens
			item.OutputTokens += metrics.OutputTokens
			item.ToolCalls += metrics.ToolCalls
			item.FailedToolCalls += metrics.FailedToolCalls
		}
		addBreakdown(sources, observed(string(session.Source), "unknown"), metrics)
		addBreakdown(models, observed(session.Model, "unknown"), metrics)
		addBreakdown(repos, observed(session.RepoDisplayName, "conversation"), metrics)
		for _, call := range session.ToolCalls {
			transport := observed(call.ToolTransport, "builtin")
			key := transport + "\x00" + call.ToolServer + "\x00" + call.ToolName
			item := tools[key]
			if item == nil {
				item = &ToolAggregate{Name: call.ToolName, Transport: transport, Server: call.ToolServer}
				tools[key] = item
			}
			item.Calls++
			if !call.Success {
				item.Failed++
			}
		}
		for _, finding := range session.Findings {
			key := finding.FindingType + "\x00" + finding.Severity + "\x00" + string(finding.Confidence)
			item := findings[key]
			if item == nil {
				item = &findingState{value: FindingAggregate{Type: finding.FindingType, Severity: finding.Severity, Confidence: string(finding.Confidence)}, sessions: map[string]struct{}{}}
				findings[key] = item
			}
			item.value.Count++
			item.sessions[session.ID] = struct{}{}
		}
	}
	report.Summary.DurationMS = mergedDuration(activeRanges)
	if report.Summary.InputTokens > 0 {
		report.Summary.CacheRatio = float64(report.Summary.CachedInputTokens) / float64(report.Summary.InputTokens)
	}
	for _, item := range daily {
		report.Daily = append(report.Daily, *item)
	}
	sort.Slice(report.Daily, func(i, j int) bool { return report.Daily[i].Date < report.Daily[j].Date })
	report.Sources = sortedBreakdowns(sources)
	report.Models = sortedBreakdowns(models)
	report.Repositories = sortedBreakdowns(repos)
	for _, item := range tools {
		report.Tools = append(report.Tools, *item)
	}
	sort.Slice(report.Tools, func(i, j int) bool {
		if report.Tools[i].Calls != report.Tools[j].Calls {
			return report.Tools[i].Calls > report.Tools[j].Calls
		}
		return report.Tools[i].Name < report.Tools[j].Name
	})
	for _, item := range findings {
		item.value.Sessions = len(item.sessions)
		report.Findings = append(report.Findings, item.value)
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Count != report.Findings[j].Count {
			return report.Findings[i].Count > report.Findings[j].Count
		}
		return report.Findings[i].Type < report.Findings[j].Type
	})
	report.Recommendations = recommend(report)
	return report
}

func telemetryOnly(session domain.Session) bool {
	empty := len(session.Turns) == 0 && len(session.ToolCalls) == 0
	placeholder := session.Title == "Live Codex session" || (session.Source == domain.SourceCodex && session.Status == "live")
	return empty && placeholder
}

// WarningMayHaveSkippedRecords separates lossy parser warnings from
// compatibility notices where the unfamiliar record was preserved inertly.
func WarningMayHaveSkippedRecords(warning string) bool {
	warning = strings.ToLower(strings.TrimSpace(warning))
	return strings.Contains(warning, "malformed json") ||
		strings.Contains(warning, "record exceeds") ||
		(strings.HasPrefix(warning, "skipped ") && strings.Contains(warning, " unsupported "))
}

func mergedDuration(ranges [][2]time.Time) int64 {
	if len(ranges) == 0 {
		return 0
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0].Before(ranges[j][0]) })
	start, end := ranges[0][0], ranges[0][1]
	var total time.Duration
	for _, span := range ranges[1:] {
		if !span[0].After(end) {
			if span[1].After(end) {
				end = span[1]
			}
			continue
		}
		total += end.Sub(start)
		start, end = span[0], span[1]
	}
	total += end.Sub(start)
	return total.Milliseconds()
}

func addBreakdown(values map[string]*Breakdown, name string, metrics domain.Metrics) {
	item := values[name]
	if item == nil {
		item = &Breakdown{Name: name}
		values[name] = item
	}
	item.Sessions++
	item.InputTokens += metrics.InputTokens
	item.ToolCalls += metrics.ToolCalls
	item.FailedToolCalls += metrics.FailedToolCalls
}

func sortedBreakdowns(values map[string]*Breakdown) []Breakdown {
	result := make([]Breakdown, 0, len(values))
	for _, value := range values {
		result = append(result, *value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Sessions != result[j].Sessions {
			return result[i].Sessions > result[j].Sessions
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func recommend(report Report) []Recommendation {
	result := []Recommendation{}
	for _, finding := range report.Findings {
		if finding.Type == "possible_scope_correction" {
			result = append(result, Recommendation{ID: "scope-clarity", Title: "Clarify scope earlier", Summary: "Possible scope corrections recur in the observed session set. Consider stating the target component and validation criteria in the opening prompt.", Confidence: "derived", Evidence: []string{finding.Type + ": " + integer(finding.Count) + " observed finding(s) across " + integer(finding.Sessions) + " session(s)"}})
			break
		}
	}
	if report.Summary.Sessions > 0 && report.Summary.InputTokens > 0 && report.Summary.CacheRatio < 0.30 {
		result = append(result, Recommendation{ID: "cache-stability", Title: "Stabilize repeated context", Summary: "Weighted cache reuse is below 30%. Keep stable instructions and repository context in consistent prefixes where the client supports caching.", Confidence: "derived", Evidence: []string{"weighted cache reuse: " + percent(report.Summary.CacheRatio)}})
	}
	if report.Summary.ToolCalls > 0 && float64(report.Summary.FailedToolCalls)/float64(report.Summary.ToolCalls) >= 0.10 {
		result = append(result, Recommendation{ID: "tool-failures", Title: "Review recurring tool failures", Summary: "At least 10% of observed tool calls failed. Use the tool breakdown and session evidence to find repeatable validation or environment issues.", Confidence: "derived", Evidence: []string{integer(report.Summary.FailedToolCalls) + " failed of " + integer(report.Summary.ToolCalls) + " observed calls"}})
	}
	return result
}

func observed(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func integer(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}

func percent(value float64) string {
	whole := int(value*100 + 0.5)
	return integer(whole) + "%"
}
