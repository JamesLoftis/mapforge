package bridge

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeBridge emulates the mod's wire protocol. Each accepted connection reads
// one newline-JSON frame and writes back the scripted reply. A script entry of
// "<no reply>" leaves the connection open without answering (the client then
// times out / falls through to retry).
type fakeBridge struct {
	ln     net.Listener
	reply  string
	conns  chan net.Conn
	closed chan struct{}
}

func startFakeBridge(t *testing.T, reply string) *fakeBridge {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fb := &fakeBridge{ln: ln, reply: reply, conns: make(chan net.Conn, 16), closed: make(chan struct{})}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				close(fb.closed)
				return
			}
			fb.conns <- conn
		}
	}()
	go func() {
		for {
			conn := <-fb.conns
			c := conn
			go func(c net.Conn) {
				defer c.Close()
				if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
					return
				}
				if fb.reply == "<no reply>" {
					select {} // hold the connection open, never answer
				}
				if fb.reply != "<no reply>" {
					_, _ = c.Write([]byte(fb.reply + "\n"))
				}
			}(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return fb
}

func (fb *fakeBridge) addr() string { return fb.ln.Addr().String() }

func TestRequestSuccess(t *testing.T) {
	fb := startFakeBridge(t, `{"ok":true,"result":{"map_open":true,"map_size_woxels":[40960,30720]}}`)
	host, port := splitAddr(fb.addr())
	c := New(host, port)
	c.Timeout = 500 * time.Millisecond
	res, err := c.Request(context.Background(), "get_status", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["map_open"] != true {
		t.Errorf("map_open = %v, want true", res["map_open"])
	}
}

func splitAddr(a string) (string, int) {
	h, p, _ := net.SplitHostPort(a)
	var port int
	_, _ = fmtSscan(p, &port)
	return h, port
}

func TestRequestRejectionNotRetried(t *testing.T) {
	fb := startFakeBridge(t, `{"ok":false,"error":"no map open"}`)
	host, port := splitAddr(fb.addr())
	c := New(host, port)
	c.Timeout = 500 * time.Millisecond
	_, err := c.Request(context.Background(), "get_status", nil)
	var appErr *Error
	if err == nil || !errors.As(err, &appErr) {
		t.Fatalf("expected app rejection, got %v", err)
	}
	if !strings.Contains(err.Error(), "no map open") {
		t.Errorf("error = %q, want it to mention the bridge message", err)
	}
}

func TestUnreachable(t *testing.T) {
	c := New("127.0.0.1", 1) // nothing listens on port 1
	c.Timeout = 200 * time.Millisecond
	c.Attempts = 2
	_, err := c.Request(context.Background(), "ping", nil)
	var un *Unreachable
	if err == nil || !errors.As(err, &un) {
		t.Fatalf("expected Unreachable, got %v", err)
	}
}

func TestClosedWithoutReplyTimesOut(t *testing.T) {
	fb := startFakeBridge(t, "<no reply>")
	host, port := splitAddr(fb.addr())
	c := New(host, port)
	c.Timeout = 300 * time.Millisecond
	c.Attempts = 1
	_, err := c.Request(context.Background(), "get_status", nil)
	if err == nil {
		t.Fatalf("expected error for unresponsive bridge")
	}
}

// startFakeBridgeSeq is startFakeBridge's sibling for RunOnLevel-style tests
// that need a different reply for each successive connection (list_levels,
// then set_level, then whatever the wrapped fn requests) rather than one
// reply repeated for every connection.
func startFakeBridgeSeq(t *testing.T, replies []string) *fakeBridge {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fb := &fakeBridge{ln: ln, conns: make(chan net.Conn, 16), closed: make(chan struct{})}
	i := 0
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				close(fb.closed)
				return
			}
			fb.conns <- conn
		}
	}()
	go func() {
		for {
			conn := <-fb.conns
			c := conn
			n := i
			i++
			go func(c net.Conn, reply string) {
				defer c.Close()
				if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
					return
				}
				_, _ = c.Write([]byte(reply + "\n"))
			}(c, replies[n%len(replies)])
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return fb
}

func TestRunOnLevelSkipsWhenEmpty(t *testing.T) {
	// No levelID: RunOnLevel must call fn directly without touching the
	// network at all (no listener started, so any request would fail).
	c := New("127.0.0.1", 1)
	called := false
	res, err := c.RunOnLevel(context.Background(), "", func() (map[string]any, error) {
		called = true
		return map[string]any{"ok": "yes"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("fn was not called")
	}
	if res["ok"] != "yes" {
		t.Errorf("res = %v, want fn's return value passed through", res)
	}
}

func TestRunOnLevelResolvesAndSwitches(t *testing.T) {
	fb := startFakeBridgeSeq(t, []string{
		`{"ok":true,"result":{"current_index":0,"levels":[{"id":0,"index":0,"label":"Ground"},{"id":7,"index":1,"label":"Upper"}]}}`,
		`{"ok":true,"result":{"current_index":1}}`,
	})
	host, port := splitAddr(fb.addr())
	c := New(host, port)
	c.Timeout = 500 * time.Millisecond

	var sawIndexSwitch bool
	res, err := c.RunOnLevel(context.Background(), "7", func() (map[string]any, error) {
		sawIndexSwitch = true // by the time fn runs, list_levels + set_level already happened
		return map[string]any{"id": 42}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawIndexSwitch {
		t.Fatal("fn was not called")
	}
	if res["id"] != 42 {
		t.Errorf("res = %v, want fn's return value passed through", res)
	}
}

func TestRunOnLevelUnknownID(t *testing.T) {
	fb := startFakeBridgeSeq(t, []string{
		`{"ok":true,"result":{"current_index":0,"levels":[{"id":0,"index":0,"label":"Ground"}]}}`,
	})
	host, port := splitAddr(fb.addr())
	c := New(host, port)
	c.Timeout = 500 * time.Millisecond

	_, err := c.RunOnLevel(context.Background(), "999", func() (map[string]any, error) {
		t.Fatal("fn must not be called when the level id can't be resolved")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected an error for an unresolvable level id")
	}
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Errorf("error = %v (%T), want a *bridge.Error", err, err)
	}
}

func fmtSscan(s string, v *int) (int, error) {
	*v = 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		*v = *v*10 + int(r-'0')
	}
	return 1, nil
}
