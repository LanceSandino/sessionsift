// @module SyncSnapshots adapts normalized local sessions into explicitly scoped portable records.
package cloudsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type SnapshotSelection struct {
	Summaries        bool `json:"summaries"`
	FindingsNotes    bool `json:"findings_notes"`
	DetailedEvidence bool `json:"detailed_evidence"`
}

type SessionSnapshot struct {
	Label          string           `json:"label"`
	SourceDeviceID string           `json:"source_device_id"`
	LocalSessionID string           `json:"local_session_id"`
	Source         string           `json:"source"`
	Title          string           `json:"title"`
	Repository     string           `json:"repository,omitempty"`
	Model          string           `json:"model,omitempty"`
	StartedAt      string           `json:"started_at,omitempty"`
	EndedAt        string           `json:"ended_at,omitempty"`
	Status         string           `json:"status"`
	Metrics        *domain.Metrics  `json:"metrics,omitempty"`
	Findings       []domain.Finding `json:"findings,omitempty"`
	Evidence       []Evidence       `json:"evidence,omitempty"`
}

type Evidence struct {
	Kind    string `json:"kind"`
	TurnID  string `json:"turn_id,omitempty"`
	Summary string `json:"summary"`
}

type QueueSummary struct {
	Examined int   `json:"examined"`
	Queued   int   `json:"queued"`
	Bytes    int64 `json:"bytes"`
}

func QueueSnapshots(ctx context.Context, engine *Engine, store *storage.Store, selection SnapshotSelection) (QueueSummary, error) {
	if engine == nil || store == nil || (!selection.Summaries && !selection.FindingsNotes && !selection.DetailedEvidence) {
		return QueueSummary{}, nil
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return QueueSummary{}, err
	}
	summary := QueueSummary{Examined: len(sessions)}
	for _, session := range sessions {
		snapshot := buildSnapshot(session, engine.DeviceID, selection)
		queued, bytes, err := queuePortablePayload(ctx, engine, store, "session_summary", session.ID, snapshot)
		if err != nil {
			return summary, err
		}
		if queued {
			summary.Queued++
			summary.Bytes += bytes
		}
	}
	return summary, nil
}

func buildSnapshot(session domain.Session, deviceID string, selection SnapshotSelection) SessionSnapshot {
	value := SessionSnapshot{
		Label: "Synced snapshot from " + deviceID, SourceDeviceID: deviceID, LocalSessionID: session.ID, Source: string(session.Source),
	}
	if selection.Summaries {
		metrics := domain.CalculateMetrics(session)
		value.Title, value.Repository = redact.Text(session.Title, 500), redact.Text(session.RepoDisplayName, 500)
		value.Model, value.Status, value.Metrics = redact.Text(session.Model, 200), session.Status, &metrics
		if !session.StartedAt.IsZero() {
			value.StartedAt = session.StartedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		if !session.EndedAt.IsZero() {
			value.EndedAt = session.EndedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		}
	}
	if selection.FindingsNotes {
		value.Findings = session.Findings
	}
	if selection.DetailedEvidence {
		for index, turn := range session.Turns {
			if index >= 20 {
				break
			}
			if text := redact.Text(turn.UserPromptRedacted, 500); text != "" {
				value.Evidence = append(value.Evidence, Evidence{Kind: "prompt", TurnID: turn.ID, Summary: text})
			}
			if text := redact.Text(turn.AssistantSummaryRedacted, 500); text != "" {
				value.Evidence = append(value.Evidence, Evidence{Kind: "assistant", TurnID: turn.ID, Summary: text})
			}
		}
		for index, call := range session.ToolCalls {
			if index >= 50 {
				break
			}
			text := redact.Text(call.ToolName+" "+call.ArgumentsRedacted+" "+call.ResultPreviewRedacted, 500)
			if text != "" {
				value.Evidence = append(value.Evidence, Evidence{Kind: "tool", TurnID: call.TurnID, Summary: text})
			}
		}
	}
	return value
}

func queuePortablePayload(ctx context.Context, engine *Engine, store *storage.Store, kind, localID string, value any) (bool, int64, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return false, 0, err
	}
	objectID := ObjectID(engine.MasterKey, kind, localID)
	digest := sha256.Sum256(payload)
	revision, expected, parentDigest, changed, err := store.PrepareSyncRevision(ctx, objectID, hex.EncodeToString(digest[:]))
	if err != nil || !changed {
		return false, 0, err
	}
	record := Record{Schema: SchemaVersion, LocalID: localID, Kind: kind, Revision: revision, ParentDigest: parentDigest, Payload: payload}
	if err = engine.Queue(ctx, record, expected); err != nil {
		return false, 0, err
	}
	return true, int64(len(payload)), nil
}
