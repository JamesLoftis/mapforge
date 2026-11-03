package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
)

// captureFrames answers every request with {"ok":true} and hands each
// decoded frame to the returned channel.
func captureFrames(t *testing.T) (*Client, <-chan map[string]any) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	frames := make(chan map[string]any, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				frame := map[string]any{}
				_ = json.Unmarshal(line, &frame)
				frames <- frame
				c.Write([]byte(`{"ok":true,"result":{}}` + "\n"))
			}(conn)
		}
	}()
	host, port := splitAddr(ln.Addr().String())
	return New(host, port), frames
}

func TestRequestCarriesMapFile(t *testing.T) {
	c, frames := captureFrames(t)
	c.MapFile = scratchPath
	if _, err := c.Request(context.Background(), "get_element", map[string]any{"id": 1}); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if got := (<-frames)["map"]; got != scratchPath {
		t.Errorf("frame map = %v, want %q", got, scratchPath)
	}
}

func TestPingCarriesNoMapFile(t *testing.T) {
	c, frames := captureFrames(t)
	c.MapFile = scratchPath
	if _, err := c.Request(context.Background(), "ping", nil); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if got, ok := (<-frames)["map"]; ok {
		t.Errorf("ping carried map %v; ping must work whatever map is open", got)
	}
}
