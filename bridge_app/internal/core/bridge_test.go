package core

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// startBridge runs a bridge on an ephemeral port and returns its address.
func startBridge(t *testing.T, o Options) (string, *Bridge, context.CancelFunc) {
	t.Helper()
	if o.AgentTimeout == 0 {
		o.AgentTimeout = 2 * time.Second
	}
	if o.CommandTimeout == 0 {
		o.CommandTimeout = 2 * time.Second
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := New(o)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = b.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		ln.Close()
	})
	return ln.Addr().String(), b, cancel
}

// client is a test socket with a buffered reader.
type client struct {
	conn   net.Conn
	reader *bufio.Reader
}

// dial opens a client socket to the bridge address.
func dial(t *testing.T, addr string) *client {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return &client{conn: c, reader: bufio.NewReader(c)}
}

// send writes one JSON request line.
func (cl *client) send(v any) {
	b, _ := json.Marshal(v)
	cl.conn.Write(append(b, '\n'))
}

// recv waits for one response line and returns the parsed object.
func (cl *client) recv(t *testing.T) map[string]any {
	t.Helper()
	cl.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := cl.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("bad json %q: %v", line, err)
	}
	return m
}

// helloAgent opens a connection and registers as an agent.
func helloAgent(t *testing.T, addr string) *client {
	t.Helper()
	cl := dial(t, addr)
	cl.send(map[string]any{"agent_hello": map[string]any{"name": "fake-mod", "protocol": 17}})
	if m := cl.recv(t); !okTrue(m) {
		t.Fatalf("agent_hello rejected: %v", m)
	}
	return cl
}

// fakeAgent replies to every forwarded command with a single canned result.
func fakeAgent(t *testing.T, addr string, result map[string]any) *client {
	t.Helper()
	return fakeAgentFunc(t, addr, func(req map[string]any) map[string]any {
		return map[string]any{"ok": true, "result": result}
	})
}

// fakeAgentFunc replies to each forwarded command using a caller-supplied
// function of the decoded request, so a test can vary the response per
// call (e.g. fail once, then succeed) rather than always returning the
// same canned result.
func fakeAgentFunc(t *testing.T, addr string, reply func(req map[string]any) map[string]any) *client {
	t.Helper()
	cl := helloAgent(t, addr)
	go func() {
		for {
			line, err := cl.reader.ReadString('\n')
			if err != nil {
				return
			}
			var req map[string]any
			if json.Unmarshal([]byte(line), &req) != nil {
				continue
			}
			if _, isHello := req["agent_hello"]; isHello {
				continue
			}
			b, _ := json.Marshal(reply(req))
			cl.conn.Write(append(b, '\n'))
		}
	}()
	return cl
}

// drain consumes reads (e.g. registration ack + heartbeats) so they never
// block; an agent that replies to nothing in tests of timeout/expiry.
func drain(r *bufio.Reader) {
	for {
		if _, err := r.ReadString('\n'); err != nil {
			return
		}
	}
}

func okTrue(m map[string]any) bool {
	ok, _ := m["ok"].(bool)
	return ok
}

func resultOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, _ := m["result"].(map[string]any)
	if r == nil {
		t.Fatalf("no result in %v", m)
	}
	return r
}

func errStr(m map[string]any) string {
	s, _ := m["error"].(string)
	return s
}

// ---------------------------------------------------------------------------

func TestPingNoAgent(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "ping"})
	m := cl.recv(t)
	if !okTrue(m) {
		t.Fatalf("ping not ok: %v", m)
	}
	res := resultOf(t, m)
	if p, _ := res["pong"].(bool); !p {
		t.Fatalf("no pong: %v", m)
	}
	if proto := res["protocol"].(float64); int(proto) != PROTOCOL_VERSION {
		t.Fatalf("wrong protocol: %v", m)
	}
	if connected, _ := res["dd_connected"].(bool); connected {
		t.Fatalf("should be dd_connected=false with no agent")
	}
}

