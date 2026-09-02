// Package bridge is a Go client for the Dungeondraft MCP bridge mod's wire
// protocol (see PROTOCOL.md): one JSON object per newline-delimited frame,
// requests {"cmd": ..., params...}, responses {"ok": true, "result": ...} or
// {"ok": false, "error": "..."} on the same TCP connection.
//
// Vendored from the dungeondraft-mcp-fork repo's bridgeclient/ module (this
// provider now lives in its own repo, so it carries its own copy rather than
// a cross-repo relative path dependency). Keep in sync by hand if the wire
// protocol's client side changes there.
//
// Failure classification mirrors the protocol's transport/application split:
// transport problems (unreachable, reset mid-reply, closed without
// responding) are retried up to Attempts times on a fresh connection, while
// an application rejection from the bridge itself ({"ok": false}, e.g. "no
// map open") surfaces as an Error and is never retried. There is no
// persistent-connection/keepalive watchdog here (earlier Python client had
// one): bridge_app now owns liveness itself and answers immediately with a
// clear "dungeondraft offline" rejection instead of a hung or dead socket, so
// a dial-per-request client no longer needs to guess about staleness.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Error is raised when the bridge itself rejects a request (app-level error, never retried).
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// Unreachable wraps a transport-level failure that prevented talking to the bridge.
type Unreachable struct {
	Err error
}

func (e *Unreachable) Error() string {
	return fmt.Sprintf("could not reach the Dungeondraft bridge: %v", e.Err)
}

func (e *Unreachable) Unwrap() error { return e.Err }

// Client talks to a single bridge endpoint.
type Client struct {
	Host     string
	Port     int
	Timeout  time.Duration // per-socket timeout for dial and reads
	Attempts int           // transport retry count (>= 1)

	// levelMu serializes every RunOnLevel call, targeted or not. set_level is
	// bridge-side GLOBAL mutable state, addressed by index, and every create
	// command implicitly targets whatever level is currently active — so two
	// RunOnLevel calls racing (e.g. two resources targeting different levels
	// created concurrently, which Terraform's default parallelism makes
	// routine) can otherwise interleave their set_level and create calls and
	// land an element on the wrong level. This must be held across the whole
	// resolve+switch+act sequence, not just the set_level call itself:
	// bridge_app's own agentPump already serializes individual *commands*
	// onto the single DD agent link (bridge_app/internal/core/bridge.go),
	// but that doesn't make this client's 3-command sequence atomic — two
	// different callers' sequences can still interleave inside that queue
	// unless something on this side holds them together.
	//
	// Crucially this must be held for a levelID=="" call too, not just
	// skipped: a plain create with no level targeting can still land between
	// a concurrent level-targeted call's set_level and its own create if it
	// isn't serialized against that call — an untargeted create is not
	// exempt from the race just because it doesn't care which level it's
	// on. bridge_app's own per-command queue doesn't help here either (see
	// above); only excluding the two calls from each other on this side
	// does. The cost is every create now serializes with every other create
	// whenever level_id is used anywhere in the plan — acceptable, since
	// bridge_app was already a de facto bottleneck of one command at a time.
	levelMu sync.Mutex
}

// New returns a client for host:port with the defaults bridge_client.py used.
func New(host string, port int) *Client {
	return &Client{Host: host, Port: port, Timeout: 5 * time.Second, Attempts: 3}
}

