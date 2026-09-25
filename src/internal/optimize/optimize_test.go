package optimize

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/analytics"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestBuildOverviewMeasuresCompletedExperimentWithoutScoringWinner(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	baseline := measuredSession("before", "Before", now.Add(-48*time.Hour), 1000, 10, 3)
	followup := measuredSession("after", "After", now.Add(-24*time.Hour), 700, 6, 1)
	experiments := []Experiment{{ID: "exp_1", Title: "Tighter validation", Hypothesis: "Validate earlier", RecommendationID: "tool-failures", BaselineSessionID: baseline.ID, FollowupSessionID: followup.ID, Status: ExperimentMeasured, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now}}
	decisions := []RecommendationDecision{{RecommendationID: "tool-failures", State: RecommendationApplied, UpdatedAt: now}}
	report := analytics.Build([]domain.Session{baseline, followup})

	overview := BuildOverview([]domain.Session{baseline, followup}, report, experiments, decisions, AdoptionSummary{ActiveDays: 2, ExperimentsCreated: 1}, now)
	if len(overview.Experiments) != 1 || overview.Experiments[0].Measurement == nil {
		t.Fatalf("experiment was not measured: %+v", overview.Experiments)
	}
	measurement := overview.Experiments[0].Measurement
	if measurement.Confidence != string(domain.Observed) || len(measurement.Deltas) == 0 {
		t.Fatalf("measurement=%+v", measurement)
	}
	for _, delta := range measurement.Deltas {
		if strings.Contains(strings.ToLower(delta.Interpretation), "winner") || strings.Contains(strings.ToLower(delta.Interpretation), "better") {
			t.Fatalf("scoring language leaked into descriptive delta: %+v", delta)
		}
	}
	if overview.Summary.MeasuredExperiments != 1 || overview.Adoption.ActiveDays != 2 {
		t.Fatalf("summary=%+v adoption=%+v", overview.Summary, overview.Adoption)
	}
	markdown := Markdown(overview)
	for _, want := range []string{"# SessionSift weekly optimization report", "Tighter validation", "Observed deltas", "No conversation text"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "secret prompt") {
		t.Fatal("conversation content leaked into report")
	}
	exported, err := json.Marshal(SafeExport(overview))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"before", "after", "Baseline session", "Follow-up session", "secret prompt"} {
		if strings.Contains(string(exported), forbidden) {
			t.Fatalf("safe JSON export leaked %q: %s", forbidden, exported)
		}
	}
}

func TestBuildOverviewKeepsExperimentRunningUntilFollowupExists(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	baseline := measuredSession("before", "Before", now.Add(-time.Hour), 100, 2, 0)
	experiment := Experiment{ID: "exp", Title: "Try a checklist", BaselineSessionID: baseline.ID, Status: ExperimentRunning, CreatedAt: now, UpdatedAt: now}
	overview := BuildOverview([]domain.Session{baseline}, analytics.Build([]domain.Session{baseline}), []Experiment{experiment}, nil, AdoptionSummary{}, now)
	if overview.Experiments[0].Measurement != nil || overview.Experiments[0].Status != ExperimentRunning {
		t.Fatalf("experiment=%+v", overview.Experiments[0])
	}
}

func TestValidationRejectsUnknownStatesAndMissingBaseline(t *testing.T) {
	if RecommendationState("unknown").Valid() {
		t.Fatal("unknown recommendation state accepted")
	}
	if err := (Experiment{Title: "x"}).Validate(); err == nil {
		t.Fatal("missing baseline accepted")
	}
}

func measuredSession(id, title string, started time.Time, input int64, calls, failed int) domain.Session {
	callsList := make([]domain.ToolCall, calls)
	for i := range callsList {
		callsList[i] = domain.ToolCall{ID: id + string(rune('a'+i)), SessionID: id, Timestamp: started.Add(time.Duration(i+1) * time.Minute), ToolName: "tool", ToolCategory: "validation", Success: i >= failed}
	}
	return domain.Session{ID: id, ExternalSessionID: id, Title: title, Source: domain.SourceCodex, Kind: domain.DeveloperSession, StartedAt: started, EndedAt: started.Add(20 * time.Minute), Turns: []domain.Turn{{ID: id + "-turn", SessionID: id, StartedAt: started, EndedAt: started.Add(20 * time.Minute), InputTokens: input, UserPromptRedacted: "secret prompt"}}, ToolCalls: callsList}
}
