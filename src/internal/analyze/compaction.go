// CompactionRediscovery finds file reads repeated across a compaction boundary.
package analyze

import (
	"context"
	"fmt"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

type CompactionRediscovery struct{}

func (CompactionRediscovery) Name() string    { return "compaction-rediscovery" }
func (CompactionRediscovery) Version() string { return "1.0.0" }
func (CompactionRediscovery) Analyze(_ context.Context, s domain.Session) ([]domain.Finding, error) {
	for _, boundary := range s.Events {
		if boundary.EventType != "compaction" {
			continue
		}
		before := map[string]bool{}
		var repeated []string
		for _, call := range s.ToolCalls {
			if call.ToolCategory != "read" || call.TargetPathHash == "" {
				continue
			}
			if call.Timestamp.Before(boundary.Timestamp) {
				before[call.TargetPathHash] = true
			} else if before[call.TargetPathHash] {
				repeated = append(repeated, call.ID)
			}
		}
		if len(repeated) > 0 {
			return []domain.Finding{{FindingType: "compaction_rediscovery", Severity: "info", Confidence: domain.Observed, Title: "Post-compaction rediscovery", Summary: fmt.Sprintf("After compaction, Codex reread %d files or file targets inspected earlier.", len(repeated)), Evidence: append([]string{boundary.ID}, repeated...), Metric: map[string]any{"repeated_reads": len(repeated)}}}, nil
		}
	}
	return nil, nil
}