func TestAgentRegistersThenPingConnected(t *testing.T) {
	addr, b, _ := startBridge(t, Options{})
	ac := helloAgent(t, addr)
	defer ac.conn.Close()
	go drain(ac.reader)

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "ping"})
	m := cl.recv(t)
	if connected, _ := resultOf(t, m)["dd_connected"].(bool); !connected {
		t.Fatalf("expected dd_connected=true after agent registered: %v", m)
	}
	if ai := b.AgentInfo(); !ai.Connected {
		t.Fatalf("AgentInfo.Connected=false: %+v", ai)
	}
}

func TestForwardReplyRelayedVerbatim(t *testing.T) {
	addr, b, _ := startBridge(t, Options{})
	result := map[string]any{"map_open": true, "map_center": []any{float64(1024), float64(1024)}}
	ac := fakeAgent(t, addr, result)
	defer ac.conn.Close()

	c1 := dial(t, addr)
	c2 := dial(t, addr)
	defer c1.conn.Close()
	defer c2.conn.Close()

	// Two users, interleaved -- the pump serializes them onto the agent.
	c1.send(map[string]any{"cmd": "get_status"})
	c2.send(map[string]any{"cmd": "get_assets"})
	m := c1.recv(t)
	res := resultOf(t, m)
	if !okTrue(m) {
		t.Fatalf("forward failed: %v", m)
	}
	if mo, _ := res["map_open"].(bool); !mo {
		t.Fatalf("reply not relayed: %v", m)
	}
	_ = c2.recv(t)

	st := b.Stats()
	if st.TotalRequests != 2 || st.CompletedOK != 2 {
		t.Fatalf("stats wrong: %+v", st)
	}
	if st.ByCmd["get_status"] != 1 || st.ByCmd["get_assets"] != 1 {
		t.Fatalf("by-cmd wrong: %+v", st.ByCmd)
	}
}

func TestOfflineRejection(t *testing.T) {
	addr, b, _ := startBridge(t, Options{})
	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_map"})
	m := cl.recv(t)
	if okTrue(m) {
		t.Fatalf("expected offline error, got %v", m)
	}
	if !strings.Contains(errStr(m), "dungeondraft offline") {
		t.Fatalf("wrong offline error: %q", errStr(m))
	}
	if st := b.Stats(); st.OfflineRejects != 1 {
		t.Fatalf("offline rejects: %+v", st)
	}
}

func TestAgentReplace(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	a1 := helloAgent(t, addr)
	defer a1.conn.Close()
	go drain(a1.reader)

	a2 := helloAgent(t, addr)
	defer a2.conn.Close()
	go drain(a2.reader)

	// The old agent socket must have been closed by the superseding hello.
	a1.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := a1.reader.ReadByte(); err == nil {
		t.Fatalf("old agent socket still open after replacement")
	}

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "ping"})
	m := cl.recv(t)
	if connected, _ := resultOf(t, m)["dd_connected"].(bool); !connected {
		t.Fatalf("expected connected after replacement: %v", m)
	}
}

func TestHeartbeatExpiry(t *testing.T) {
	addr, b, _ := startBridge(t, Options{AgentTimeout: 120 * time.Millisecond})
	ac := helloAgent(t, addr)
	defer ac.conn.Close()
	go drain(ac.reader)

	// Registered, then silent past the heartbeat window.
	time.Sleep(200 * time.Millisecond)

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "ping"})
	m := cl.recv(t)
	if connected, _ := resultOf(t, m)["dd_connected"].(bool); connected {
		t.Fatalf("expected dd_connected=false after expiry: %v", m)
	}

	cl.send(map[string]any{"cmd": "get_status"})
	m = cl.recv(t)
	if !strings.Contains(errStr(m), "dungeondraft offline") {
		t.Fatalf("expected offline rejection: %v", m)
	}
	if ai := b.AgentInfo(); ai.Connected {
		t.Fatalf("AgentInfo still connected: %+v", ai)
	}
}

