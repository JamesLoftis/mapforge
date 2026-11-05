//go:build !windows

package main

import "os/exec"

// detachFromConsole does nothing here: dd-launch only ships for Windows.
func detachFromConsole(cmd *exec.Cmd) {}
