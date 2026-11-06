package ddprovider

import (
	"bufio"
	"encoding/json"
	"net"
	"strconv"
	"testing"
)

// startReplyServer answers every request line with reply.
func startReplyServer(t *testing.T, reply string) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if _, err := bufio.NewReader(c).ReadString('\n'); err == nil {
					c.Write([]byte(reply + "\n"))
				}
			}(conn)
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ = strconv.Atoi(p)
	return h, port
}

// startCapturingServer answers every request line with reply and passes each
// request it reads, decoded, to the returned channel.
func startCapturingServer(t *testing.T, reply string) (host string, port int, requests <-chan map[string]any) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	seen := make(chan map[string]any, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				var req map[string]any
				if json.Unmarshal([]byte(line), &req) == nil {
					seen <- req
				}
				c.Write([]byte(reply + "\n"))
			}(conn)
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ = strconv.Atoi(p)
	return h, port, seen
}
