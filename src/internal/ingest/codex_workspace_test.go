// @module CodexWorkspaceTests pins bounded header reads and workspace privacy.
package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexWorkspaceReadsOnlyBoundedHeader(t *testing.T) {
	for _, tc := range []struct {
		name, body, cwd string
		wantErr         bool
	}{
		{"header", `{"type":"session_meta","payload":{"cwd":"/private/repo"}}` + "\n" + strings.Repeat("x", 256<<10), "/private/repo", false},
		{"no cwd", `{"type":"session_meta","payload":{}}`, "", false},
		{"invalid json", "{", "", true},
		{"wrong record", `{"type":"response_item"}`, "", true},
		{"oversize", strings.Repeat("x", (128<<10)+1), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.jsonl")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			cwd, err := codexWorkspace(path)
			if (err != nil) != tc.wantErr || cwd != tc.cwd {
				t.Fatalf("cwd=%q err=%v", cwd, err)
			}
		})
	}
	if _, err := codexWorkspace(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing header accepted")
	}
}