func TestCommandTimeout(t *testing.T) {
	addr, _, _ := startBridge(t, Options{CommandTimeout: 60 * time.Millisecond})
	ac := helloAgent(t, addr)
	defer ac.conn.Close()
	go drain(ac.reader) // never replies to forwarded commands

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_status"})
	m := cl.recv(t)
	if !strings.Contains(errStr(m), "timed out") {
		t.Fatalf("expected timeout error, got: %v", m)
	}
}

func TestInvalidJSON(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.conn.Write([]byte("not json\n"))
	m := cl.recv(t)
	if okTrue(m) || errStr(m) != "invalid JSON request" {
		t.Fatalf("expected invalid JSON error, got: %v", m)
	}
}

func TestConnectionsAndAdminSnapshot(t *testing.T) {
	addr, b, _ := startBridge(t, Options{})
	ac := helloAgent(t, addr)
	defer ac.conn.Close()
	go drain(ac.reader)

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "ping"})
	cl.recv(t)

	conns := b.Connections()
	var sawAgent, sawUser bool
	for _, cn := range conns {
		if cn.Role == "agent" {
			sawAgent = true
		}
		if cn.Role == "user" {
			sawUser = true
		}
	}
	if !sawAgent || !sawUser {
		t.Fatalf("connection roles missing: %+v", conns)
	}
	if b.UptimeSeconds() <= 0 {
		t.Fatalf("uptime not positive")
	}
}

// TestTerrainSlot4to7ErrorRelayedThenFixWorks is a regression test for the
// splatImage2-is-null bug (mcp_bridge.gd's _open_splat / _init_splat2):
// confirmed live, every paint_terrain call on slots 4-7 failed identically
// with "bridge rejected paint_terrain: could not read splat image for slot
// N" because Dungeondraft never allocated the second splat image for that
// level. bridge_app itself has no slot-specific logic -- it's a dumb
// relay -- so this only pins down that (a) the exact error string reaches
// the client byte-for-byte, not swallowed or rewritten into something
// generic, and (b) nothing on this path is stateful in a way that would
// keep failing calls to the SAME slot after the mod-side fix starts
// succeeding (e.g. a bad "first response wins" cache). It cannot exercise
// the actual fix -- that's GDScript, tested separately in
// mod_tests/unit/test_open_splat.gd against
// a fake Terrain, since this repo's Go side never sees a real Dungeondraft
// engine either way.
func TestTerrainSlot4to7ErrorRelayedThenFixWorks(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})

	var calls int
	ac := fakeAgentFunc(t, addr, func(req map[string]any) map[string]any {
		if req["cmd"] != "paint_terrain" {
			return map[string]any{"ok": true, "result": map[string]any{}}
		}
		calls++
		if calls == 1 {
			// Pre-fix behavior: every slot 4-7 call fails identically.
			return map[string]any{"ok": false, "error": "bridge rejected paint_terrain: could not read splat image for slot 5"}
		}
		// Post-fix behavior: same slot, now succeeds (simulating a mod
		// reload picking up the _init_splat2 fix between these two calls).
		return map[string]any{"ok": true, "result": map[string]any{"painted_slot": float64(5), "pixels": float64(42)}}
	})
	defer ac.conn.Close()

	cl := dial(t, addr)
	defer cl.conn.Close()

	cl.send(map[string]any{"cmd": "paint_terrain", "slot": 5, "x": 100, "y": 100, "radius": 50})
	first := cl.recv(t)
	if okTrue(first) {
		t.Fatalf("expected the first call to fail (pre-fix simulation), got: %v", first)
	}
	wantErr := "bridge rejected paint_terrain: could not read splat image for slot 5"
	if got := errStr(first); got != wantErr {
		t.Fatalf("error not relayed verbatim: got %q, want %q", got, wantErr)
	}

	cl.send(map[string]any{"cmd": "paint_terrain", "slot": 5, "x": 100, "y": 100, "radius": 50})
	second := cl.recv(t)
	if !okTrue(second) {
		t.Fatalf("expected the second call to succeed (post-fix simulation), got: %v", second)
	}
	res := resultOf(t, second)
	if slot, _ := res["painted_slot"].(float64); slot != 5 {
		t.Fatalf("result not relayed correctly: %v", second)
	}
}
