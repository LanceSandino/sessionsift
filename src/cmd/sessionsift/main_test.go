package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDelegatesToCLI(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "2.0.1") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}
