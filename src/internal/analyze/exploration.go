// ExplorationRatio describes the share of observed tool activity spent reading and searching.
package analyze

import (
	"context"
	"fmt"
	"math"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

type ExplorationRatio struct{}

func (ExplorationRatio) Name() string    { return "exploration-ratio" }
func (ExplorationRatio) Version() string { return "1.0.0" }
func (ExplorationRatio) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	if len(s.ToolCalls) == 0 {
		return nil, nil
	}
	exploration := 0
	evidence := []string{}
	for _, call := range s.ToolCalls {
		if call.ToolCategory == "read" || call.ToolCategory == "search" {
			exploration++
			evidence = append(evidence, call.ID)
		}
	}
	ratio := float64(exploration) / float64(len(s.ToolCalls))
	percent := int(math.Round(ratio * 100))
	return []domain.Finding{{FindingType: "exploration_ratio", Severity: "info", Confidence: domain.Derived, Title: "Exploration ratio", Summary: fmt.Sprintf("%d%% of observed tool activity was reading or searching.", percent), Evidence: evidence, Metric: map[string]any{"ratio": ratio, "exploration_calls": exploration, "tool_calls": len(s.ToolCalls)}}}, nil
}
