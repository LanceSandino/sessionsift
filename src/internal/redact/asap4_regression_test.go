package redact

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ASAP4-F4: transcripts carry home paths JSON-escaped, with forward slashes,
// in different letter case, and for other accounts. None may reveal a username.
func TestHomeAndAccountPathsAreRedactedInEveryEncoding(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	user := filepath.Base(home)
	inputs := []string{
		strings.ReplaceAll(home, `\`, `\\`) + `\\project\\main.go`,
		strings.ReplaceAll(home, `\`, `/`) + `/project/main.go`,
		strings.ToUpper(home) + string(os.PathSeparator) + "project",
		`/Users/alice/secret-project/main.go`,
		`/home/bob/code/app`,
		`C:\Users\carol\Documents\plan.md`,
		`C:\\Users\\dave\\AppData\\Local`,
		`c:/users/erin/src`,
	}
	for _, input := range inputs {
		got := Text(input, 0)
		for _, name := range []string{user, "alice", "bob", "carol", "dave", "erin"} {
			segment := regexp.MustCompile(`(?i)[\\/]` + regexp.QuoteMeta(name) + `([\\/]|$)`)
			if segment.MatchString(got) {
				t.Fatalf("Text(%q)=%q retained account %q", input, got, name)
			}
		}
	}
}

// ASAP4-F4b: common provider credentials outside the original pattern set.
func TestAdditionalProviderCredentialsAreRedacted(t *testing.T) {
	secrets := []string{
		"sk-ant-" + "api03-abcdefghijklmnopqrstuvwxyz0123456789",
		"sk_live_" + "abcdefghijklmnopqrstuvwx", // gitleaks:allow -- synthetic redaction fixture
		"rk_live_" + "abcdefghijklmnopqrstuvwx", // gitleaks:allow -- synthetic redaction fixture
		"xoxb-" + "123456789012-abcdefghijklmnop",
		"AIzaSy" + "A-abcdefghijklmnopqrstuvwxyz01234",
		"glpat-" + "abcdefghijklmnopqrst",               // gitleaks:allow -- synthetic redaction fixture
		"npm_" + "abcdefghijklmnopqrstuvwxyz0123456789", // gitleaks:allow -- synthetic redaction fixture
		"hf_" + "abcdefghijklmnopqrstuvwxyzABCDEF",
		"github_pat_" + "11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz",
	}
	for _, secret := range secrets {
		if got := Text("value "+secret+" end", 0); strings.Contains(got, secret) {
			t.Fatalf("credential retained: %q", got)
		}
	}
	for _, assignment := range []string{"aws_secret_access_key=wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY", "SECRET_KEY: hunter2hunter2", "private_token = abcdef123456", "DB_PASSWORD=correcthorse"} { // gitleaks:allow -- synthetic redaction fixtures
		got := Text(assignment, 0)
		if !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("named secret retained: %q", got)
		}
	}
}

// ASAP4-F6: shared path identities are keyed per installation.
func TestPathHashesAreKeyedPerInstallation(t *testing.T) {
	path := "/Users/alice/project"
	plain := Hash(path)
	SetPathKey(nil)
	unkeyed := PathHash(path)
	SetPathKey([]byte("installation-a-secret-key-000000"))
	first := PathHash(path)
	SetPathKey([]byte("installation-b-secret-key-000000"))
	second := PathHash(path)
	SetPathKey(nil)
	if unkeyed == plain || first == plain || first == unkeyed || first == second || len(first) != 64 {
		t.Fatalf("plain=%s unkeyed=%s first=%s second=%s", plain, unkeyed, first, second)
	}
}

func TestNamedCredentialsKeepJSONQuoting(t *testing.T) {
	got := Text(`{"password":"hunter2","token": 'abc', "api_key":12345}`, 0)
	if strings.Contains(got, "hunter2") || strings.Contains(got, "abc'") || strings.Contains(got, "12345") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, `"password":"[REDACTED]"`) || !strings.Contains(got, `'[REDACTED]'`) {
		t.Fatalf("quoting lost: %q", got)
	}
}

func TestTitleIsSingleLineRedactedAndRuneSafe(t *testing.T) {
	got := Title("  Fix\n the  ghp_abcdefghijklmnopqrstuvwxyz  bug é"+strings.Repeat("é", 80), 40)
	if strings.Contains(got, "ghp_") || strings.Contains(got, "\n") || len(got) > 40+len("…") {
		t.Fatalf("title=%q", got)
	}
	if !strings.HasSuffix(got, "…") || !utf8Valid(got) {
		t.Fatalf("title=%q", got)
	}
}

func utf8Valid(value string) bool { return strings.ToValidUTF8(value, "\uFFFD") == value }

// The account placeholder must survive Markdown rendering, where a backslash
// before punctuation (such as "<") is consumed as an escape.
func TestAccountPlaceholderIsMarkdownSafe(t *testing.T) {
	for input, want := range map[string]string{
		`C:\Users\carol\plan.md`:   `C:\Users\redacted-user\plan.md`,
		`/Users/alice/project`:     `/Users/redacted-user/project`,
		`"/home/bob/.ssh/config"`:  `"/home/redacted-user/.ssh/config"`,
		`C:\\Users\\dave\\AppData`: `C:\\Users\\redacted-user\\AppData`,
	} {
		if got := Paths(input); got != want {
			t.Fatalf("Paths(%q)=%q want %q", input, got, want)
		}
	}
}

// Large inputs are scanned only to maxBytes plus a margin; credentials cut by
// that scan boundary must still be redacted rather than partially retained.
func TestBoundedScanRedactsCredentialsCutAtTheMargin(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("MIIEpAIBAAKCAQEA0vx7agoebGcQSuuPiLJXZpt", 2000) + "\n-----END RSA PRIVATE KEY-----" // gitleaks:allow -- synthetic redaction fixture
	got := Text("key follows "+pem+strings.Repeat(" tail", 10), 64)
	if strings.Contains(got, "MIIEpAIBAAKCAQEA") {
		t.Fatalf("cut-off private key retained: %q", got)
	}
	jwt := "eyJhbGciOiJIUzI1NiJ9." + strings.Repeat("eyJzdWIiOiJ1c2VyQGV4YW1wbGUuY29tIn0", 1000) + ".c2lnbmF0dXJl"
	if got = Text("auth "+jwt, 64); strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Fatalf("cut-off JWT retained: %q", got)
	}
	huge := strings.Repeat("x", 100_000) + " ghp_abcdefghijklmnopqrstuvwxyz"
	if got = Text(huge, 50); len(got) > 50+len("…") {
		t.Fatalf("bound exceeded: %d", len(got))
	}
	if got = Text("prefix ghp_abcdefghijklmnopqrstuvwxyz "+strings.Repeat("y", 100_000), 64); strings.Contains(got, "ghp_") {
		t.Fatalf("credential inside kept output retained: %q", got)
	}
}
