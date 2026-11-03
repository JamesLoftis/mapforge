package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// dropFirstThenAnswer closes the first connection without replying, then
// answers {"ok":true}; it reports every decoded frame.
func dropFirstThenAnswer(t *testing.T) (*Client, <-chan map[string]any) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	frames := make(chan map[string]any, 8)
	go func() {
		for n := 0; ; n++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			frame := map[string]any{}
			_ = json.Unmarshal(line, &frame)
			frames <- frame
			if n > 0 {
				conn.Write([]byte(`{"ok":true,"result":{}}` + "\n"))
			}
			conn.Close()
		}
	}()
	host, port := splitAddr(ln.Addr().String())
	c := New(host, port)
	c.Timeout, c.ReplyTimeout = time.Second, time.Second
	return c, frames
}

func TestRetryReusesRequestID(t *testing.T) {
	c, frames := dropFirstThenAnswer(t)
	if _, err := c.Request(context.Background(), "add_light", map[string]any{"x": 1}); err != nil {
		t.Fatalf("Request: %v", err)
	}
	first, retry := <-frames, <-frames
	id, _ := first["req_id"].(string)
	if id == "" || retry["req_id"] != id {
		t.Fatalf("req_id first=%v retry=%v; want the same non-empty id on both", first["req_id"], retry["req_id"])
	}
}

func TestEachRequestGetsItsOwnID(t *testing.T) {
	c, frames := captureFrames(t)
	for i := 0; i < 2; i++ {
		if _, err := c.Request(context.Background(), "get_status", nil); err != nil {
			t.Fatalf("Request: %v", err)
		}
	}
	if a, b := (<-frames)["req_id"], (<-frames)["req_id"]; a == b {
		t.Fatalf("two requests share req_id %v", a)
	}
}

func TestWaitMapOpTimeoutNamesLastState(t *testing.T) {
	reply := `{"ok":true,"result":{"busy":true,"last_map_op":{"op":"save_map","path":"a.dungeondraft_map","done":false}}}`
	c := newTestClient(t, startFakeBridge(t, reply))
	err := c.WaitMapOp(context.Background(), "save_map", "a.dungeondraft_map", 600*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "busy true") || !strings.Contains(err.Error(), "save_map") {
		t.Fatalf("err = %v; want the last op and busy state it saw", err)
	}
}
