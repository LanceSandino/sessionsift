package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefinitionsAreBoundedDomainOperations(t *testing.T) {
	want := []Operation{SessionList, SessionGet, SessionMetrics, SessionTimeline, SessionFindings, SessionCompare, TurnGet, ConnectionStatus, ConnectionDisconnect, AnnotationList, AnnotationCreate, ReviewList, ReviewCreate}
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
