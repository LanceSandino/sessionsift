// Package analyze runs deterministic, explainable rules over normalized sessions.
package analyze

import (
	"context"
	"fmt"
	"time"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

// Analyzer is the stable extension point for deterministic finding rules.
type Analyzer interface {
	Name() string
	Version() string
	Analyze(context.Context, domain.Session) ([]domain.Finding, error)
}

// Default returns the MVP rule set in deterministic order.
func Default() []Analyzer {
	return []Analyzer{RepeatedReads{}, RepeatedCommands{}, FailedLoops{}, LargeResults{}, ScopeCorrections{}, TaskShift{}, CompactionRediscovery{}, ExplorationRatio{}}
}

// Run executes all analyzers and assigns stable IDs where rules omitted them.
func Run(ctx context.Context, session domain.Session, analyzers []Analyzer) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, analyzer := range analyzers {
		items, err := analyzer.Analyze(ctx, session)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", analyzer.Name(), err)
		}
		for index := range items {
			items[index].SessionID = session.ID
			items[index].RuleVersion = analyzer.Version()
			if items[index].CreatedAt.IsZero() {
				items[index].CreatedAt = time.Now().UTC()
			}
			if items[index].ID == "" {
				items[index].ID = fmt.Sprintf("%s-%s-%d", session.ID, analyzer.Name(), len(findings)+index+1)
			}
		}
		findings = append(findings, items...)
	}
	return findings, nil
}
