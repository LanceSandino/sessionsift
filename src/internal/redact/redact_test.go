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

func TestTextRedactsCloudflareAccessAndNamedCredentials(t *testing.T) {
	clientID := "0123456789abcdef0123456789abcdef.access"
	serviceSecret := "cfast_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	clientSecret := "example-client-secret-value"
	input := strings.Join([]string{
		"CF-Access-Client-Id: " + clientID,
		"CF-Access-Client-Secret: " + serviceSecret,
		"client_secret=" + clientSecret,
		"input_tokens=42 token_source=otel",
	}, "\n")
	got := Text(input, 0)
	for _, secret := range []string{clientID, serviceSecret, clientSecret} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential leaked: %q", got)
		}
	}
	if strings.Count(got, "[REDACTED]") != 3 {
		t.Fatalf("redaction markers=%d text=%q", strings.Count(got, "[REDACTED]"), got)
	}
	if !strings.Contains(got, "input_tokens=42 token_source=otel") {
		t.Fatalf("non-secret token metadata changed: %q", got)
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
