package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/optimize"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

// SaveExperiment creates or updates one local experiment without copying session content.
func (s *Store) SaveExperiment(ctx context.Context, experiment optimize.Experiment) error {
	if err := experiment.Validate(); err != nil {
		return err
	}
	experiment.Title = redact.Title(experiment.Title, 160)
	experiment.Hypothesis = redact.Text(experiment.Hypothesis, 500)
	_, err := s.db.ExecContext(ctx, `INSERT INTO optimization_experiments(id,title,hypothesis,recommendation_id,baseline_session_id,followup_session_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,hypothesis=excluded.hypothesis,recommendation_id=excluded.recommendation_id,baseline_session_id=excluded.baseline_session_id,followup_session_id=excluded.followup_session_id,status=excluded.status,updated_at=excluded.updated_at`, experiment.ID, experiment.Title, experiment.Hypothesis, experiment.RecommendationID, experiment.BaselineSessionID, experiment.FollowupSessionID, experiment.Status, formatTime(experiment.CreatedAt), formatTime(experiment.UpdatedAt))
	return err
}

// ListExperiments returns most recently changed experiments first.
func (s *Store) ListExperiments(ctx context.Context) ([]optimize.Experiment, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT id,title,hypothesis,recommendation_id,baseline_session_id,followup_session_id,status,created_at,updated_at FROM optimization_experiments ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	experiments := []optimize.Experiment{}
	for rows.Next() {
		var experiment optimize.Experiment
		var created, updated string
		if err := rows.Scan(&experiment.ID, &experiment.Title, &experiment.Hypothesis, &experiment.RecommendationID, &experiment.BaselineSessionID, &experiment.FollowupSessionID, &experiment.Status, &created, &updated); err != nil {
			return nil, err
		}
		experiment.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse experiment created_at: %w", err)
		}
		experiment.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse experiment updated_at: %w", err)
		}
		experiments = append(experiments, experiment)
	}
	return experiments, rows.Err()
}

// SetRecommendationDecision records the owner's explicit lifecycle choice.
func (s *Store) SetRecommendationDecision(ctx context.Context, decision optimize.RecommendationDecision) error {
	if err := decision.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO optimization_recommendations(recommendation_id,state,updated_at) VALUES(?,?,?) ON CONFLICT(recommendation_id) DO UPDATE SET state=excluded.state,updated_at=excluded.updated_at`, decision.RecommendationID, decision.State, formatTime(decision.UpdatedAt))
	return err
}

func (s *Store) ListRecommendationDecisions(ctx context.Context) ([]optimize.RecommendationDecision, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT recommendation_id,state,updated_at FROM optimization_recommendations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	decisions := []optimize.RecommendationDecision{}
	for rows.Next() {
		var decision optimize.RecommendationDecision
		var updated string
		if err := rows.Scan(&decision.RecommendationID, &decision.State, &updated); err != nil {
			return nil, err
		}
		decision.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, fmt.Errorf("parse recommendation updated_at: %w", err)
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

// RecordAdoption increments one content-free daily counter from a closed allowlist.
func (s *Store) RecordAdoption(ctx context.Context, event optimize.AdoptionEvent, at time.Time) error {
	columns := map[optimize.AdoptionEvent]string{
		optimize.AdoptionOptimizeViewed:         "optimize_views",
		optimize.AdoptionExperimentsCreated:     "experiments_created",
		optimize.AdoptionRecommendationsChanged: "recommendations_changed",
		optimize.AdoptionReportsExported:        "reports_exported",
	}
	column, ok := columns[event]
	if !ok || !event.Valid() {
		return fmt.Errorf("invalid adoption event")
	}
	day := at.UTC().Format("2006-01-02")
	query := `INSERT INTO optimization_adoption_daily(day,` + column + `) VALUES(?,1) ON CONFLICT(day) DO UPDATE SET ` + column + `=` + column + `+1`
	_, err := s.db.ExecContext(ctx, query, day)
	return err
}

// AdoptionSummary aggregates only local action counts in the requested window.
func (s *Store) AdoptionSummary(ctx context.Context, start, end time.Time) (optimize.AdoptionSummary, error) {
	var summary optimize.AdoptionSummary
	err := s.reader().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(optimize_views),0),COALESCE(SUM(experiments_created),0),COALESCE(SUM(recommendations_changed),0),COALESCE(SUM(reports_exported),0) FROM optimization_adoption_daily WHERE day>=? AND day<=?`, start.UTC().Format("2006-01-02"), end.UTC().Format("2006-01-02")).Scan(&summary.ActiveDays, &summary.OptimizeViews, &summary.ExperimentsCreated, &summary.RecommendationsChanged, &summary.ReportsExported)
	return summary, err
}
