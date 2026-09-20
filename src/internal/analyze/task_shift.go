// TaskShift identifies evidence that activity moved between distinct path families.
package analyze

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// TaskShift is intentionally conservative and labels every result as possible.
type TaskShift struct{}

func (TaskShift) Name() string    { return "task-shift" }
func (TaskShift) Version() string { return "1.0.0" }

func (TaskShift) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	byTurn := map[string]map[string][]string{}
	for _, call := range s.ToolCalls {
		family := pathFamily(call.TargetPathDisplay)
		if call.TurnID == "" || family == "" {
			continue
		}
		if byTurn[call.TurnID] == nil {
			byTurn[call.TurnID] = map[string][]string{}
		}
		byTurn[call.TurnID][family] = append(byTurn[call.TurnID][family], call.ID)
	}
	var findings []domain.Finding
	previousFamily := ""
	previousTurn := ""
	for _, turn := range s.Turns {
		family, evidence := dominantFamily(byTurn[turn.ID])
		if family == "" {
			continue
		}
		if previousFamily != "" && previousFamily != family {
			findings = append(findings, domain.Finding{
				TurnID: turn.ID, FindingType: "possible_task_shift", Severity: "info", Confidence: domain.Possible,
				Title:    "Possible task shift",
				Summary:  fmt.Sprintf("Possible task shift: activity moved from %s/ to %s/.", previousFamily, family),
				Evidence: append([]string{previousTurn}, evidence...), Metric: map[string]any{"from": previousFamily, "to": family},
			})
		}
		previousFamily, previousTurn = family, turn.ID
	}
	return findings, nil
}

func pathFamily(value string) string {
	value = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(value)), "./")
	if value == "" || value == "." {
		return ""
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 || parts[0] == "src" || parts[0] == "app" || parts[0] == "internal" {
		return ""
	}
	return parts[0]
}

func dominantFamily(values map[string][]string) (string, []string) {
	best := ""
	var evidence []string
	for family, ids := range values {
		if len(ids) > len(evidence) || (len(ids) == len(evidence) && family < best) {
			best, evidence = family, ids
		}
	}
	return best, evidence
}
