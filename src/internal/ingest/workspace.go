// @module IngestWorkspace removes the private absolute working directory from every shared field.
package ingest

import (
	"github.com/LanceSandino/sessionsift/src/internal/domain"
	"github.com/LanceSandino/sessionsift/src/internal/redact"
)

// scrubWorkspace rewrites the session's absolute working directory to its
// display name in all text that can be listed, shared, or relayed. The CWD
// itself stays local (it is never serialized) for hashing and grouping.
func scrubWorkspace(session *domain.Session) {
	scrub := redact.Workspace(session.CWD, session.RepoDisplayName)
	session.Title = scrub(session.Title)
	for index := range session.Warnings {
		session.Warnings[index] = scrub(session.Warnings[index])
	}
	for index := range session.Turns {
		turn := &session.Turns[index]
		turn.UserPromptRedacted = scrub(turn.UserPromptRedacted)
		turn.AssistantSummaryRedacted = scrub(turn.AssistantSummaryRedacted)
	}
	for index := range session.Events {
		session.Events[index].PayloadRedacted = scrubValue(session.Events[index].PayloadRedacted, scrub).(map[string]any)
	}
	for index := range session.ToolCalls {
		call := &session.ToolCalls[index]
		call.ArgumentsRedacted = scrub(call.ArgumentsRedacted)
		call.ResultPreviewRedacted = scrub(call.ResultPreviewRedacted)
		call.CommandSignature = scrub(call.CommandSignature)
		call.TargetPathDisplay = scrub(call.TargetPathDisplay)
	}
	for index := range session.Findings {
		finding := &session.Findings[index]
		finding.Title = scrub(finding.Title)
		finding.Summary = scrub(finding.Summary)
		for evidence := range finding.Evidence {
			finding.Evidence[evidence] = scrub(finding.Evidence[evidence])
		}
	}
}

func scrubValue(value any, scrub func(string) string) any {
	switch typed := value.(type) {
	case string:
		return scrub(typed)
	case map[string]any:
		if typed == nil {
			return typed
		}
		for key, item := range typed {
			typed[key] = scrubValue(item, scrub)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = scrubValue(item, scrub)
		}
		return typed
	default:
		return value
	}
}
