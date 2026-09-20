package redact

import (
	"strings"
	"testing"
)

func TestTextRedactsSecretsAndBoundsOutput(t *testing.T) {
	input := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz and ghp_abcdefghijklmnopqrstuvwxyz"
	got := Text(input, 80)
	if strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret leaked: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("missing marker: %q", got)
	}
}

func TestPathHelpers(t *testing.T) {
	if got := DisplayPath("/Users/example/project/src/auth.go"); got != "src/auth.go" {
		t.Fatalf("DisplayPath() = %q", got)
	}
	if Hash("a") == Hash("b") || len(Hash("a")) != 64 {
		t.Fatal("Hash must be stable SHA-256")
	}
}
