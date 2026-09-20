package toolmeta

import "testing"

func TestDescribeRecognizesMCPAndApps(t *testing.T) {
	tests := []struct {
		name, transport, server string
	}{
		{"mcp__codex_app__list_threads", "mcp", "codex_app"},
		{"mcp.canva.create_design", "mcp", "canva"},
		{"connector.google_drive.search", "app", "google_drive"},
		{"exec", "builtin", ""},
		{"mcp__incomplete", "mcp", ""},
		{"mcp.incomplete", "mcp", ""},
		{"app.slack", "app", "slack"},
	}
	for _, test := range tests {
		transport, server := Describe(test.name)
		if transport != test.transport || server != test.server {
			t.Errorf("Describe(%q)=(%q,%q), want (%q,%q)", test.name, transport, server, test.transport, test.server)
		}
	}
}
