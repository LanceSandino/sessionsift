package transcript

import (
	"strings"
	"testing"
	"time"
)

// ASAP5-F4: the shortened tool-target display must be derived from redacted
// text so a home-directory account name never survives the truncation.
func TestToolTargetDisplayNeverRetainsHomeAccountName(t *testing.T) {
	for _, target := range []string{"/Users/dummy-owner/plan.txt", "/home/dummy-owner/plan.txt", `C:\\Users\\dummy-owner\\plan.txt`} {
		call := newToolCall("c1", "s1", "t1", time.Now(), "read_file", `{"path":"`+target+`"}`)
		if call.TargetPathDisplay == "" || strings.Contains(call.TargetPathDisplay, "dummy-owner") {
			t.Errorf("target %q displayed as %q", target, call.TargetPathDisplay)
		}
	}
}
