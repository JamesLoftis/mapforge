//go:build windows

// Package ddwatch answers one question: is a given executable currently
// running? Used to tie bridge_app's lifecycle to Dungeondraft's — it has no
// business staying up for hours after DD itself is gone (which is exactly
// what happened before this existed).
package ddwatch

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsRunning reports whether any running process's image name matches exeName
// (case-insensitive, e.g. "Dungeondraft.exe"). Uses a toolhelp snapshot
// directly rather than shelling out to tasklist.exe (avoids parsing its
// localized/columnar text output) or an unmaintained process-listing library.
func IsRunning(exeName string) (bool, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	err = windows.Process32First(snapshot, &entry)
	for err == nil {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, exeName) {
			return true, nil
		}
		err = windows.Process32Next(snapshot, &entry)
	}
	return false, nil
}
