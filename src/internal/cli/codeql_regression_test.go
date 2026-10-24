package cli

import (
	"testing"

	bridgeapp "github.com/LanceSandino/sessionsift/src/internal/bridge"
)

// Operator sync limits come from the environment. The page size becomes an
// int, so it is parsed with a bound instead of narrowing an unchecked int64.
func TestSyncLimitsFromEnvBoundsPageSize(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"", 0},
		{"50", 50},
		{"0", 0},
		{"-5", 0},
		{"not-a-number", 0},
		{"1001", bridgeapp.MaxSyncPageSize},
		{"9223372036854775807", bridgeapp.MaxSyncPageSize},
		{"99999999999999999999", bridgeapp.MaxSyncPageSize},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("SESSIONSIFT_SYNC_MAX_PAGE_SIZE", tc.value)
			t.Setenv("SESSIONSIFT_SYNC_MAX_OBJECT_BYTES", "4096")
			limits := syncLimitsFromEnv()
			if limits.MaxPageSize != tc.want {
				t.Fatalf("MaxPageSize = %d, want %d", limits.MaxPageSize, tc.want)
			}
			if limits.MaxObjectBytes != 4096 {
				t.Fatalf("MaxObjectBytes = %d, want 4096", limits.MaxObjectBytes)
			}
		})
	}
}
