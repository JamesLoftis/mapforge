//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveMapIntoMissingFolderFails(t *testing.T) {
	scratchMap(t)
	path := filepath.Join(t.TempDir(), "no such folder", scratchMapName)
	assertSaveFails(t, path)
}

// A folder sitting at the map's path makes Dungeondraft's final rename fail
// after the save has started, which it only logs.
func TestSaveMapReportsFailedWrite(t *testing.T) {
	scratchMap(t)
	path := filepath.Join(t.TempDir(), scratchMapName)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("creating blocking folder: %v", err)
	}
	assertSaveFails(t, path)
}

func assertSaveFails(t *testing.T, path string) {
	t.Helper()
	c := ctx(t)
	err := dd.SaveMap(c, path)
	if err == nil {
		err = dd.WaitMapOp(c, "save_map", path, mapOpTimeout)
	}
	if err == nil {
		t.Fatalf("save_map %q reported success; nothing can have been written there", path)
	}
	t.Logf("save_map failed as expected: %v", err)
}
