// Package core implements the Dungeondraft bridge app.
//
// Topology: a wire-protocol TCP server owns :8787 (same JSON-lines protocol
// as the old in-DD listener, so clients are unchanged). The Dungeondraft mod
// connects to the server as an *agent*; the server relays user commands to the
// agent and replies back. The server itself answers `ping` and reports the
// live `dd_connected` state, so liveness never depends on Dungeondraft.
package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// PROTOCOL_VERSION is the wire protocol version, mirrored from the mod.
const PROTOCOL_VERSION = 17

// AgentTimeout is how long a silent agent socket lingers before the bridge
// reports the map as offline (agent heartbeats every ~5s).
const AgentTimeout = 20 * time.Second

// CommandTimeout bounds how long a forwarded user command may wait for the
// agent's reply (long-but-bounded; export_map takes a few seconds).
const CommandTimeout = 30 * time.Second

// offlineReply is the answer to any non-ping command while DD's agent is not
// connected — a clear, structured reason instead of a dead port.
func offlineReply() map[string]any {
	return map[string]any{
		"ok":    false,
		"error": "dungeondraft offline: bridge agent not connected",
	}
}

// Options tunes the bridge.
type Options struct {
	Verbose bool // log every request/response frame

	// AgentTimeout is how long a silent agent socket lingers before the
	// bridge reports the map as offline. The mod heartbeats every ~5s, so
	// 20s is a safe default. Tests shorten it.
	AgentTimeout time.Duration

	// CommandTimeout bounds how long a forwarded user command may wait for
	// the agent's reply. Zero means the package default (30s).
	CommandTimeout time.Duration
}

// Stats is the aggregated command accounting surfaced by the admin API.
type Stats struct {
	TotalRequests  int64
	CompletedOK    int64
	CompletedErr   int64
	Pings          int64
	OfflineRejects int64
	LastCmd        string
	LastCmdAt      string
	ByCmd          map[string]uint64
}

// AgentInfo describes the connected Dungeondraft mod.
type AgentInfo struct {
	Connected    bool
	ID           int64
	Protocol     int
	ConnectedAt  string
	LastSeen     string
	HeartbeatAge time.Duration
	Remote       string
}

// ConnInfo describes one live wire connection for /connections.
type ConnInfo struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"` // "agent" or "user"
	Remote    string    `json:"remote"`
	Connected time.Time `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`
	LastCmd   string    `json:"last_cmd,omitempty"`
}

// frame is one parsed request/response plus its unmodified wire bytes.
type frame struct {
	raw    []byte
	parsed map[string]any
}

// job is a user command queued for the serialized agent pipeline.
type job struct {
	req  frame
	conn *wireConn
}

// wireConn is a live client (agent or user).
type wireConn struct {
	id          int64
	conn        net.Conn
	role        string // "agent" | "user"
	repo        *Bridge
	rwmu        sync.Mutex
	out         *bufio.Writer
	respCh      chan frame // agent: responses to forwarded commands
	lastCmd     string
	lastCmdAt   time.Time
	connectedAt time.Time
	closed      bool
}

// Bridge is the wire-protocol server plus agent link.
type Bridge struct {
	opts       Options
	cmdTimeout time.Duration
	hbTimeout  time.Duration
	jobs       chan job
	startAt    time.Time

	mu         sync.Mutex
	conns      map[int64]*wireConn
	nextID     int64
	agent      *wireConn
	agentProto int
	agentAt    time.Time
	agentSeen  time.Time
	stats      Stats
}

