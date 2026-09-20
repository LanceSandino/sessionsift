// @module AudienceTests ensures invitation tokens cannot act as account-wide tokens.
package accessauth

import "testing"

func TestPrimaryAudienceIsDistinctFromInvitationAudience(t *testing.T) {
	verifier, err := NewJWTVerifier("https://access.example", "main,invite", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		audiences []string
		allowed   bool
	}{{[]string{"main"}, true}, {[]string{"invite"}, false}, {nil, false}, {[]string{"other", "main"}, true}} {
		if got := verifier.IsPrimaryAudience(Principal{Audiences: test.audiences}); got != test.allowed {
			t.Fatal("audience boundary failed")
		}
	}
}
