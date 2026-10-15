// @module MCPSetupFormatTests pins escaped-key conflicts and malformed input behavior.
package mcpsetup

import (
	"encoding/json"
	"testing"
)

func TestJSONPreservesLargeNumbersAndRejectsEscapedDuplicates(t *testing.T) {
	for _, body := range []string{`{"mcpServers":{},"\u006dcpServers":{}}`, `{"unrelated":{"one":1,"\u006fne":2}}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
		if _, _, e := jsonConfig([]byte(body)); e == nil {
			t.Fatalf("accepted duplicate/invalid UTF8 %q", body)
		}
	}
	root, _, e := jsonConfig([]byte(`{"unrelated":123456789012345678901234567890}`))
	if e != nil {
		t.Fatal(e)
	}
	if string(root["unrelated"]) != "123456789012345678901234567890" {
		t.Fatal("numeric precision lost")
	}
}
func FuzzExistingJSONEntryCannotBeOverwritten(f *testing.F) {
	f.Add(`{"command":"user-owned"}`)
	f.Add(`null`)
	f.Add(`{"url":"https://remote.invalid"}`)
	f.Add(`12345678901234567890`)
	f.Fuzz(func(t *testing.T, entry string) {
		if len(entry) > 4096 || !json.Valid([]byte(entry)) {
			return
		}
		body := []byte(`{"mcpServers":{"sessionsift":` + entry + `}}`)
		_, _, _, e := proposed(Options{Client: "claude-code", Command: "/usr/bin/true", Args: []string{"mcp"}}, body)
		if e == nil {
			t.Fatal("overwrote user-owned entry")
		}
	})
}
