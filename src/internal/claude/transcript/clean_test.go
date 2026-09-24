package transcript

import (
	"strings"
	"testing"
)

func TestInjectedContextIsStrippedAndSlashCommandsRendered(t *testing.T) {
	session := parseRecords(t, false,
		with(userRecord(1, "meta", "Injected skill body"), fields{"isMeta": true}),
		userRecord(2, "p1", "<system-reminder>Private reminder</system-reminder>\n<ide_opened_file>The user opened a file</ide_opened_file>Explain this function"),
		with(userRecord(3, "p1-summary", "This session is being continued from a previous conversation"), fields{"isCompactSummary": true}),
		userRecord(4, "p2", "<command-message>review</command-message>\n<command-name>/review</command-name>\n<command-args>PR 12</command-args>"),
		userRecord(5, "p2", "<local-command-stdout>Private output</local-command-stdout>"),
		userRecord(6, "p3", "<command-name>clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"),
		userRecord(7, "p4", []any{textBlock("<local-command-caveat>Caveat text</local-command-caveat>"), textBlock("<ide_selection>selected code</ide_selection>Summarize the selection")}),
		userRecord(8, "p5", "<local-command-stderr>Private error</local-command-stderr>   "),
	)
	want := []string{"Explain this function", "/review PR 12", "/clear", "Summarize the selection"}
	if len(session.Turns) != len(want) || len(session.Events) != len(want) {
		t.Fatalf("turns=%+v events=%+v", session.Turns, session.Events)
	}
	for index, prompt := range want {
		if session.Turns[index].UserPromptRedacted != prompt || session.Events[index].EventType != "prompt" {
			t.Fatalf("turn %d=%q event=%+v", index, session.Turns[index].UserPromptRedacted, session.Events[index])
		}
	}
	encoded := marshal(t, session)
	for _, private := range []string{"Private reminder", "Private output", "Private error", "Injected skill body", "continued from a previous", "selected code", "user opened", "Caveat text"} {
		if strings.Contains(encoded, private) {
			t.Fatalf("injected context %q retained", private)
		}
	}
	if got := cleanPrompt("<command-name>/x</command-name>\n<command-args>y</command-args>\nextra words"); got != "/x y\nextra words" {
		t.Fatalf("cleanPrompt=%q", got)
	}
}
