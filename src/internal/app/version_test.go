package app

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != "2.1.1" {
		t.Fatalf("Version() = %q, want %q", got, "2.1.1")
	}
}
