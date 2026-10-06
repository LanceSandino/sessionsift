package app

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != "2.0.0" {
		t.Fatalf("Version() = %q, want %q", got, "2.0.0")
	}
}
