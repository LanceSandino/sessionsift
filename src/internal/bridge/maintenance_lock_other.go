//go:build !linux && !darwin

// @module UnsupportedBridgeMaintenance refuses unsafe maintenance on unqualified platforms.
package bridge

import (
	"errors"
	"os"
)

func acquireMaintenanceLock(string) (*os.File, error) {
	return nil, errors.New("bridge maintenance requires Linux or macOS")
}
func privateDirectory(string) error { return errors.New("bridge maintenance requires Linux or macOS") }
func readPrivate(string, int64) ([]byte, error) {
	return nil, errors.New("bridge maintenance requires Linux or macOS")
}
func openPrivateFile(string) (*os.File, error) {
	return nil, errors.New("bridge maintenance requires Linux or macOS")
}
