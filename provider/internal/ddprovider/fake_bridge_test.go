package ddprovider

import (
	"bufio"
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