// Request sends one command with optional params and returns the decoded result
// map. Params with nil values are omitted (the JSON protocol keys every param).
func (c *Client) Request(ctx context.Context, cmd string, params map[string]any) (map[string]any, error) {
	payload := map[string]any{"cmd": cmd}
	for k, v := range params {
		if v != nil {
			payload[k] = v
		}
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("bridge request marshal: %w", err)
	}
	frame := append(buf, '\n')

	var lastErr error
	for attempt := 0; attempt < c.Attempts; attempt++ {
		result, err := c.roundTrip(ctx, cmd, frame)
		if err == nil {
			return result, nil
		}
		// App-level rejections are definitive — don't retry a request the
		// bridge already answered.
		var appErr *Error
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	return nil, lastErr
}

// RunOnLevel resolves levelID (a level's `id`, as dungeondraft_level.x.id
// reports — NOT the `index` set_level actually takes; add_level never
// returns an index, only list_levels does, so this always re-resolves it
// fresh rather than trusting a cached one) to its current index, switches
// the bridge's active level, then runs fn — the whole sequence under
// levelMu so no other RunOnLevel call's set_level/create can land in
// between. levelMu is held even when levelID is empty (see its doc comment
// on Client): every RunOnLevel call — targeted or not — must be excluded
// from every other one, or an untargeted call can land between a targeted
// call's set_level and its own create.
func (c *Client) RunOnLevel(ctx context.Context, levelID string, fn func() (map[string]any, error)) (map[string]any, error) {
	c.levelMu.Lock()
	defer c.levelMu.Unlock()

	if levelID == "" {
		return fn()
	}

	lv, err := c.Request(ctx, "list_levels", nil)
	if err != nil {
		return nil, fmt.Errorf("resolving level %s: %w", levelID, err)
	}
	levels, _ := lv["levels"].([]any)
	index := int64(-1)
	for _, raw := range levels {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if formatJSONNumber(m["id"]) == levelID {
			index = jsonNumberAsInt64(m["index"])
			break
		}
	}
	if index < 0 {
		return nil, &Error{Msg: fmt.Sprintf("no level with id %s (from list_levels)", levelID)}
	}
	if _, err := c.Request(ctx, "set_level", map[string]any{"index": index}); err != nil {
		return nil, fmt.Errorf("set_level(%d) for level %s: %w", index, levelID, err)
	}
	return fn()
}

// jsonNumberAsInt64 coerces a JSON number (float64/int64/int from
// encoding/json) to int64.
func jsonNumberAsInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// formatJSONNumber renders a decoded JSON id — number or string — the same
// way createNode/idAsString (ddprovider package) render one for a
// resource's Terraform id, so a level_id attribute value compares equal to
// what list_levels reports regardless of which JSON type the bridge sent
// it as. Kept in sync with idAsString by hand (see this package's doc
// comment: it's a deliberate vendored copy, not a shared import).
func formatJSONNumber(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%d", jsonNumberAsInt64(v))
}

// roundTrip opens a fresh connection (the bridge reaps idle peers, so reuse
// buys little and fresh avoids the stale-socket class of bug), sends one frame
// and reads exactly one reply line.
func (c *Client) roundTrip(ctx context.Context, cmd string, frame []byte) (map[string]any, error) {
	dialer := net.Dialer{Timeout: c.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", c.Host, c.Port))
	if err != nil {
		return nil, &Unreachable{Err: fmt.Errorf("connecting: %w", err)}
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}
	_ = conn.SetDeadline(time.Now().Add(c.Timeout))

	if _, err := conn.Write(frame); err != nil {
		return nil, &Unreachable{Err: fmt.Errorf("writing request: %w", err)}
	}
	reader := bufio.NewReaderSize(conn, 64*1024)
	line, err := readLine(reader)
	if err != nil {
		return nil, &Unreachable{Err: err}
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(line, &decoded); err != nil {
		return nil, &Unreachable{Err: fmt.Errorf("invalid response from bridge: %w", err)}
	}
	ok, _ := decoded["ok"].(bool)
	if !ok {
		msg, _ := decoded["error"].(string)
		if msg == "" {
			msg = "unknown bridge error"
		}
		return nil, &Error{Msg: fmt.Sprintf("bridge rejected %s: %s", cmd, msg)}
	}
	result, _ := decoded["result"].(map[string]any)
	return result, nil
}

// readLine reads until a newline (reassembling multi-segment replies).
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := r.ReadBytes('\n')
		buf.Write(line)
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			return nil, fmt.Errorf("reading reply (closed without responding? %w)", err)
		}
		return bytes.TrimRight(buf.Bytes(), "\r\n"), nil
	}
}
