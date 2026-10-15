package v1

import (
	"encoding/json"
	"testing"
)

// ASAP5-F1: Go's JSON decoder folds Unicode look-alike keys (U+017F long s,
// U+212A Kelvin sign) onto ASCII field names, so the scope check must treat
// them as the same key instead of ignoring them.
func TestSessionScopedRejectsUnicodeFoldedSessionKeys(t *testing.T) {
	cases := []string{
		`{"session_id":"s1","\u017fession_id":"s2"}`,
		`{"\u017fession_id":"s2","session_id":"s1"}`,
		`{"\u017fession_id":"s1"}`,
		`{"session_id":"s1","\u017fESSION_ID":"s2"}`,
	}
	for _, arguments := range cases {
		if SessionScoped(json.RawMessage(arguments), "s1") {
			var decoded struct {
				SessionID string `json:"session_id"`
			}
			_ = json.Unmarshal([]byte(arguments), &decoded)
			t.Errorf("SessionScoped(%s)=true while the decoder selects %q", arguments, decoded.SessionID)
		}
	}
	if err := rejectAmbiguousKeys(json.RawMessage(`{"limit":1,"\u212aey":"x","key":"y"}`)); err == nil {
		t.Fatal("Kelvin-sign key was not treated as a repeat of its ASCII spelling")
	}
}
