//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bridge "dungeondraft-mcp/bridgeclient"
)

// Terraform state is only true if the map
// file matches it, so the mod saves a map-bound client's edits once they
// have been quiet for 3 s, and marks the map modified on every edit.
const (
	autosaveQuiet = 3 * time.Second
	autosaveWait  = 15 * time.Second
)

// boundClient talks to the bridge as Terraform does: every command carries
// the map it is for.
func boundClient(mapFile string) *bridge.Client {
	c := bridge.New(bridgeHost, bridgePort)
	c.MapFile = mapFile
	return c
}

func addLight(t *testing.T, c *bridge.Client, x float64) int {
	t.Helper()
	res, err := c.Request(ctx(t), "add_light", map[string]any{"x": x, "y": 200})
	if err != nil {
		t.Fatalf("add_light: %v", err)
	}
	return int(res["id"].(float64))
}

// waitForAutosave waits for the mod's autosave to finish and returns it.
func waitForAutosave(t *testing.T) bridge.MapOp {
	t.Helper()
	deadline := time.Now().Add(autosaveWait)
	for time.Now().Before(deadline) {
		status, err := dd.Status(ctx(t))
		if err != nil {
			t.Fatalf("get_status: %v", err)
		}
		if op := status.LastMapOp; op.Op == "autosave" && op.Done {
			return op
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no autosave finished within %s", autosaveWait)
	return bridge.MapOp{}
}

// waitForSaved waits until an autosave has finished and nothing is left
// unsaved: the last edit is on disk.
func waitForSaved(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(autosaveWait)
	var status bridge.MapStatus
	for time.Now().Before(deadline) {
		var err error
		if status, err = dd.Status(ctx(t)); err != nil {
			t.Fatalf("get_status: %v", err)
		}
		op := status.LastMapOp
		if op.Op == "autosave" && op.Done && op.Error != "" {
			t.Fatalf("autosave failed: %s", op.Error)
		}
		if op.Op == "autosave" && op.Done && !status.Modified {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("within %s no autosave left the map saved (last op %+v, modified %v)", autosaveWait,
		status.LastMapOp, status.Modified)
}

func assertElementsInFile(t *testing.T, path string, ids []int) {
	t.Helper()
	runMapOp(t, "open_map", path, func(c context.Context) error { return dd.OpenMap(c, path) })
	for _, id := range ids {
		if _, err := dd.Request(ctx(t), "get_element", map[string]any{"id": id}); err != nil {
			t.Errorf("element %d is not in %s after reopening: %v", id, path, err)
		}
	}
}

func TestBoundEditMarksMapModified(t *testing.T) {
	path := scratchMap(t)
	addLight(t, boundClient(path), 100)
	status, err := dd.Status(ctx(t))
	if err != nil {
		t.Fatalf("get_status: %v", err)
	}
	if !status.Modified {
		t.Fatal("after an edit get_status reports modified=false; Dungeondraft would not warn before closing")
	}
}

func TestUnboundEditMarksModifiedButIsNotSaved(t *testing.T) {
	path := scratchMap(t)
	before := modTime(t, path)
	addLight(t, dd, 100)
	if status, _ := dd.Status(ctx(t)); !status.Modified {
		t.Error("after an unbound edit get_status reports modified=false")
	}
	time.Sleep(autosaveQuiet + 3*time.Second)
	if after := modTime(t, path); !after.Equal(before) {
		t.Fatalf("the map file changed (%s -> %s) after an edit from a client not bound to it", before, after)
	}
}

func TestBoundEditsAreSavedOnceQuiet(t *testing.T) {
	path := scratchMap(t)
	c := boundClient(path)
	ids := []int{addLight(t, c, 100), addLight(t, c, 300), addLight(t, c, 500)}
	waitForSaved(t)
	assertElementsInFile(t, path, ids)
}

const autosaveTerraformConfig = `
terraform {
  required_providers {
    dungeondraft = { source = "registry.terraform.io/jamesloftis/dungeondraft" }
  }
}
variable "map_file" { type = string }
provider "dungeondraft" {
  map_file = var.map_file
  level    = "Ground"
}
resource "dungeondraft_text" "note" {
  x    = 400
  y    = 400
  text = "saved without asking"
}
`

func TestTerraformApplyIsOnDiskWithoutASave(t *testing.T) {
	tf := newTerraformWith(t, map[string]string{"main.tf": autosaveTerraformConfig})
	tf.mapFile = scratchMap(t)
	tf.run(t, "init", "-input=false")
	tf.run(t, "apply", "-auto-approve", "-input=false")
	waitForSaved(t)
	runMapOp(t, "open_map", tf.mapFile, func(c context.Context) error { return dd.OpenMap(c, tf.mapFile) })
	if code, out := tf.output(t, "plan", "-detailed-exitcode", "-input=false"); code != 0 {
		t.Fatalf("plan after reopening the autosaved map exited %d; want 0 (no changes):\n%s", code, lastLines(out, 12))
	}
}

func TestOpenMapSavesPendingEditsFirst(t *testing.T) {
	other := scratchMap(t)
	path := scratchMap(t)
	id := addLight(t, boundClient(path), 100)
	runMapOp(t, "open_map", other, func(c context.Context) error { return dd.OpenMap(c, other) })
	assertElementsInFile(t, path, []int{id})
}

// Commands that arrive while the autosave writes wait for it instead of
// failing as busy, and are saved by the next autosave.
func TestCommandDuringAutosaveWaits(t *testing.T) {
	path := scratchMap(t)
	c := boundClient(path)
	ids := []int{addLight(t, c, 100)}
	time.Sleep(autosaveQuiet)
	for i := 0; i < 20; i++ {
		ids = append(ids, addLight(t, c, float64(150+20*i)))
		time.Sleep(50 * time.Millisecond)
	}
	waitForSaved(t)
	assertElementsInFile(t, path, ids)
}

// A folder sitting at the map's path makes Dungeondraft's write fail, which
// it only logs (see TestSaveMapReportsFailedWrite).
func TestFailedAutosaveIsReported(t *testing.T) {
	path := scratchMap(t)
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the scratch file: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("creating blocking folder: %v", err)
	}
	addLight(t, boundClient(path), 100)
	op := waitForAutosave(t)
	if op.Error == "" {
		t.Fatalf("autosave to %s reported success; a folder is in the way", path)
	}
	if !strings.EqualFold(filepath.Clean(op.Path), filepath.Clean(path)) {
		t.Errorf("failed autosave names %q, want %q", op.Path, path)
	}
	t.Logf("autosave failed as expected: %s", op.Error)
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}
