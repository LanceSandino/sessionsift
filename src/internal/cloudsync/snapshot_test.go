// Package cloudsync verifies the portable sync allowlist and explicit evidence consent boundary.
package cloudsync

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/optimize"
	"github.com/LanceSandino/sessionsift/src/internal/storage"
)

type snapshotTransport struct{}

func (snapshotTransport) Put(context.Context, WireObject, uint64) (Change, error) {
	return Change{}, nil
}
func (snapshotTransport) Changes(context.Context, int64, int) ([]Change, bool, error) {
	return nil, false, nil
}
func (snapshotTransport) Get(context.Context, string) (WireObject, error) { return WireObject{}, nil }

func TestSnapshotExcludesRawAndUnselectedEvidence(t *testing.T) {
	session := domain.Session{
		ID: "local-session-id", ExternalSessionID: "RAW-EXTERNAL-CANARY", CWD: "/Users/private/RAW-PATH-CANARY",
		Title: "redacted title", RepoDisplayName: "redacted repository", Source: domain.SourceCodex,
		Turns:     []domain.Turn{{ID: "turn-1", UserPromptRedacted: "EVIDENCE-CANARY", AssistantSummaryRedacted: "ASSISTANT-CANARY"}},
		ToolCalls: []domain.ToolCall{{TurnID: "turn-1", ToolName: "read_file", ArgumentsRedacted: "ARG-CANARY", ResultPreviewRedacted: "RESULT-CANARY"}},
		Events:    []domain.Event{{PayloadRedacted: map[string]any{"raw": "EVENT-CANARY"}}},
	}
	snapshot := buildSnapshot(session, "device-a", SnapshotSelection{Summaries: true})
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"RAW-EXTERNAL-CANARY", "RAW-PATH-CANARY", "EVIDENCE-CANARY", "ASSISTANT-CANARY", "ARG-CANARY", "RESULT-CANARY", "EVENT-CANARY"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("unselected or raw field %q escaped portable allowlist: %s", forbidden, body)
		}
	}
}

func TestSnapshotIncludesOnlyBoundedRedactedEvidenceAfterOptIn(t *testing.T) {
	session := domain.Session{ID: "session", Title: "title", Source: domain.SourceCodex,
		Turns:     []domain.Turn{{ID: "turn", UserPromptRedacted: strings.Repeat("x", 900)}},
		ToolCalls: []domain.ToolCall{{TurnID: "turn", ToolName: "read", ArgumentsRedacted: strings.Repeat("y", 900)}},
	}
	snapshot := buildSnapshot(session, "device-a", SnapshotSelection{Summaries: true, DetailedEvidence: true})
	if len(snapshot.Evidence) != 2 {
		t.Fatalf("evidence=%+v", snapshot.Evidence)
	}
	for _, evidence := range snapshot.Evidence {
		if len(evidence.Summary) > 503 {
			t.Fatalf("unbounded evidence length=%d", len(evidence.Summary))
		}
	}
}

func TestSnapshotCategoriesDoNotRequireOrLeakSummaryFields(t *testing.T) {
	session := domain.Session{ID: "session", Title: "SUMMARY-TITLE-CANARY", RepoDisplayName: "SUMMARY-REPO-CANARY", Source: domain.SourceCodex, Findings: []domain.Finding{{Title: "Selected finding"}}}
	snapshot := buildSnapshot(session, "device-a", SnapshotSelection{FindingsNotes: true})
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "" || snapshot.Repository != "" || snapshot.Metrics != nil || len(snapshot.Findings) != 1 || strings.Contains(string(body), "SUMMARY-TITLE-CANARY") || strings.Contains(string(body), "SUMMARY-REPO-CANARY") {
		t.Fatalf("findings-only category leaked summary fields: %s", body)
	}
}

