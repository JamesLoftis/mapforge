// Package bridge is a Go client for the Dungeondraft MCP bridge mod's wire
// protocol (see PROTOCOL.md): one JSON object per newline-delimited frame,
// requests {"cmd": ..., params...}, responses {"ok": true, "result": ...} or
// {"ok": false, "error": "..."} on the same TCP connection.
//
// Shared by provider/ and mcpserver/ — this is the one place the wire
// protocol's client side is implemented; nothing else should dial the bridge
// directly.
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
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// Error is raised when the bridge itself rejects a request (app-level error, never retried).
// Code is the reply's machine-readable reason, "" when it gave none.
type Error struct {
	Msg  string
	Code string
}

func (e *Error) Error() string { return e.Msg }

// Error codes (PROTOCOL.md, Errors). Branch on these, never on message text.
const (
	CodeOffline  = "offline"
	CodeTimeout  = "timeout"
	CodeNotFound = "not_found"
	CodeWrongMap = "wrong_map"
)

// IsNotFound reports whether err is the bridge saying the element does not
// exist — the only answer that means an element is gone.
func IsNotFound(err error) bool {
	var appErr *Error
	return errors.As(err, &appErr) && appErr.Code == CodeNotFound
}

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
	// ReplyTimeout bounds one request's wait for its reply (the read
	// deadline). It must exceed bridge_app's 30 s command timeout.
	ReplyTimeout time.Duration
	// MapFile, when set, rides on every command but ping as "map"; the mod
	// refuses (wrong_map) any command while a different map is open.
	MapFile string
	// Name, when set, rides on every command as "client" so bridge_app's
	// command log can say who sent it ("terraform", "mcp").
	Name string
}

// New returns a client for host:port with the defaults bridge_client.py used.
func New(host string, port int) *Client {
	return &Client{Host: host, Port: port, Timeout: 5 * time.Second, ReplyTimeout: 40 * time.Second, Attempts: 3}
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
	if c.MapFile != "" && cmd != "ping" {
		payload["map"] = c.MapFile
	}
	if c.Name != "" {
		payload["client"] = c.Name
	}
	// One id per logical request, reused on every transport retry, so
	// bridge_app runs a retried command once and replays its reply.
	payload["req_id"] = rand.Text()
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
	_ = conn.SetDeadline(time.Now().Add(c.replyTimeout()))

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
		code, _ := decoded["code"].(string)
		return nil, &Error{Msg: fmt.Sprintf("bridge rejected %s: %s", cmd, msg), Code: code}
	}
	result, _ := decoded["result"].(map[string]any)
	return result, nil
}

// replyTimeout falls back to Timeout for a Client built without New.
func (c *Client) replyTimeout() time.Duration {
	if c.ReplyTimeout > 0 {
		return c.ReplyTimeout
	}
	return c.Timeout
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
