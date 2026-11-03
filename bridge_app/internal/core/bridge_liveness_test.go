package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func codeOf(m map[string]any) string {
	s, _ := m["code"].(string)
	return s
}

// recvWithin is recv with a caller-chosen deadline.
func (cl *client) recvWithin(t *testing.T, d time.Duration) map[string]any {
	t.Helper()
	cl.conn.SetReadDeadline(time.Now().Add(d))
	line, err := cl.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("no reply within %s: %v", d, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("bad json %q: %v", line, err)
	}
	return m
}

// Plan 1 evidence: after new_map/open_map the new agent superseded the old
// within ~1s, but the in-flight request waited out the full command timeout.
func TestSupersededAgentFailsInFlightRequestFast(t *testing.T) {
	addr, _, _ := startBridge(t, Options{CommandTimeout: 10 * time.Second})
	old := helloAgent(t, addr)
	defer old.conn.Close()
	go drain(old.reader) // receives the command, never replies

	cl := dial(t, addr)
	defer cl.conn.Close()
	start := time.Now()
	cl.send(map[string]any{"cmd": "open_map"})
	time.Sleep(200 * time.Millisecond)
	replacement := helloAgent(t, addr)
	defer replacement.conn.Close()
	go drain(replacement.reader)

	m := cl.recvWithin(t, 5*time.Second)
	if codeOf(m) != "agent_lost" || time.Since(start) > 2*time.Second {
		t.Fatalf("after %s got %v; want code agent_lost within 2s", time.Since(start), m)
	}
	if !strings.Contains(errStr(m), "open_map") {
		t.Errorf("error %q does not name the command it was running", errStr(m))
	}
}

func TestStaleAgentProtocolRejected(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	stale := dial(t, addr)
	defer stale.conn.Close()
	stale.send(map[string]any{"agent_hello": map[string]any{"name": "old-mod", "protocol": PROTOCOL_VERSION - 1}})
	stale.recv(t)
	go func() { // answers anything forwarded, so only the bridge can refuse
		for {
			if _, err := stale.reader.ReadString('\n'); err != nil {
				return
			}
			stale.conn.Write([]byte(`{"ok":true,"result":{}}` + "\n"))
		}
	}()

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_status"})
	m := cl.recv(t)
	old, current := fmt.Sprintf("v%d", PROTOCOL_VERSION-1), fmt.Sprintf("v%d", PROTOCOL_VERSION)
	if codeOf(m) != "protocol_mismatch" || !strings.Contains(errStr(m), old) || !strings.Contains(errStr(m), current) {
		t.Fatalf("got %v; want code protocol_mismatch naming %s and %s", m, old, current)
	}
}

func TestPingReportsAgentProtocol(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	agent := helloAgent(t, addr)
	defer agent.conn.Close()
	go drain(agent.reader)

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "ping"})
	if got := resultOf(t, cl.recv(t))["agent_protocol"]; got != float64(PROTOCOL_VERSION) {
		t.Fatalf("ping agent_protocol = %v, want %d", got, PROTOCOL_VERSION)
	}
}

// Spec finding 5: a user that stops reading must not block the single pump.
func TestUnreadingClientDoesNotBlockOthers(t *testing.T) {
	addr, _, _ := startBridge(t, Options{WriteTimeout: 200 * time.Millisecond, CommandTimeout: 5 * time.Second})
	big := strings.Repeat("x", 512*1024)
	agent := fakeAgent(t, addr, map[string]any{"blob": big})
	defer agent.conn.Close()

	stalled := dial(t, addr)
	defer stalled.conn.Close()
	for i := 0; i < 50; i++ {
		stalled.send(map[string]any{"cmd": "dump_state"})
	}

	other := dial(t, addr)
	defer other.conn.Close()
	other.send(map[string]any{"cmd": "get_status"})
	if m := other.recvWithin(t, 5*time.Second); !okTrue(m) {
		t.Fatalf("other client got %v", m)
	}
}

// Its value is under `go test -race`: many users, a heartbeating agent and
// admin readers all touch shared connection state at once.
func TestConcurrentClientsHeartbeatsAndAdmin(t *testing.T) {
	addr, b, _ := startBridge(t, Options{})
	agent := heartbeatingAgent(t, addr, 10*time.Millisecond)
	defer agent.conn.Close()

	stop := make(chan struct{})
	var admin sync.WaitGroup
	admin.Add(1)
	go func() {
		defer admin.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = b.Connections()
				_ = b.AgentInfo()
				_ = b.Stats()
			}
		}
	}()

	var users sync.WaitGroup
	failures := make(chan string, 400)
	for u := 0; u < 20; u++ {
		users.Add(1)
		go func() {
			defer users.Done()
			cl := dial(t, addr)
			defer cl.conn.Close()
			for i := 0; i < 20; i++ {
				cl.send(map[string]any{"cmd": "get_status"})
				cl.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				line, err := cl.reader.ReadString('\n')
				if err != nil || !strings.Contains(line, `"ok":true`) {
					failures <- fmt.Sprintf("%q %v", line, err)
					return
				}
			}
		}()
	}
	users.Wait()
	close(stop)
	admin.Wait()
	close(failures)
	for f := range failures {
		t.Errorf("command failed: %s", f)
	}
}

// heartbeatingAgent pings like the mod does and answers forwarded commands.
func heartbeatingAgent(t *testing.T, addr string, every time.Duration) *client {
	t.Helper()
	cl := helloAgent(t, addr)
	var writes sync.Mutex
	write := func(line string) {
		writes.Lock()
		defer writes.Unlock()
		cl.conn.Write([]byte(line + "\n"))
	}
	go func() {
		for range time.Tick(every) {
			write(`{"cmd":"ping"}`)
		}
	}()
	go func() {
		for {
			line, err := cl.reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.Contains(line, `"cmd"`) { // a forwarded command, not a pong
				write(`{"ok":true,"result":{}}`)
			}
		}
	}()
	return cl
}
