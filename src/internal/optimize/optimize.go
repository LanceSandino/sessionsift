// Package optimize turns privacy-safe analytics into a deliberate, measurable
// local improvement loop. It never reads or exports conversation bodies.
// @module OptimizationLoop models recommendation decisions, experiments, observed deltas, and weekly reports.
package optimize

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/analytics"
	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// RecommendationState is the owner's explicit position on one deterministic recommendation.
type RecommendationState string

const (
	RecommendationProposed  RecommendationState = "proposed"
	RecommendationAccepted  RecommendationState = "accepted"
	RecommendationApplied   RecommendationState = "applied"
	RecommendationDismissed RecommendationState = "dismissed"
	RecommendationMeasured  RecommendationState = "measured"
)

func (state RecommendationState) Valid() bool {
	switch state {
	case RecommendationProposed, RecommendationAccepted, RecommendationApplied, RecommendationDismissed, RecommendationMeasured:
		return true
	default:
		return false
	}
}

// RecommendationDecision persists only an aggregate recommendation ID and lifecycle state.
type RecommendationDecision struct {
	RecommendationID string              `json:"recommendation_id"`
	State            RecommendationState `json:"state"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

func (decision RecommendationDecision) Validate() error {
	if strings.TrimSpace(decision.RecommendationID) == "" || len(decision.RecommendationID) > 120 {
		return errors.New("a valid recommendation id is required")
	}
	if !decision.State.Valid() {
		return errors.New("a valid recommendation state is required")
	}
	return nil
}

// ExperimentStatus tracks whether an experiment is awaiting a follow-up or has observed deltas.
type ExperimentStatus string

const (
	ExperimentRunning  ExperimentStatus = "running"
	ExperimentMeasured ExperimentStatus = "measured"
)

// Experiment connects a baseline session to a later follow-up without copying either transcript.
type Experiment struct {
	ID                string           `json:"id"`
	Title             string           `json:"title"`
	Hypothesis        string           `json:"hypothesis,omitempty"`
	RecommendationID  string           `json:"recommendation_id,omitempty"`
	BaselineSessionID string           `json:"baseline_session_id"`
	FollowupSessionID string           `json:"followup_session_id,omitempty"`
	Status            ExperimentStatus `json:"status"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

func (experiment Experiment) Validate() error {
	if strings.TrimSpace(experiment.ID) == "" || len(experiment.ID) > 120 {
		return errors.New("a valid experiment id is required")
	}
	if strings.TrimSpace(experiment.Title) == "" || len(experiment.Title) > 160 {
		return errors.New("an experiment title is required")
	}
	if len(experiment.Hypothesis) > 500 || len(experiment.RecommendationID) > 120 {
		return errors.New("experiment text is too long")
	}
	if strings.TrimSpace(experiment.BaselineSessionID) == "" || len(experiment.BaselineSessionID) > 256 || len(experiment.FollowupSessionID) > 256 {
		return errors.New("a valid baseline session is required")
	}
	if experiment.FollowupSessionID != "" && experiment.FollowupSessionID == experiment.BaselineSessionID {
		return errors.New("baseline and follow-up sessions must differ")
	}
	if experiment.Status != ExperimentRunning && experiment.Status != ExperimentMeasured {
		return errors.New("a valid experiment status is required")
	}
	if experiment.Status == ExperimentMeasured && experiment.FollowupSessionID == "" {
		return errors.New("a measured experiment requires a follow-up session")
	}
	return nil
}

// AdoptionEvent is an allowlisted, content-free local product action.
type AdoptionEvent string

const (
	AdoptionOptimizeViewed         AdoptionEvent = "optimize_viewed"
	AdoptionExperimentsCreated     AdoptionEvent = "experiments_created"
	AdoptionRecommendationsChanged AdoptionEvent = "recommendations_changed"
	AdoptionReportsExported        AdoptionEvent = "reports_exported"
)

func (event AdoptionEvent) Valid() bool {
	switch event {
	case AdoptionOptimizeViewed, AdoptionExperimentsCreated, AdoptionRecommendationsChanged, AdoptionReportsExported:
		return true
	default:
		return false
	}
}

// AdoptionSummary contains local counters only; there are no actor, session, or content dimensions.
type AdoptionSummary struct {
	ActiveDays             int `json:"active_days"`
	OptimizeViews          int `json:"optimize_views"`
	ExperimentsCreated     int `json:"experiments_created"`
	RecommendationsChanged int `json:"recommendations_changed"`
	ReportsExported        int `json:"reports_exported"`
}

// MetricDelta describes a raw follow-up minus baseline observation.
type MetricDelta struct {
	Metric         string  `json:"metric"`
	Baseline       float64 `json:"baseline"`
	Followup       float64 `json:"followup"`
	Delta          float64 `json:"delta"`
	Unit           string  `json:"unit"`
	Interpretation string  `json:"interpretation"`
}

// Measurement is an evidence-backed comparison; it deliberately has no score or winner.
type Measurement struct {
	Confidence string        `json:"confidence"`
	Deltas     []MetricDelta `json:"deltas"`
	Evidence   []string      `json:"evidence"`
}

type ExperimentView struct {
	Experiment
	BaselineTitle string       `json:"baseline_title,omitempty"`
	FollowupTitle string       `json:"followup_title,omitempty"`
	Measurement   *Measurement `json:"measurement,omitempty"`
}

type RecommendationView struct {
	analytics.Recommendation
	State RecommendationState `json:"state"`
}

type WeeklySummary struct {
	Sessions            int `json:"sessions"`
	Turns               int `json:"turns"`
	ToolCalls           int `json:"tool_calls"`
	FailedToolCalls     int `json:"failed_tool_calls"`
	Findings            int `json:"findings"`
	RunningExperiments  int `json:"running_experiments"`
	MeasuredExperiments int `json:"measured_experiments"`
}

// Overview is the complete local optimization workspace and export payload.
type Overview struct {
	GeneratedAt     time.Time            `json:"generated_at"`
	PeriodStart     time.Time            `json:"period_start"`
	Summary         WeeklySummary        `json:"summary"`
	Recommendations []RecommendationView `json:"recommendations"`
	Experiments     []ExperimentView     `json:"experiments"`
	Adoption        AdoptionSummary      `json:"adoption"`
	Privacy         []string             `json:"privacy"`
}

// ExportReport omits session identifiers and prompt-derived titles while
// retaining owner-authored experiment labels and aggregate measurements.
type ExportReport struct {
	GeneratedAt     time.Time            `json:"generated_at"`
	PeriodStart     time.Time            `json:"period_start"`
	Summary         WeeklySummary        `json:"summary"`
	Recommendations []RecommendationView `json:"recommendations"`
	Experiments     []ExportExperiment   `json:"experiments"`
	Adoption        AdoptionSummary      `json:"adoption"`
	Privacy         []string             `json:"privacy"`
}

type ExportExperiment struct {
	Title            string           `json:"title"`
	Hypothesis       string           `json:"hypothesis,omitempty"`
	RecommendationID string           `json:"recommendation_id,omitempty"`
	Status           ExperimentStatus `json:"status"`
	Measurement      *Measurement     `json:"measurement,omitempty"`
}

// SafeExport returns the report surface that is suitable for deliberate download.
func SafeExport(overview Overview) ExportReport {
	experiments := make([]ExportExperiment, 0, len(overview.Experiments))
	for _, experiment := range overview.Experiments {
		var measurement *Measurement
		if experiment.Measurement != nil {
			copyValue := *experiment.Measurement
			copyValue.Evidence = []string{"Observed metrics from one baseline and one follow-up session."}
			measurement = &copyValue
		}
		experiments = append(experiments, ExportExperiment{Title: experiment.Title, Hypothesis: experiment.Hypothesis, RecommendationID: experiment.RecommendationID, Status: experiment.Status, Measurement: measurement})
	}
	return ExportReport{GeneratedAt: overview.GeneratedAt, PeriodStart: overview.PeriodStart, Summary: overview.Summary, Recommendations: overview.Recommendations, Experiments: experiments, Adoption: overview.Adoption, Privacy: overview.Privacy}
}

// BuildOverview joins aggregate analytics to owner-managed state and observed session metrics.
func BuildOverview(sessions []domain.Session, report analytics.Report, experiments []Experiment, decisions []RecommendationDecision, adoption AdoptionSummary, now time.Time) Overview {
	now = now.UTC()
	periodStart := now.AddDate(0, 0, -7)
	recent := make([]domain.Session, 0, len(sessions))
	byID := make(map[string]domain.Session, len(sessions))
	for _, session := range sessions {
		byID[session.ID] = session
		if !session.StartedAt.Before(periodStart) && !session.StartedAt.After(now) {
			recent = append(recent, session)
		}
	}
	weekly := analytics.Build(recent)
	states := make(map[string]RecommendationState, len(decisions))
	for _, decision := range decisions {
		states[decision.RecommendationID] = decision.State
	}
	overview := Overview{
		GeneratedAt: now, PeriodStart: periodStart, Adoption: adoption,
		Summary:         WeeklySummary{Sessions: weekly.Summary.Sessions, Turns: weekly.Summary.Turns, ToolCalls: weekly.Summary.ToolCalls, FailedToolCalls: weekly.Summary.FailedToolCalls, Findings: weekly.Summary.Findings},
		Privacy:         []string{"No conversation text, assistant response, tool argument, or tool result is included.", "Measurements are observed deltas, not productivity scores or automatic quality judgments.", "Adoption counters stay in this local database and have no session or actor dimension."},
		Recommendations: make([]RecommendationView, 0, len(report.Recommendations)), Experiments: make([]ExperimentView, 0, len(experiments)),
	}
	for _, recommendation := range report.Recommendations {
		state := states[recommendation.ID]
		if !state.Valid() {
			state = RecommendationProposed
		}
		overview.Recommendations = append(overview.Recommendations, RecommendationView{Recommendation: recommendation, State: state})
	}
	for _, experiment := range experiments {
		view := ExperimentView{Experiment: experiment}
		baseline, baselineFound := byID[experiment.BaselineSessionID]
		if baselineFound {
			view.BaselineTitle = baseline.Title
		}
		followup, followupFound := byID[experiment.FollowupSessionID]
		if followupFound {
			view.FollowupTitle = followup.Title
		}
		if experiment.Status == ExperimentMeasured && baselineFound && followupFound {
			view.Measurement = measure(baseline, followup)
			overview.Summary.MeasuredExperiments++
		} else {
			overview.Summary.RunningExperiments++
		}
		overview.Experiments = append(overview.Experiments, view)
	}
	sort.SliceStable(overview.Experiments, func(i, j int) bool { return overview.Experiments[i].UpdatedAt.After(overview.Experiments[j].UpdatedAt) })
	return overview
}

func measure(baseline, followup domain.Session) *Measurement {
	a, b := domain.CalculateMetrics(baseline), domain.CalculateMetrics(followup)
	values := []struct {
		name, unit string
		a, b       float64
	}{
		{"Input tokens", "tokens", float64(a.InputTokens), float64(b.InputTokens)},
		{"Cache reuse", "percentage points", a.CacheRatio * 100, b.CacheRatio * 100},
		{"Tool calls", "calls", float64(a.ToolCalls), float64(b.ToolCalls)},
		{"Failed calls", "calls", float64(a.FailedToolCalls), float64(b.FailedToolCalls)},
		{"Findings", "findings", float64(a.FindingCount), float64(b.FindingCount)},
		{"Active work", "seconds", float64(a.DurationMS) / 1000, float64(b.DurationMS) / 1000},
		{"Time to first validation", "seconds", float64(a.TimeToFirstValidationMS) / 1000, float64(b.TimeToFirstValidationMS) / 1000},
	}
	deltas := make([]MetricDelta, 0, len(values))
	for _, value := range values {
		delta := round(value.b-value.a, 2)
		deltas = append(deltas, MetricDelta{Metric: value.name, Baseline: round(value.a, 2), Followup: round(value.b, 2), Delta: delta, Unit: value.unit, Interpretation: fmt.Sprintf("Follow-up changed by %+.2f %s.", delta, value.unit)})
	}
	return &Measurement{Confidence: string(domain.Observed), Deltas: deltas, Evidence: []string{"Baseline session " + baseline.ID, "Follow-up session " + followup.ID}}
}

func round(value float64, places int) float64 {
	power := math.Pow10(places)
	return math.Round(value*power) / power
}

// Markdown renders the same bounded aggregate report shown by the UI.
func Markdown(overview Overview) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SessionSift weekly optimization report\n\nGenerated %s for %s through %s.\n\n", overview.GeneratedAt.Format(time.RFC3339), overview.PeriodStart.Format("2006-01-02"), overview.GeneratedAt.Format("2006-01-02"))
	fmt.Fprintf(&out, "## Activity\n\n- Sessions: %d\n- Prompts: %d\n- Tool calls: %d\n- Failed calls: %d\n- Findings: %d\n- Running experiments: %d\n- Measured experiments: %d\n\n", overview.Summary.Sessions, overview.Summary.Turns, overview.Summary.ToolCalls, overview.Summary.FailedToolCalls, overview.Summary.Findings, overview.Summary.RunningExperiments, overview.Summary.MeasuredExperiments)
	fmt.Fprintln(&out, "## Experiments")
	if len(overview.Experiments) == 0 {
		fmt.Fprintln(&out, "\nNo experiments yet.")
	}
	for _, experiment := range overview.Experiments {
		fmt.Fprintf(&out, "\n### %s\n\nStatus: %s\n", experiment.Title, experiment.Status)
		if experiment.Hypothesis != "" {
			fmt.Fprintf(&out, "\nHypothesis: %s\n", experiment.Hypothesis)
		}
		if experiment.Measurement != nil {
			fmt.Fprintln(&out, "\nObserved deltas:")
			for _, delta := range experiment.Measurement.Deltas {
				fmt.Fprintf(&out, "\n- %s: %.2f to %.2f (%+.2f %s)", delta.Metric, delta.Baseline, delta.Followup, delta.Delta, delta.Unit)
			}
			fmt.Fprintln(&out)
		}
	}
	fmt.Fprintln(&out, "\n## Privacy")
	fmt.Fprintln(&out)
	for _, statement := range overview.Privacy {
		fmt.Fprintf(&out, "- %s\n", statement)
	}
	return out.String()
}
