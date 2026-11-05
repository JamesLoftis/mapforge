//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

// bridge_app shared dd-launch's console, so closing the window
// dd-launch ran in shut bridge_app down. Each child must get no console and
// its own process group, so console close and Ctrl+C events never reach it.
func TestLaunchCommandDetachesFromTheConsole(t *testing.T) {
	cmd := launchCommand(`C:\Windows\System32\cmd.exe`, nil)
	if cmd.SysProcAttr == nil {
		t.Fatal("launchCommand set no SysProcAttr; the child would share dd-launch's console")
	}
	want := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	if got := cmd.SysProcAttr.CreationFlags & want; got != want {
		t.Fatalf("CreationFlags = %#x; want DETACHED_PROCESS and CREATE_NEW_PROCESS_GROUP (%#x) set",
			cmd.SysProcAttr.CreationFlags, want)
	}
}
