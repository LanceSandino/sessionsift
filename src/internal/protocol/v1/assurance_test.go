// @module ProtocolAssuranceFuzz bounds parser and case-collision authorization inputs (A05/A09).
package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzA05Arguments(f *testing.F) {
	f.Add(`{"session_id":"allowed","SESSION_ID":"other"}`)
	f.Add(`{"nested":{"a":1,"A":2}}`)
	f.Add(`{"session_id":"allowed"}`)
	f.Add("") // Arguments is explicitly optional in the protocol contract.
	seed := `{"session_id":"allowed"}`
	f.Add(strings.Repeat(" ", MaximumArgumentsBytes+1-len(seed)) + seed)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > MaximumArgumentsBytes+1 {
			return
		}
		request := Request{Version: Version, RequestID: "fuzz", Operation: SessionGet, Arguments: json.RawMessage(raw)}
		err := request.Validate()
		if ((len(raw) > 0 && !json.Valid([]byte(raw))) || len(raw) > MaximumArgumentsBytes) && err == nil {
			t.Fatal("invalid input accepted")
		}
		if err != nil && SessionScoped(request.Arguments, "allowed") {
			t.Fatal("invalid arguments passed scope")
		}
	})
}
