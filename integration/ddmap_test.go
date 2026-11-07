//go:build integration

package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ddmap is how other tools (Campaign Creator) get a blank map to build on and
// a picture of what they built.
func TestDdmapNewThenExport(t *testing.T) {
	ddmap := buildDdmap(t)
	dir := t.TempDir()
	mapFile := filepath.Join(dir, "m.dungeondraft_map")

	runDdmap(t, ddmap, "new", "-w", "20", "-h", "15", "-out", mapFile)
	if _, err := os.Stat(mapFile); err != nil {
		t.Fatalf("ddmap new did not write %s: %v", mapFile, err)
	}
	status, err := dd.Request(ctx(t), "get_status", nil)
	if err != nil {
		t.Fatalf("get_status: %v", err)
	}
	size := status["map_size_woxels"].([]any)
	if size[0].(float64) != 20*256 || size[1].(float64) != 15*256 {
		t.Errorf("map size = %v; want [5120 3840]", size)
	}

	render := filepath.Join(dir, "r.png")
	runDdmap(t, ddmap, "export", "-ppi", "8", "-out", render)
	png, err := os.ReadFile(render)
	if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Errorf("ddmap export did not write a PNG to %s (err %v, %d bytes)", render, err, len(png))
	}
}

func TestDdmapRefusesMissingArguments(t *testing.T) {
	ddmap := buildDdmap(t)
	out, err := exec.Command(ddmap, "new", "-w", "20").CombinedOutput()
	if err == nil {
		t.Fatalf("ddmap new without -h and -out succeeded:\n%s", out)
	}
	if !bytes.Contains(out, []byte("-out")) {
		t.Errorf("ddmap's refusal does not name the missing -out:\n%s", out)
	}
}

func buildDdmap(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "ddmap.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ddmap")
	build.Dir = filepath.Join("..", "provider")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building ddmap: %v\n%s", err, out)
	}
	return binary
}

func runDdmap(t *testing.T, ddmap string, args ...string) {
	t.Helper()
	if out, err := exec.Command(ddmap, args...).CombinedOutput(); err != nil {
		t.Fatalf("ddmap %v: %v\n%s", args, err, out)
	}
}
