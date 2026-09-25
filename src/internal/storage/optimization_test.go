package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/optimize"
)

func TestOptimizationStatePersistsAndAdoptionAggregatesByDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "optimization.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	experiment := optimize.Experiment{ID: "exp_1", Title: "Validate first", Hypothesis: "Earlier validation reduces retries", RecommendationID: "tool-failures", BaselineSessionID: "s1", Status: optimize.ExperimentRunning, CreatedAt: now, UpdatedAt: now}
	if err = store.SaveExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	experiment.FollowupSessionID = "s2"
	experiment.Status = optimize.ExperimentMeasured
	if err = store.SaveExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	if err = store.SetRecommendationDecision(ctx, optimize.RecommendationDecision{RecommendationID: "tool-failures", State: optimize.RecommendationApplied, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordAdoption(ctx, optimize.AdoptionExperimentsCreated, now); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordAdoption(ctx, optimize.AdoptionExperimentsCreated, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordAdoption(ctx, optimize.AdoptionReportsExported, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	experiments, err := store.ListExperiments(ctx)
	if err != nil || len(experiments) != 1 || experiments[0].FollowupSessionID != "s2" {
		t.Fatalf("experiments=%+v err=%v", experiments, err)
	}
	decisions, err := store.ListRecommendationDecisions(ctx)
	if err != nil || len(decisions) != 1 || decisions[0].State != optimize.RecommendationApplied {
		t.Fatalf("decisions=%+v err=%v", decisions, err)
	}
	adoption, err := store.AdoptionSummary(ctx, now.Add(-time.Hour), now.Add(48*time.Hour))
	if err != nil || adoption.ActiveDays != 2 || adoption.ExperimentsCreated != 2 || adoption.ReportsExported != 1 {
		t.Fatalf("adoption=%+v err=%v", adoption, err)
	}
}

func TestOptimizationStoreRejectsInvalidValues(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "optimization.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveExperiment(context.Background(), optimize.Experiment{ID: "x"}); err == nil {
		t.Fatal("invalid experiment accepted")
	}
	if err := store.SetRecommendationDecision(context.Background(), optimize.RecommendationDecision{RecommendationID: "x", State: "nope"}); err == nil {
		t.Fatal("invalid decision accepted")
	}
	if err := store.RecordAdoption(context.Background(), "secret_payload", time.Now()); err == nil {
		t.Fatal("unknown adoption event accepted")
	}
}
