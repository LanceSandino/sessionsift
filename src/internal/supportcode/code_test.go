package supportcode

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenerateProducesReadableUniqueCodes(t *testing.T) {
	shape := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)
	seen := map[string]bool{}
	for index := 0; index < 500; index++ {
		code, err := Generate()
		if err != nil || !shape.MatchString(code) || seen[code] {
			t.Fatalf("code=%q err=%v duplicate=%v", code, err, seen[code])
		}
		seen[code] = true
	}
}

func TestNormalizeAcceptsHumanVariantsAndRejectsInvalid(t *testing.T) {
	want := "0123ABCD1111"
	for _, input := range []string{"0123-ABCD-1111", "o123 abcd ilIL", " 0123abcd-1111 "} {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Fatalf("Normalize(%q)=%q err=%v", input, got, err)
		}
	}
	for _, input := range []string{"", "0123-ABCD-111", "0123-ABCD-11111", "0123-ABCD-111U", "0123-ABCD-11!1", strings.Repeat("A", 40)} {
		if _, err := Normalize(input); err == nil {
			t.Fatalf("Normalize(%q) accepted", input)
		}
	}
}

func TestHashIsPurposeSeparatedAndMatchesConstantTime(t *testing.T) {
	code, _ := Generate()
	support, err := Hash(Support, code)
	if err != nil || !ValidHash(support) {
		t.Fatalf("hash=%q err=%v", support, err)
	}
	link, _ := Hash(Link, code)
	if support == link {
		t.Fatal("support and link hashes collide")
	}
	if !Matches(Support, strings.ToLower(code), support) || Matches(Link, code, support) || Matches(Support, "0000-0000-0000", support) || Matches(Support, "bad", support) || Matches(Support, code, "short") {
		t.Fatal("match semantics wrong")
	}
	if ValidHash("zz") || ValidHash(strings.Repeat("g", 64)) {
		t.Fatal("invalid hash accepted")
	}
	if _, err = Hash(Support, "nope"); err == nil {
		t.Fatal("invalid code hashed")
	}
}
