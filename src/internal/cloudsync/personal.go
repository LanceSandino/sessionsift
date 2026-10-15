// @module SyncPersonalState adapts selected notes, reviews, experiments, and portable preferences into encrypted records.
package cloudsync

import (
	"context"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type PortableNote struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TurnID    string    `json:"turn_id,omitempty"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type PortableReview struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Reviewer  string    `json:"reviewer"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type PersonalState struct {
	Notes   []PortableNote
	Reviews []PortableReview
}

// QueuePersonalState adds only the explicitly selected, already-redacted local
// personal state. It never reads transcripts, bridge grants, credentials, or audit logs.
func QueuePersonalState(ctx context.Context, engine *Engine, store *storage.Store, selection SnapshotSelection, state PersonalState) (QueueSummary, error) {
	summary := QueueSummary{}
	if engine == nil || store == nil || !selection.FindingsNotes {
		return summary, nil
	}
	experiments, err := store.ListExperiments(ctx)
	if err != nil {
		return summary, err
	}
	decisions, err := store.ListRecommendationDecisions(ctx)
	if err != nil {
		return summary, err
	}
	records := make([]struct {
		kind, id string
		value    any
	}, 0, len(state.Notes)+len(state.Reviews)+len(experiments)+len(decisions)+1)
	for _, note := range state.Notes {
		records = append(records, struct {
			kind, id string
			value    any
		}{"note", "annotation:" + note.ID, note})
	}
	for _, review := range state.Reviews {
		records = append(records, struct {
			kind, id string
			value    any
		}{"note", "review:" + review.ID, review})
	}
	for _, experiment := range experiments {
		records = append(records, struct {
			kind, id string
			value    any
		}{"experiment", experiment.ID, experiment})
	}
	for _, decision := range decisions {
		records = append(records, struct {
			kind, id string
			value    any
		}{"preference", "recommendation:" + decision.RecommendationID, decision})
	}
	records = append(records, struct {
		kind, id string
		value    any
	}{"preference", "sync-categories", selection})
	for _, record := range records {
		summary.Examined++
		queued, bytes, queueErr := queuePortablePayload(ctx, engine, store, record.kind, record.id, record.value)
		if queueErr != nil {
			return summary, queueErr
		}
		if queued {
			summary.Queued++
			summary.Bytes += bytes
		}
	}
	return summary, nil
}
