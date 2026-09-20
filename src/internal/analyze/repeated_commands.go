// RepeatedCommands reports identical command signatures and repeated failures.
package analyze

import (
	"context"
	"fmt"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

type RepeatedCommands struct{}

func (RepeatedCommands) Name() string    { return "repeated-command" }
func (RepeatedCommands) Version() string { return "1.0.0" }
func (RepeatedCommands) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	groups := map[string][]domain.ToolCall{}
	for _, call := range s.ToolCalls {
		if call.CommandSignature != "" {
			groups[call.CommandSignature] = append(groups[call.CommandSignature], call)
		}
	}
	var findings []domain.Finding
	for signature, calls := range groups {
		if len(calls) < 2 {
			continue
		}
		failures := 0
		evidence := make([]string, 0, len(calls))
		for _, call := range calls {
			evidence = append(evidence, call.ID)
			if !call.Success {
				failures++
			}
		}
		if failures < 2 && len(calls) < 3 {
			continue
		}
		label := "Repeated command"
		summary := fmt.Sprintf("The same command signature ran %d times.", len(calls))
		severity := "info"
		if failures >= 2 {
			label = "Repeated failing command"
			summary = fmt.Sprintf("The same command failed %d times with no material command change.", failures)
			severity = "noteworthy"
		}
		findings = append(findings, domain.Finding{FindingType: "repeated_command", Severity: severity, Confidence: domain.Observed, Title: label, Summary: summary, Evidence: evidence, Metric: map[string]any{"count": len(calls), "failures": failures, "signature": signature}})
	}
	return findings, nil
}
