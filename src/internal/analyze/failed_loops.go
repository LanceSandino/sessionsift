// FailedLoops detects consecutive tool failures before inputs change materially.
package analyze

import (
	"context"
	"fmt"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

type FailedLoops struct{}

func (FailedLoops) Name() string    { return "failed-tool-loop" }
func (FailedLoops) Version() string { return "1.0.0" }
func (FailedLoops) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	var findings []domain.Finding
	for start := 0; start < len(s.ToolCalls); {
		if s.ToolCalls[start].Success {
			start++
			continue
		}
		end := start + 1
		for end < len(s.ToolCalls) && !s.ToolCalls[end].Success && similarTarget(s.ToolCalls[start], s.ToolCalls[end]) {
			end++
		}
		if end-start >= 3 {
			evidence := make([]string, 0, end-start)
			for _, call := range s.ToolCalls[start:end] {
				evidence = append(evidence, call.ID)
			}
			findings = append(findings, domain.Finding{FindingType: "failed_tool_loop", Severity: "noteworthy", Confidence: domain.Derived, Title: "Failed tool loop", Summary: fmt.Sprintf("%d similar tool failures occurred before the target or command changed.", end-start), Evidence: evidence, Metric: map[string]any{"count": end - start}})
		}
		start = end
	}
	return findings, nil
}

func similarTarget(a, b domain.ToolCall) bool {
	if a.ToolName != b.ToolName {
		return false
	}
	if a.TargetPathHash != "" || b.TargetPathHash != "" {
		return a.TargetPathHash == b.TargetPathHash
	}
	return a.CommandSignature == b.CommandSignature
}