// New builds a bridge.
func New(o Options) *Bridge {
	if o.AgentTimeout == 0 {
		o.AgentTimeout = AgentTimeout
	}
	if o.CommandTimeout == 0 {
		o.CommandTimeout = CommandTimeout
	}
	return &Bridge{
		opts:      o,
		cmdTimeout: o.CommandTimeout,
		hbTimeout:  o.AgentTimeout,
		jobs:      make(chan job, 64),
		conns:     map[int64]*wireConn{},
		startAt:   time.Now(),
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// ListenAndServe accepts wire connections until ctx is cancelled.
func (b *Bridge) ListenAndServe(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	return b.Serve(ctx, ln)
}

// Serve services an existing listener until ctx is cancelled.
func (b *Bridge) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	b.logf("wire endpoint listening on tcp://%s", ln.Addr())

	go b.agentPump(ctx)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("accept: %v", err)
			continue
		}
		go b.handleConn(ctx, nc)
	}
}

// agentPump serializes user commands onto the agent link and relays replies.
// It is the only writer of forwarded *requests*; the agent's own read loop is
// the only reader. Exactly one forwarded command is outstanding at a time.
func (b *Bridge) agentPump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case jb := <-b.jobs:
			if jb.conn.closed {
				continue // the user went away; nothing to answer
			}
			b.relay(ctx, jb)
		}
	}
}

// relay forwards one user command to the agent and streams the reply back.
func (b *Bridge) relay(ctx context.Context, jb job) {
	agent := b.agentLive()
	if agent == nil {
		b.respond(jb.conn, offlineReply())
		b.statsRejectOffline(jb.req)
		return
	}

	agent.rwmu.Lock()
	if b.opts.Verbose {
		b.logf("-> agent %s", strings.TrimSpace(string(jb.req.raw)))
	}
	_, werr := agent.conn.Write(append(append([]byte{}, jb.req.raw...), '\n'))
	agent.rwmu.Unlock()
	if werr != nil {
		b.dropAgent(agent, "socket write failed: "+werr.Error())
		b.respond(jb.conn, offlineReply())
		b.statsRejectOffline(jb.req)
		return
	}

	select {
	case resp := <-agent.respCh:
		if b.opts.Verbose {
			b.logf("<- agent %s", strings.TrimSpace(string(resp.raw)))
		}
		b.respondRaw(jb.conn, resp.raw)
		b.statsRecord(jb.req, resp.raw)
	case <-time.After(b.cmdTimeout):
		b.dropAgent(agent, "no reply within "+b.cmdTimeout.String())
		b.respond(jb.conn, map[string]any{
			"ok":    false,
			"error": "bridge timed out waiting for Dungeondraft",
		})
		b.statsRecord(jb.req, nil)
	case <-ctx.Done():
		return
	}
}

// ---------------------------------------------------------------------------
// Connections
// ---------------------------------------------------------------------------

// handleConn runs the per-connection frame router. Both roles share it: the
// role is fixed by the first frame (agent_hello => agent) and then routing
// differs (see routeFrame).
func (b *Bridge) handleConn(ctx context.Context, nc net.Conn) {
	wc := b.register(nc)
	defer b.unregister(wc)

	sc := bufio.NewScanner(nc)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	wc.out = bufio.NewWriter(nc)

	for sc.Scan() {
		line := strings.TrimSpace(string(sc.Bytes()))
		if line == "" {
			continue
		}
		fr, ok := parseFrame([]byte(line))
		if !ok {
			b.respond(wc, map[string]any{"ok": false, "error": "invalid JSON request"})
			continue
		}
		if cmd := fstring(fr.parsed, "cmd"); cmd != "" {
			wc.lastCmd = cmd
			wc.lastCmdAt = time.Now()
		}
		if stop := b.routeFrame(ctx, wc, fr); stop {
			break
		}
	}
	if err := sc.Err(); err != nil && !isClosedErr(err) {
		b.logf("conn %d (%s): %v", wc.id, wc.role, err)
	}
	if wc.role == "agent" && !wc.closed {
		b.dropAgent(wc, "socket closed")
	}
}

