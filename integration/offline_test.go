//go:build integration

package integration

import (
	"net"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRefreshWithDungeondraftOfflineKeepsState reproduces spec finding 1: a
// refresh through a bridge_app with no Dungeondraft attached must fail and
// leave state alone, not report every element gone.
func TestRefreshWithDungeondraftOfflineKeepsState(t *testing.T) {
	tf := newTerraform(t)
	tf.mapFile = scratchMap(t)
	tf.run(t, "init", "-input=false")
	tf.run(t, "apply", "-auto-approve", "-input=false")
	t.Cleanup(func() { tf.run(t, "destroy", "-auto-approve", "-input=false") })
	before := tf.stateList(t)

	// The target reads no dungeondraft_assets data source: a failing data
	// source aborts the whole run before state is written, which would hide
	// what an element's own Read does with an offline answer.
	port := startAgentlessBridge(t)
	code, out := tf.output(t, "apply", "-refresh-only", "-auto-approve", "-input=false",
		"-target=dungeondraft_text.text",
		"-var", "bridge_port="+strconv.Itoa(port))

	if code == 0 || !strings.Contains(out, "dungeondraft offline") {
		t.Errorf("refresh with Dungeondraft offline exited %d; want a failure naming \"dungeondraft offline\"", code)
	}
	if after := tf.stateList(t); !reflect.DeepEqual(after, before) {
		t.Errorf("state changed while Dungeondraft was offline:\nbefore %v\nafter  %v", before, after)
	}
}

func (tf terraform) stateList(t *testing.T) []string {
	t.Helper()
	code, out := tf.output(t, "state", "list")
	if code != 0 {
		t.Fatalf("terraform state list exited %d", code)
	}
	return strings.Fields(out)
}

// startAgentlessBridge runs this tree's bridge_app on free ports; no
// Dungeondraft ever connects to it. Returns its wire-protocol port.
func startAgentlessBridge(t *testing.T) int {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "bridge_app.exe")
	build := exec.Command("go", "build", "-o", binary, "./cmd/bridge_app")
	build.Dir = filepath.Join("..", "bridge_app")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building bridge_app: %v\n%s", err, out)
	}
	port, admin := freePort(t), freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	cmd := exec.Command(binary, "-addr", addr, "-admin", "127.0.0.1:"+strconv.Itoa(admin), "-watch-dd=false")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting bridge_app: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitForListener(t, addr)
	return port
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", addr); err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("bridge_app never listened on %s", addr)
}
