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