// ScopeCorrections identifies possible corrections while explicitly preserving uncertainty.
package analyze

import (
	"context"
	"regexp"
	"strings"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

var correctionPattern = regexp.MustCompile(`(?i)\b(i meant|only|do not|don't|not that|leave .+ alone|instead|no[,;:]?)\b`)

type ScopeCorrections struct{}

func (ScopeCorrections) Name() string    { return "possible-scope-correction" }
func (ScopeCorrections) Version() string { return "1.0.0" }
func (ScopeCorrections) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, turn := range s.Turns {
		if turn.Sequence <= 1 || !correctionPattern.MatchString(strings.TrimSpace(turn.UserPromptRedacted)) {
			continue
		}
		prior := 0
		evidence := []string{turn.ID}
		for _, call := range s.ToolCalls {
			if call.Timestamp.Before(turn.StartedAt) {
				prior++
				evidence = append(evidence, call.ID)
			}
		}
		if prior < 2 {
			continue
		}
		findings = append(findings, domain.Finding{TurnID: turn.ID, FindingType: "possible_scope_correction", Severity: "info", Confidence: domain.Possible, Title: "Possible scope correction", Summary: "Possible scope correction detected after exploratory tool activity.", Evidence: evidence, Metric: map[string]any{"prior_tool_calls": prior}})
	}
	return findings, nil
}
