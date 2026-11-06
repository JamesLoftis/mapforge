//go:build integration

// Package integration drives a real, running Dungeondraft through bridge_app.
// Run with: go test -tags integration ./integration/
// It replaces the open map, so it refuses to start on a map that could hold
// someone's work (see checkPreconditions).
package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	bridge "mapforge/bridgeclient"
)

const (
	bridgeHost       = "127.0.0.1"
	bridgePort       = 8787
	mapOpTimeout     = 60 * time.Second
	requestTimeout   = 2 * time.Minute
	scratchMapTiles  = 20
	scratchMapFolder = "scratch map" // contains a space on purpose
	scratchMapName   = "scratch.dungeondraft_map"
)

var dd *bridge.Client

func TestMain(m *testing.M) {
	dd = bridge.New(bridgeHost, bridgePort)
	if err := checkPreconditions(readPreconditions(context.Background())); err != nil {
		fmt.Fprintln(os.Stderr, "integration preconditions failed:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// bridgePing is what the harness needs from bridge_app's own ping reply.
type bridgePing struct {
	Err           error
	DDConnected   bool
	Protocol      int // bridge_app's
	AgentProtocol int // the connected mod's
}

func readPreconditions(ctx context.Context) (bridgePing, bridge.MapStatus) {
	res, err := dd.Request(ctx, "ping", nil)
	ping := bridgePing{
		Err:           err,
		DDConnected:   res["dd_connected"] == true,
		Protocol:      intField(res, "protocol"),
		AgentProtocol: intField(res, "agent_protocol"),
	}
	if !ping.isReady() || ping.isStaleMod() {
		return ping, bridge.MapStatus{} // a stale mod refuses get_status
	}
	status, err := dd.Status(ctx)
	if err != nil {
		ping.Err = err
	}
	return ping, status
}

func (p bridgePing) isReady() bool { return p.Err == nil && p.DDConnected }

func (p bridgePing) isStaleMod() bool { return p.AgentProtocol != p.Protocol }

func intField(m map[string]any, key string) int {
	n, _ := m[key].(float64)
	return int(n)
}

func checkPreconditions(ping bridgePing, status bridge.MapStatus) error {
	switch {
	case ping.Err != nil:
		return fmt.Errorf("bridge_app is not running on %s:%d — start Dungeondraft via dd-launch (%v)",
			bridgeHost, bridgePort, ping.Err)
	case !ping.DDConnected:
		return errors.New("Dungeondraft is not connected to bridge_app — is the TF Bridge Agent mod enabled?")
	case ping.isStaleMod():
		return fmt.Errorf("Dungeondraft's mod speaks protocol v%d, bridge_app v%d; restart Dungeondraft so it "+
			"loads the current mod", ping.AgentProtocol, ping.Protocol)
	case !status.MapOpen:
		return nil
	case status.MapFile == "" && status.Modified:
		return errors.New("an unsaved map with unsaved changes is open; save or discard it first — " +
			"the harness replaces the open map")
	case status.MapFile == "" && totalElements(status) > 0:
		return fmt.Errorf("an unsaved map with content is open (%s); save or discard it first — "+
			"the harness replaces the open map", describeCounts(status))
	case status.MapFile != "" && !isUnderTempDir(status.MapFile):
		return fmt.Errorf("a non-scratch map is open (%s); close or save it first — "+
			"the harness replaces the open map", status.MapFile)
	}
	return nil
}

func isUnderTempDir(path string) bool {
	rel, err := filepath.Rel(strings.ToLower(os.TempDir()), strings.ToLower(filepath.Clean(path)))
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func totalElements(status bridge.MapStatus) int {
	total := 0
	for _, n := range status.Counts {
		total += n
	}
	return total
}

func describeCounts(status bridge.MapStatus) string {
	kinds := make([]string, 0, len(status.Counts))
	for kind, n := range status.Counts {
		kinds = append(kinds, fmt.Sprintf("%s=%d", kind, n))
	}
	sort.Strings(kinds)
	return strings.Join(kinds, " ")
}

// ctx bounds one test's bridge calls.
func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), requestTimeout)
	t.Cleanup(cancel)
	return c
}

// scratchMap opens a fresh 20x20-tile map saved under a per-test temp dir whose
// path contains a space; returns that .dungeondraft_map path.
func scratchMap(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), scratchMapFolder)
	// Runs before the temp dir is removed (cleanups are last-in, first-out):
	// an autosave into a removed folder would fail, and the mod would then
	// refuse to leave the map.
	t.Cleanup(func() { settleAutosave(t) })
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating scratch dir: %v", err)
	}
	path := filepath.Join(dir, scratchMapName)
	runMapOp(t, "new_map", "", func(c context.Context) error {
		return dd.NewMap(c, scratchMapTiles, scratchMapTiles)
	})
	runMapOp(t, "save_map", path, func(c context.Context) error { return dd.SaveMap(c, path) })
	return path
}

// settleAutosave waits until no autosave is pending or writing.
func settleAutosave(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(mapOpTimeout)
	for time.Now().Before(deadline) {
		status, err := dd.Status(ctx(t))
		if err != nil || !status.AutosavePending {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Errorf("an autosave was still pending after %s", mapOpTimeout)
}

// saveAndReopen saves the open map to path, reopens it, and waits for both;
// fails the test (with element-kind counts from get_status) on any timeout.
func saveAndReopen(t *testing.T, path string) {
	t.Helper()
	runMapOp(t, "save_map", path, func(c context.Context) error { return dd.SaveMap(c, path) })
	runMapOp(t, "open_map", path, func(c context.Context) error { return dd.OpenMap(c, path) })
}

func runMapOp(t *testing.T, op, path string, start func(context.Context) error) {
	t.Helper()
	c := ctx(t)
	began := time.Now()
	err := start(c)
	if err == nil {
		err = dd.WaitMapOp(c, op, path, mapOpTimeout)
	}
	t.Logf("%s took %s", op, time.Since(began).Round(time.Millisecond))
	if err != nil {
		status, _ := dd.Status(c)
		t.Fatal(mapOpFailure(op, path, err, status))
	}
}

func mapOpFailure(op, path string, err error, status bridge.MapStatus) string {
	return fmt.Sprintf("%s \"%s\" failed: %v (map holds: %s)", op, path, err, describeCounts(status))
}
