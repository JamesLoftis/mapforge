// Package bridge is a Go client for the Dungeondraft MCP bridge mod's wire
// protocol (see PROTOCOL.md): one JSON object per newline-delimited frame,
// requests {"cmd": ..., params...}, responses {"ok": true, "result": ...} or
// {"ok": false, "error": "..."} on the same TCP connection.
//
// Failure classification mirrors bridge_client.py: transport problems
// (unreachable, reset mid-reply, closed without responding) are retried up to
// Attempts times on a fresh connection, while an application rejection from the
// bridge itself ({"ok": false}, e.g. "no map open") surfaces as an Error and is
// never retried.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

func (e *Unreachable) Error() string { return fmt.Sprintf("could not reach the Dungeondraft bridge: %v", e.Err) }

func (e *Unreachable) Unwrap() error { return e.Err }

// Client talks to a single bridge endpoint.
type Client struct {
	Host    string
	Port    int
	Timeout time.Duration // per-socket timeout for dial and reads
	Attempts int          // transport retry count (>= 1)
}

// New returns a client for host:port with the defaults bridge_client.py uses.
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