func TestQueueSnapshotsIsDurableAndSkipsUnchangedContent(t *testing.T) {
	store, _ := storage.Open(filepath.Join(t.TempDir(), "local.db"))
	defer store.Close()
	now := time.Now().UTC()
	session := domain.Session{ID: "session", ExternalSessionID: "session", Title: "Portable summary", Source: domain.SourceCodex, StartedAt: now, EndedAt: now, ParserVersion: domain.ParserVersion, Findings: []domain.Finding{{Title: "Finding"}}}
	if err := store.SaveImport(context.Background(), "fixture", "one", session); err != nil {
		t.Fatal(err)
	}
	key, _ := GenerateMasterKey(rand.Reader)
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	engine := &Engine{Store: store, Transport: snapshotTransport{}, MasterKey: key, WorkspaceID: "workspace", DeviceID: "device", Sign: func(value []byte) []byte { return ed25519.Sign(private, value) }}
	first, err := QueueSnapshots(context.Background(), engine, store, SnapshotSelection{Summaries: true, FindingsNotes: true})
	if err != nil || first.Examined != 1 || first.Queued != 1 || first.Bytes == 0 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := QueueSnapshots(context.Background(), engine, store, SnapshotSelection{Summaries: true, FindingsNotes: true})
	if err != nil || second.Queued != 0 {
		t.Fatalf("unchanged=%+v err=%v", second, err)
	}
	if disabled, err := QueueSnapshots(context.Background(), nil, store, SnapshotSelection{}); err != nil || disabled.Queued != 0 {
		t.Fatalf("disabled=%+v err=%v", disabled, err)
	}
}

func TestDetailedSnapshotBoundsTurnAndToolEvidence(t *testing.T) {
	session := domain.Session{ID: "session", Source: domain.SourceCodex, StartedAt: time.Now(), EndedAt: time.Now()}
	for index := 0; index < 25; index++ {
		session.Turns = append(session.Turns, domain.Turn{ID: "turn", UserPromptRedacted: "prompt", AssistantSummaryRedacted: "reply"})
	}
	for index := 0; index < 55; index++ {
		session.ToolCalls = append(session.ToolCalls, domain.ToolCall{TurnID: "turn", ToolName: "tool"})
	}
	value := buildSnapshot(session, "device", SnapshotSelection{Summaries: true, DetailedEvidence: true})
	if len(value.Evidence) != 90 || value.StartedAt == "" || value.EndedAt == "" {
		t.Fatalf("bounded evidence=%d started=%q ended=%q", len(value.Evidence), value.StartedAt, value.EndedAt)
	}
}

func TestQueuePersonalStateIncludesSelectedNotesExperimentsAndPreferences(t *testing.T) {
	store, _ := storage.Open(filepath.Join(t.TempDir(), "personal.db"))
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.SaveExperiment(ctx, optimize.Experiment{ID: "exp-1", Title: "Try a checklist", BaselineSessionID: "session-1", Status: optimize.ExperimentRunning, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRecommendationDecision(ctx, optimize.RecommendationDecision{RecommendationID: "tool-failures", State: optimize.RecommendationApplied, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	key, _ := GenerateMasterKey(rand.Reader)
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	engine := &Engine{Store: store, Transport: snapshotTransport{}, MasterKey: key, WorkspaceID: "workspace", DeviceID: "device", Sign: func(value []byte) []byte { return ed25519.Sign(private, value) }}
	state := PersonalState{Notes: []PortableNote{{ID: "note-1", SessionID: "session-1", Author: "owner", Body: "Remember this", CreatedAt: now}}, Reviews: []PortableReview{{ID: "review-1", SessionID: "session-1", Reviewer: "owner", Status: "reviewed", Summary: "Looks good", CreatedAt: now}}}
	summary, err := QueuePersonalState(ctx, engine, store, SnapshotSelection{FindingsNotes: true}, state)
	if err != nil || summary.Examined != 5 || summary.Queued != 5 || summary.Bytes == 0 {
		t.Fatalf("personal summary=%+v err=%v", summary, err)
	}
	if pending, _ := store.PendingSyncCount(ctx); pending != 5 {
		t.Fatalf("personal pending=%d", pending)
	}
	if disabled, err := QueuePersonalState(ctx, engine, store, SnapshotSelection{}, state); err != nil || disabled.Examined != 0 {
		t.Fatalf("disabled personal state=%+v err=%v", disabled, err)
	}
}