// routeFrame handles one frame according to the connection's role. Returns
// true to close the connection.
func (b *Bridge) routeFrame(ctx context.Context, wc *wireConn, fr frame) bool {
	// Agent registration: first frame on a fresh connection is agent_hello.
	if hello, isHello := fr.parsed["agent_hello"]; isHello {
		proto := PROTOCOL_VERSION
		if hm, ok := hello.(map[string]any); ok {
			if p := fint(hm, "protocol"); p != 0 {
				proto = p
			}
		}
		b.promoteAgent(wc, proto)
		b.respond(wc, map[string]any{"ok": true, "result": map[string]any{"agent": "registered", "protocol": proto}})
		return false
	}

	// A user connection that somehow sends agent_hello later is not an agent.
	if wc.role == "agent" {
		// Heartbeat ping from the mod — a request we answer app-side; the
		// agent ignores the reply. Anything else is a reply to a forwarded
		// user command, handed to the awaiting pump.
		if fstring(fr.parsed, "cmd") == "ping" {
			b.agentSeen = time.Now()
			b.respond(wc, map[string]any{"ok": true, "result": map[string]any{"pong": true}})
			return false
		}
		b.agentSeen = time.Now()
		select {
		case wc.respCh <- fr:
		default:
			// A response nobody is waiting for (protocol confusion) — treat
			// as a stuck agent rather than silently mis-routing frames.
			b.dropAgent(wc, "unexpected agent frame: "+strings.TrimSpace(string(fr.raw)))
			return true
		}
		return false
	}

	// User connection: ping is answered by the app itself so liveness never
	// depends on Dungeondraft.
	if fstring(fr.parsed, "cmd") == "ping" {
		b.mu.Lock()
		b.stats.Pings++
		connected := b.agentSeenLive()
		b.mu.Unlock()
		b.respond(wc, map[string]any{
			"ok": true,
			"result": map[string]any{
				"pong":         true,
				"protocol":     PROTOCOL_VERSION,
				"app":          "dungeondraft-bridge",
				"dd_connected": connected,
			},
		})
		return false
	}

	select {
	case b.jobs <- job{req: fr, conn: wc}:
	case <-ctx.Done():
		return true
	}
	return false
}

func (b *Bridge) register(nc net.Conn) *wireConn {
	b.mu.Lock()
	b.nextID++
	wc := &wireConn{
		id:          b.nextID,
		conn:        nc,
		role:        "user",
		repo:        b,
		respCh:      make(chan frame, 1),
		connectedAt: time.Now(),
	}
	b.conns[wc.id] = wc
	b.mu.Unlock()
	b.logf("connection %d accepted from %s", wc.id, nc.RemoteAddr())
	return wc
}

func (b *Bridge) unregister(wc *wireConn) {
	b.mu.Lock()
	delete(b.conns, wc.id)
	if b.agent == wc {
		b.agent = nil
	}
	wasOpen := !wc.closed
	wc.closed = true
	b.mu.Unlock()
	if wasOpen {
		b.logf("connection %d (%s) disconnected", wc.id, wc.role)
	}
}

// promoteAgent makes wc the agent, superseding any previous agent socket.
func (b *Bridge) promoteAgent(wc *wireConn, proto int) {
	b.mu.Lock()
	old := b.agent
	b.agent = wc
	wc.role = "agent"
	b.agentProto = proto
	now := time.Now()
	b.agentAt = now
	b.agentSeen = now
	if old != nil && old != wc {
		old.closed = true
		old.conn.Close()
		b.logf("agent connection %d superseded by %d", old.id, wc.id)
	}
	b.mu.Unlock()
	b.logf("agent connection %d registered (protocol v%d)", wc.id, proto)
}

func (b *Bridge) dropAgent(agent *wireConn, reason string) {
	b.mu.Lock()
	if b.agent == agent {
		b.agent = nil
	}
	agent.closed = true
	b.mu.Unlock()
	b.logf("agent connection %d lost: %s", agent.id, reason)
	agent.conn.Close()
}

// agentLive returns the agent wireConn if it is connected and still has a
// recent heartbeat; otherwise nil.
func (b *Bridge) agentLive() *wireConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agent == nil || time.Since(b.agentSeen) > b.hbTimeout {
		return nil
	}
	return b.agent
}

