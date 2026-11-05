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
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PROTOCOL_VERSION is the wire protocol version, mirrored from the mod.
const PROTOCOL_VERSION = 20

// AgentTimeout is how long a silent agent socket lingers before the bridge
// reports the map as offline (agent heartbeats every ~5s).
const AgentTimeout = 20 * time.Second

// CommandTimeout bounds how long a forwarded user command may wait for the
// agent's reply (long-but-bounded; export_map takes a few seconds).
const CommandTimeout = 30 * time.Second

// Error codes bridge_app itself answers with (PROTOCOL.md, Errors).
const (
	codeOffline          = "offline"
	codeTimeout          = "timeout"
	codeAgentLost        = "agent_lost"
	codeProtocolMismatch = "protocol_mismatch"
)

// WriteTimeout bounds one reply write to a user, so a client that stops
// reading cannot stall the single agent pump.
const WriteTimeout = 5 * time.Second

// errorReply is a failed answer carrying a machine-readable code.
func errorReply(code, message string) map[string]any {
	return map[string]any{"ok": false, "error": message, "code": code}
}

// offlineReply is the answer to any non-ping command while DD's agent is not
// connected — a clear, structured reason instead of a dead port.
func offlineReply() map[string]any {
	return errorReply(codeOffline, "dungeondraft offline: bridge agent not connected")
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

	// WriteTimeout bounds one reply write. Zero means the package default.
	WriteTimeout time.Duration

	// CommandLog receives one JSON line per command event (PROTOCOL.md,
	// Command log). Nil means no command log.
	CommandLog io.Writer
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
	req     frame
	conn    *wireConn
	request *pendingRequest // nil when the client sent no req_id
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
	closed      atomic.Bool
	gone        chan struct{} // closed once the connection is finished
	goneOnce    sync.Once
}

// markClosed records that wc is finished and wakes anything waiting on it.
// Safe to call more than once; reports whether wc was still open.
func (wc *wireConn) markClosed() bool {
	wasOpen := !wc.closed.Swap(true)
	wc.goneOnce.Do(func() { close(wc.gone) })
	return wasOpen
}

// Bridge is the wire-protocol server plus agent link.
type Bridge struct {
	opts         Options
	cmdTimeout   time.Duration
	hbTimeout    time.Duration
	writeTimeout time.Duration
	jobs         chan job
	requests     *requestTable
	commands     *commandLog
	startAt      time.Time

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
	if o.WriteTimeout == 0 {
		o.WriteTimeout = WriteTimeout
	}
	b := &Bridge{
		opts:         o,
		cmdTimeout:   o.CommandTimeout,
		hbTimeout:    o.AgentTimeout,
		writeTimeout: o.WriteTimeout,
		jobs:         make(chan job, 64),
		requests:     newRequestTable(),
		conns:        map[int64]*wireConn{},
		startAt:      time.Now(),
	}
	b.commands = newCommandLog(o.CommandLog, b.logf)
	return b
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
			if jb.request == nil && jb.conn.closed.Load() {
				continue // the user went away; nothing to answer
			}
			// With a req_id the job runs even if its user left: that user's
			// retry is waiting on this request's reply.
			b.relay(ctx, jb)
		}
	}
}

// relay forwards one user command to the agent and streams the reply back.
func (b *Bridge) relay(ctx context.Context, jb job) {
	agent, proto := b.agentLive()
	if agent == nil {
		b.answerUnsent(jb, offlineReply())
		b.statsRejectOffline(jb.req)
		return
	}
	if proto != PROTOCOL_VERSION {
		b.answerUnsent(jb, errorReply(codeProtocolMismatch, fmt.Sprintf(
			"Dungeondraft's mod speaks protocol v%d, bridge_app v%d; restart Dungeondraft so it loads the current mod",
			proto, PROTOCOL_VERSION)))
		b.statsRecord(jb.req, nil)
		return
	}

	began := time.Now()
	b.commands.record(entryFor(eventSent, jb).withRequest(jb))
	agent.rwmu.Lock()
	if b.opts.Verbose {
		b.logf("-> agent %s", strings.TrimSpace(string(jb.req.raw)))
	}
	_, werr := agent.conn.Write(append(append([]byte{}, jb.req.raw...), '\n'))
	agent.rwmu.Unlock()
	if werr != nil {
		b.dropAgent(agent, "socket write failed: "+werr.Error())
		b.answerUnsent(jb, offlineReply())
		b.statsRejectOffline(jb.req)
		return
	}
	b.awaitReply(ctx, jb, agent, began)
}

// awaitReply relays the agent's reply to jb, skipping any reply that names
// another request (a late answer to one that already timed out).
func (b *Bridge) awaitReply(ctx context.Context, jb job, agent *wireConn, began time.Time) {
	timeout := time.NewTimer(b.cmdTimeout)
	defer timeout.Stop()
	cmd := fstring(jb.req.parsed, "cmd")
	for {
		select {
		case resp := <-agent.respCh:
			if b.opts.Verbose {
				b.logf("<- agent %s", strings.TrimSpace(string(resp.raw)))
			}
			if !repliesTo(resp, jb) {
				b.logf("discarding agent reply for another request: %s", strings.TrimSpace(string(resp.raw)))
				continue
			}
			b.finish(jb, resp.raw, began)
			b.statsRecord(jb.req, resp.raw)
		case <-timeout.C:
			b.dropAgent(agent, "no reply within "+b.cmdTimeout.String())
			b.finish(jb, marshalReply(errorReply(codeTimeout, fmt.Sprintf(
				"bridge timed out after %s waiting for Dungeondraft to answer %s", b.cmdTimeout, cmd))), began)
			b.statsRecord(jb.req, nil)
		case <-agent.gone:
			b.finish(jb, marshalReply(errorReply(codeAgentLost, fmt.Sprintf(
				"Dungeondraft's bridge agent went away while running %s; it may or may not have run", cmd))), began)
			b.statsRecord(jb.req, nil)
		case <-ctx.Done():
		}
		return
	}
}

