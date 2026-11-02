//go:build !windows

package ddwatch

import "errors"

// IsRunning is Windows-only (Dungeondraft's install/launch conventions this
// watcher relies on are Windows-specific here); callers should gate use of
// this package behind runtime.GOOS or a flag on other platforms.
func IsRunning(exeName string) (bool, error) {
	return false, errors.New("ddwatch: process watching is only implemented on windows")
}
