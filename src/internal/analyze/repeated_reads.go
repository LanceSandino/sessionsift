// RepeatedReads reports observed repeated file reads without calling them wasteful.
package analyze

import (
	"context"
	"fmt"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

type RepeatedReads struct{}

func (RepeatedReads) Name() string    { return "repeated-file-read" }
func (RepeatedReads) Version() string { return "1.0.0" }
func (RepeatedReads) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	type group struct {
		calls   []string
		turns   map[string]bool
		display string
	}
	groups := map[string]*group{}
	for _, call := range s.ToolCalls {
		if call.ToolCategory != "read" || call.TargetPathHash == "" {
			continue
		}
		if groups[call.TargetPathHash] == nil {
			groups[call.TargetPathHash] = &group{turns: map[string]bool{}, display: call.TargetPathDisplay}
		}
		groups[call.TargetPathHash].calls = append(groups[call.TargetPathHash].calls, call.ID)
		groups[call.TargetPathHash].turns[call.TurnID] = true
	}
	var findings []domain.Finding
	for _, item := range groups {
		if len(item.calls) < 3 {
			continue
		}
		severity := "info"
		if len(item.calls) >= 8 {
			severity = "high"
		} else if len(item.calls) >= 5 {
			severity = "noteworthy"
		}
		findings = append(findings, domain.Finding{FindingType: "repeated_file_read", Severity: severity, Confidence: domain.Observed, Title: "Repeated read", Summary: fmt.Sprintf("%s was read %d times across %d turns.", item.display, len(item.calls), len(item.turns)), Evidence: item.calls, Metric: map[string]any{"count": len(item.calls), "turns": len(item.turns)}})
	}
	return findings, nil
}
