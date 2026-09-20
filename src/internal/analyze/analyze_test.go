package analyze

import (
	"context"
	"testing"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestDefaultAnalyzersProduceEvidenceBackedFindings(t *testing.T) {
	base := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	session := domain.Session{ID: "s1", Turns: []domain.Turn{
		{ID: "t1", ExternalTurnID: "t1", Sequence: 1, StartedAt: base, UserPromptRedacted: "Fix auth"},
		{ID: "t2", ExternalTurnID: "t2", Sequence: 2, StartedAt: base.Add(5 * time.Minute), UserPromptRedacted: "No, only change the Cognito path"},
	}, Events: []domain.Event{{ID: "compact", EventType: "compaction", Timestamp: base.Add(3 * time.Minute)}}}
	for index := 0; index < 3; index++ {
		session.ToolCalls = append(session.ToolCalls, domain.ToolCall{ID: string(rune('a' + index)), TurnID: "t1", Timestamp: base.Add(time.Duration(index) * time.Minute), ToolName: "read_file", ToolCategory: "read", TargetPathHash: "hash", TargetPathDisplay: "src/auth.go", Success: true})
	}
	session.ToolCalls = append(session.ToolCalls, domain.ToolCall{ID: "reread", TurnID: "t2", Timestamp: base.Add(4 * time.Minute), ToolName: "read_file", ToolCategory: "read", TargetPathHash: "hash", TargetPathDisplay: "src/auth.go", Success: true})
	for index := 0; index < 3; index++ {
		session.ToolCalls = append(session.ToolCalls, domain.ToolCall{ID: string(rune('d' + index)), TurnID: "t2", Timestamp: base.Add(time.Duration(5+index) * time.Minute), ToolName: "exec", ToolCategory: "validation", CommandSignature: "go test ./...", Success: false})
	}
	findings, err := Run(context.Background(), session, Default())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, finding := range findings {
		kinds[finding.FindingType] = true
		if finding.RuleVersion == "" || finding.ID == "" {
			t.Fatalf("missing provenance: %+v", finding)
		}
	}
	for _, want := range []string{"repeated_file_read", "repeated_command", "failed_tool_loop", "possible_scope_correction", "compaction_rediscovery", "exploration_ratio"} {
		if !kinds[want] {
			t.Errorf("missing %s in %+v", want, kinds)
		}
	}
}

func TestAnalyzersHandleEmptySession(t *testing.T) {
	findings, err := Run(context.Background(), domain.Session{ID: "empty"}, Default())
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestLargeResultAnalyzer(t *testing.T) {
	session := domain.Session{ID: "large", ToolCalls: []domain.ToolCall{{ID: "call", ResultPreviewRedacted: string(make([]byte, 1700))}}}
	analyzer := LargeResults{}
	if analyzer.Name() == "" || analyzer.Version() == "" {
		t.Fatal("missing identity")
	}
	findings, err := analyzer.Analyze(context.Background(), session)
	if err != nil || len(findings) != 1 || findings[0].FindingType != "large_tool_result" {
		t.Fatalf("findings=%v err=%v", findings, err)
	}
}

func TestTaskShiftAnalyzer(t *testing.T) {
	base := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	session := domain.Session{ID: "shift", Turns: []domain.Turn{{ID: "t1", Sequence: 1}, {ID: "t2", Sequence: 2}}, ToolCalls: []domain.ToolCall{
		{ID: "a", TurnID: "t1", Timestamp: base, TargetPathDisplay: "terraform/main.tf"},
		{ID: "b", TurnID: "t2", Timestamp: base.Add(time.Minute), TargetPathDisplay: "k8s/deployment.yaml"},
	}}
	analyzer := TaskShift{}
	if analyzer.Name() == "" || analyzer.Version() == "" {
		t.Fatal("missing analyzer identity")
	}
	findings, err := analyzer.Analyze(context.Background(), session)
	if err != nil || len(findings) != 1 || findings[0].FindingType != "possible_task_shift" || findings[0].Confidence != domain.Possible {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
}
