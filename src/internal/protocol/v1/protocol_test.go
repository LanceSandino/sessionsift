package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefinitionsAreBoundedDomainOperations(t *testing.T) {
	want := []Operation{SessionList, SessionGet, SessionMetrics, SessionTimeline, SessionFindings, SessionCompare, TurnGet, ConnectionStatus, ConnectionDisconnect, AnnotationList, AnnotationCreate, ReviewList, ReviewCreate, AnalysisPatterns, AnalysisCorrections, ContextRecommendations, SupportRedeem}
	definitions := Definitions()
	if len(definitions) != len(want) {
		t.Fatalf("definitions=%d want=%d", len(definitions), len(want))
	}
	for _, operation := range want {
		definition, ok := DefinitionFor(operation)
		if !ok || definition.Capability == "" || definition.AuditCategory == "" || definition.MaximumResponseBytes <= 0 || definition.RedactionPolicy == "" || definition.Timeout <= 0 {
			t.Fatalf("incomplete definition for %q: %+v", operation, definition)
		}
		name := string(operation)
		for _, forbidden := range []string{"shell", "process", "filesystem", "sql", "command"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("unsafe generic operation registered: %s", operation)
			}
		}
	}
	if _, ok := DefinitionFor("shell.exec"); ok {
		t.Fatal("generic shell capability must fail closed")
	}
	sessionGet, _ := DefinitionFor(SessionGet)
	if sessionGet.MaximumResponseBytes < 16<<20 {
		t.Fatalf("session.get limit=%d; long sessions need at least 16 MiB of bounded response headroom", sessionGet.MaximumResponseBytes)
	}
}

func TestRequestValidationFailsClosed(t *testing.T) {
	valid := Request{Version: Version, RequestID: "req_92d1", Operation: SessionList, Arguments: json.RawMessage(`{"include_internal":false}`)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []Request{
		{Version: 2, RequestID: "req", Operation: SessionList},
		{Version: Version, RequestID: "spaces are invalid", Operation: SessionList},
		{Version: Version, RequestID: "req", Operation: "filesystem.read"},
		{Version: Version, RequestID: "req", Operation: SessionList, Arguments: json.RawMessage(`{`)},
		{Version: Version, RequestID: "req", Operation: SessionList, Arguments: json.RawMessage(`"` + strings.Repeat("x", MaximumArgumentsBytes) + `"`)},
	}
	for _, request := range cases {
		if err := request.Validate(); err == nil {
			t.Fatalf("expected invalid request: %+v", request)
		}
	}
}

func TestInvitationOperationsAreCanonicalAndIsolated(t *testing.T) {
	view, ok := InvitationOperations("view")
	if !ok || len(view) != 5 {
		t.Fatalf("view operations=%v ok=%v", view, ok)
	}
	comment, ok := InvitationOperations("comment")
	if !ok || len(comment) != 7 || comment[len(comment)-1] != AnnotationCreate {
		t.Fatalf("comment operations=%v ok=%v", comment, ok)
	}
	review, ok := InvitationOperations("review")
	if !ok || len(review) != 9 || review[len(review)-1] != ReviewCreate {
		t.Fatalf("review operations=%v ok=%v", review, ok)
	}
	if _, ok = InvitationOperations("owner"); ok {
		t.Fatal("unknown invitation role was accepted")
	}
	view[0] = SessionList
	fresh, _ := InvitationOperations("view")
	if fresh[0] == SessionList {
		t.Fatal("caller mutated the canonical invitation operation set")
	}
}

func TestHostedDefaultOperationsExcludeDetailedSessionEvidence(t *testing.T) {
	defaultOperations := HostedDefaultOperations()
	allowed := map[Operation]bool{}
	for _, operation := range defaultOperations {
		allowed[operation] = true
	}
	if !allowed[SessionList] || !allowed[AnalysisPatterns] {
		t.Fatalf("default hosted profile lacks bounded discovery: %v", defaultOperations)
	}
	for _, detailed := range []Operation{SessionGet, SessionMetrics, SessionTimeline, SessionFindings, SessionCompare, TurnGet} {
		if allowed[detailed] {
			t.Fatalf("detailed operation %q escaped least-privilege default", detailed)
		}
	}
}
