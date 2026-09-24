package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateRejectsAmbiguousArgumentKeysAtAnyDepth(t *testing.T) {
	for _, arguments := range []string{
		`{"session_id":"a","SESSION_ID":"b"}`,
		`{"x":{"k":1,"K":2}}`,
		`{"x":[{"id":1},{"id":2,"ID":3}]}`,
	} {
		request := Request{Version: Version, RequestID: "r", Operation: SessionGet, Arguments: json.RawMessage(arguments)}
		if err := request.Validate(); err == nil {
			t.Fatalf("accepted ambiguous arguments %s", arguments)
		}
	}
	nested := strings.Repeat(`{"a":`, 70) + "1" + strings.Repeat("}", 70)
	if err := (Request{Version: Version, RequestID: "r", Operation: SessionGet, Arguments: json.RawMessage(nested)}).Validate(); err == nil {
		t.Fatal("accepted excessively nested arguments")
	}
	valid := Request{Version: Version, RequestID: "r", Operation: SessionGet, Arguments: json.RawMessage(`{"session_id":"a","nested":[{"id":1},{"id":2}]}`)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("rejected valid arguments: %v", err)
	}
}

func TestSessionScopedRequiresCanonicalSingleSession(t *testing.T) {
	cases := map[string]bool{
		`{"session_id":"s1"}`:                   true,
		`{"session_id":"s1","turn_id":"t1"}`:    true,
		`{"session_id":"s1","limit":5}`:         true,
		`{"session_id":"s2"}`:                   false,
		`{"SESSION_ID":"s1"}`:                   false,
		`{"Session_Id":"s1","session_id":"s1"}`: false,
		`{"session_id":"s1","a":"s1","b":"s2"}`: false,
		`{"session_id":"s1","A":"s1"}`:          false,
		`{"session_id":1}`:                      false,
		`[]`:                                    false,
		``:                                      false,
	}
	for arguments, want := range cases {
		if got := SessionScoped(json.RawMessage(arguments), "s1"); got != want {
			t.Errorf("SessionScoped(%s)=%v want %v", arguments, got, want)
		}
	}
	if SessionScoped(json.RawMessage(`{"session_id":""}`), "") {
		t.Fatal("empty scope accepted")
	}
}

func TestInternalOperationsAreNeverGrantable(t *testing.T) {
	if Grantable(SupportRedeem) || !Grantable(SessionGet) || Grantable("unknown") {
		t.Fatal("grantability mismatch")
	}
	for _, operation := range ReadOperations() {
		if !Grantable(operation) {
			t.Fatalf("read operation %s is not grantable", operation)
		}
	}
	for _, role := range []string{"view", "comment", "review"} {
		operations, _ := InvitationOperations(role)
		for _, operation := range operations {
			if !Grantable(operation) {
				t.Fatalf("role %s includes internal %s", role, operation)
			}
		}
	}
}
