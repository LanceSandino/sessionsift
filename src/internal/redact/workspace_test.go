package redact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRewritesEverySpellingToTheDisplayName(t *testing.T) {
	scrub := Workspace(`C:\Dev\Clients\Acme\checkout`, "checkout")
	for input, want := range map[string]string{
		`C:\Dev\Clients\Acme\checkout\main.go`:                   `checkout\main.go`,
		`{"file_path":"C:\\Dev\\Clients\\Acme\\checkout\\a.go"}`: `{"file_path":"checkout\\a.go"}`,
		`cd c:/dev/clients/acme/checkout && go test`:             `cd checkout && go test`,
		`C:\DEV\CLIENTS\ACME\CHECKOUT`:                           `checkout`,
		`no workspace mention here`:                              `no workspace mention here`,
	} {
		if got := scrub(input); got != want {
			t.Fatalf("scrub(%q)=%q want %q", input, got, want)
		}
	}
	posix := Workspace("/srv/clients/acme/payments/", "")
	if got := posix("open /srv/clients/acme/payments/api.go"); got != "open payments/api.go" {
		t.Fatalf("posix=%q", got)
	}
}

func TestWorkspaceIgnoresRootsHomeAndShortPaths(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, cwd := range []string{"", "/", `C:\`, "~", "/ab", home} {
		if got := Workspace(cwd, "x")("keep /ab and C:\\ and ~/y"); got != "keep /ab and C:\\ and ~/y" {
			t.Fatalf("cwd %q changed text to %q", cwd, got)
		}
	}
	nested := filepath.Join(home, "code", "app")
	scrub := Workspace(nested, "app")
	if got := scrub(Text(nested+string(os.PathSeparator)+"main.go", 0)); !strings.HasPrefix(got, "app") || strings.Contains(got, filepath.Base(home)) {
		t.Fatalf("home-relative workspace=%q", got)
	}
}
