package app

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != "0.1.0-dev" {
		t.Fatalf("Version() = %q, want %q", got, "0.1.0-dev")
	}
}
