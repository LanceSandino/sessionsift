// LargeResults reports context-heavy tool output using explicit approximation language.
package analyze

import (
	"context"
	"fmt"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

type LargeResults struct{}

func (LargeResults) Name() string    { return "large-tool-result" }
func (LargeResults) Version() string { return "1.0.0" }
func (LargeResults) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, call := range s.ToolCalls {
		bytes := len(call.ResultPreviewRedacted)
		if bytes < 1600 {
			continue
		}
		findings = append(findings, domain.Finding{TurnID: call.TurnID, FindingType: "large_tool_result", Severity: "info", Confidence: domain.Derived, Title: "Large tool result", Summary: fmt.Sprintf("A tool result retained an approximately %d-byte redacted preview and may have added substantial context.", bytes), Evidence: []string{call.ID}, Metric: map[string]any{"preview_bytes": bytes}})
	}
	return findings, nil
}
