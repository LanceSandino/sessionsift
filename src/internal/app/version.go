// Package app exposes process-level SessionSift metadata.
package app

import "github.com/LanceSandino/sessionsift/src/internal/release"

// Version returns the semantic version of this build.
func Version() string {
	return release.CurrentVersion()
}