// finish logs the outcome of a command Dungeondraft received and answers it.
func (b *Bridge) finish(jb job, reply []byte, began time.Time) {
	b.commands.record(entryFor(eventDone, jb).withOutcome(reply, began))
	b.answerRaw(jb, reply)
}

func marshalReply(resp map[string]any) []byte {
	raw, _ := json.Marshal(resp) // string-keyed maps of strings and bools always marshal
	return raw
}

// repliesTo reports whether resp answers jb. A reply without req_id (or a
// request without one) cannot be told apart and is accepted.
func repliesTo(resp frame, jb job) bool {
	got := fstring(resp.parsed, "req_id")
	return jb.request == nil || got == "" || got == jb.request.id
}

// answerRaw replies to jb's user and, for a request Dungeondraft received,
// keeps the reply for any retry of the same req_id.
func (b *Bridge) answerRaw(jb job, raw []byte) {
	if jb.request != nil {
		b.requests.finish(jb.request, raw, time.Now())
	}
	b.respondRaw(jb.conn, raw)
}

// answerUnsent replies for a request that never reached Dungeondraft, so a
// retry of it runs rather than replaying this answer.
func (b *Bridge) answerUnsent(jb job, resp map[string]any) {
	refused := entryFor(eventRefused, jb).withRequest(jb)
	refused.Code, refused.Error = fstring(resp, "code"), fstring(resp, "error")
	b.commands.record(refused)
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if jb.request != nil {
		b.requests.forget(jb.request, raw)
	}
	b.respondRaw(jb.conn, raw)
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
			b.noteCommand(wc, cmd)
		}
		if stop := b.routeFrame(ctx, wc, fr); stop {
			break
		}
	}
	if err := sc.Err(); err != nil && !isClosedErr(err) {
		b.logf("conn %d (%s): %v", wc.id, wc.role, err)
	}
	if wc.role == "agent" && !wc.closed.Load() {
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
			b.markAgentSeen()
			b.respond(wc, map[string]any{"ok": true, "result": map[string]any{"pong": true}})
			return false
		}
		b.markAgentSeen()
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
		agentProto := 0
		if connected {
			agentProto = b.agentProto
		}
		b.mu.Unlock()
		b.respond(wc, map[string]any{
			"ok": true,
			"result": map[string]any{
				"pong":           true,
				"protocol":       PROTOCOL_VERSION,
				"app":            "dungeondraft-bridge",
				"dd_connected":   connected,
				"agent_protocol": agentProto,
			},
		})
		return false
	}

	jb := job{req: fr, conn: wc}
	if id := fstring(fr.parsed, "req_id"); id != "" {
		request, isRepeat := b.requests.join(id, time.Now())
		if isRepeat {
			go b.replay(ctx, job{req: fr, conn: wc}, request)
			return false
		}
		jb.request = request
	}
	select {
	case b.jobs <- jb:
	case <-ctx.Done():
		return true
	}
	return false
}

// replay answers a repeated request with the original's reply once it has one.
func (b *Bridge) replay(ctx context.Context, repeat job, request *pendingRequest) {
	select {
	case <-request.done:
		b.commands.record(entryFor(eventReplayed, repeat))
		b.respondRaw(repeat.conn, request.reply)
	case <-ctx.Done():
	}
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
		gone:        make(chan struct{}),
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
	wasOpen := wc.markClosed()
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
		old.markClosed()
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
	agent.markClosed()
	b.mu.Unlock()
	b.logf("agent connection %d lost: %s", agent.id, reason)
	agent.conn.Close()
}

// agentLive returns the agent wireConn and its protocol if it is connected
// and still has a recent heartbeat; otherwise nil.
func (b *Bridge) agentLive() (*wireConn, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agent == nil || time.Since(b.agentSeen) > b.hbTimeout {
		return nil, 0
	}
	return b.agent, b.agentProto
}

// markAgentSeen records a frame from the agent (heartbeat or reply).
func (b *Bridge) markAgentSeen() {
	b.mu.Lock()
	b.agentSeen = time.Now()
	b.mu.Unlock()
}

// noteCommand records wc's latest command for /connections.
func (b *Bridge) noteCommand(wc *wireConn, cmd string) {
	b.mu.Lock()
	wc.lastCmd = cmd
	wc.lastCmdAt = time.Now()
	b.mu.Unlock()
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
	// The deadline covers this reply only; left set, it would also fail the
	// pump's later forwards on an agent socket.
	_ = wc.conn.SetWriteDeadline(time.Now().Add(b.writeTimeout))
	defer wc.conn.SetWriteDeadline(time.Time{})
	var err error
	if wc.out != nil {
		wc.out.Write(raw)
		wc.out.WriteByte('\n')
		err = wc.out.Flush()
	} else {
		_, err = wc.conn.Write(append(append([]byte{}, raw...), '\n'))
	}
	if err != nil {
		// A client that stopped reading: drop it rather than stall the pump.
		b.logf("connection %d (%s): reply write failed, closing: %v", wc.id, wc.role, err)
		wc.conn.Close()
	}
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
