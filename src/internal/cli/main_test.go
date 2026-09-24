package cli

import (
	"os"
	"testing"
)

// TestMain isolates every CLI test from the developer's real Claude Code home
// so commands that import or watch transcripts never read private sessions.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "sessionsift-claude-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("CLAUDE_CONFIG_DIR", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