// agentSeenLive reports only whether DD is currently reachable (for ping).
func (b *Bridge) agentSeenLive() bool {
	return b.agent != nil && time.Since(b.agentSeen) <= b.hbTimeout
}

// ---------------------------------------------------------------------------
// Responses + stats
// ---------------------------------------------------------------------------

func (b *Bridge) respond(wc *wireConn, resp map[string]any) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	b.respondRaw(wc, raw)
}

func (b *Bridge) respondRaw(wc *wireConn, raw []byte) {
	wc.rwmu.Lock()
	defer wc.rwmu.Unlock()
	if wc.out != nil {
		wc.out.Write(raw)
		wc.out.WriteByte('\n')
		wc.out.Flush()
		return
	}
	wc.conn.Write(append(append([]byte{}, raw...), '\n'))
}

func (b *Bridge) statsRejectOffline(_ frame) {
	b.mu.Lock()
	b.stats.OfflineRejects++
	b.mu.Unlock()
}

// statsRecord tallies a completed forwarded command. line nil means timed out.
func (b *Bridge) statsRecord(req frame, line []byte) {
	b.mu.Lock()
	cmd := fstring(req.parsed, "cmd")
	b.stats.TotalRequests++
	if line != nil && !strings.Contains(string(line), `"ok":false`) {
		b.stats.CompletedOK++
	} else {
		b.stats.CompletedErr++
	}
	if b.stats.ByCmd == nil {
		b.stats.ByCmd = map[string]uint64{}
	}
	b.stats.ByCmd[cmd]++
	b.stats.LastCmd = cmd
	b.stats.LastCmdAt = time.Now().Format(time.RFC3339)
	b.mu.Unlock()
}

func (b *Bridge) logf(format string, args ...any) {
	log.Printf("[bridge] "+format, args...)
}

// ---------------------------------------------------------------------------
// Observability / admin API
// ---------------------------------------------------------------------------

// AgentInfo returns the current agent status.
func (b *Bridge) AgentInfo() AgentInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agent == nil {
		return AgentInfo{}
	}
	age := time.Since(b.agentSeen)
	return AgentInfo{
		Connected:    age <= b.hbTimeout,
		ID:           b.agent.id,
		Protocol:     b.agentProto,
		ConnectedAt:  b.agentAt.Format(time.RFC3339),
		LastSeen:     b.agentSeen.Format(time.RFC3339),
		HeartbeatAge: age,
		Remote:       b.agent.conn.RemoteAddr().String(),
	}
}

// Connections lists every live wire connection.
func (b *Bridge) Connections() []ConnInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ConnInfo, 0, len(b.conns))
	for id, wc := range b.conns {
		out = append(out, ConnInfo{
			ID:        id,
			Role:      wc.role,
			Remote:    wc.conn.RemoteAddr().String(),
			Connected: wc.connectedAt,
			LastSeen:  wc.lastCmdAt,
			LastCmd:   wc.lastCmd,
		})
	}
	return out
}

// Stats returns a copy of the running counters.
func (b *Bridge) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := b.stats
	cp.ByCmd = map[string]uint64{}
	for k, v := range b.stats.ByCmd {
		cp.ByCmd[k] = v
	}
	return cp
}

// UptimeSeconds returns seconds since the app started.
func (b *Bridge) UptimeSeconds() float64 {
	return time.Since(b.startAt).Seconds()
}

// QueueDepth returns how many user commands are currently queued waiting for
// the single agent link (see jobs/agentPump) — any client's commands funnel
// through here, this is what makes that visible from outside.
func (b *Bridge) QueueDepth() int {
	return len(b.jobs)
}

// ---------------------------------------------------------------------------
// wire helpers
// ---------------------------------------------------------------------------

func parseFrame(raw []byte) (frame, bool) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return frame{}, false
	}
	return frame{raw: append([]byte{}, raw...), parsed: m}, true
}

func fstring(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func fint(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func isClosedErr(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "EOF"))
}