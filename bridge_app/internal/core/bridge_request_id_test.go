package core

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

// countingAgent replies after delay, echoing req_id, and counts commands.
func countingAgent(t *testing.T, addr string, delay time.Duration) (*client, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	cl := fakeAgentFunc(t, addr, func(req map[string]any) map[string]any {
		count.Add(1)
		time.Sleep(delay)
		return map[string]any{"ok": true, "result": map[string]any{"id": 7}, "req_id": req["req_id"]}
	})
	return cl, &count
}

func TestDuplicateRequestRunsOnce(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	agent, count := countingAgent(t, addr, 300*time.Millisecond)
	defer agent.conn.Close()

	first, retry := dial(t, addr), dial(t, addr)
	defer first.conn.Close()
	defer retry.conn.Close()
	frame := map[string]any{"cmd": "add_light", "req_id": "r1"}
	first.send(frame)
	time.Sleep(50 * time.Millisecond)
	retry.send(frame)

	for _, cl := range []*client{first, retry} {
		if m := cl.recv(t); !okTrue(m) {
			t.Fatalf("reply %v", m)
		}
	}
	if n := count.Load(); n != 1 {
		t.Fatalf("agent ran the command %d times; want 1", n)
	}
}

func TestRepeatAfterFinishGetsCachedReply(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	agent, count := countingAgent(t, addr, 0)
	defer agent.conn.Close()

	cl := dial(t, addr)
	defer cl.conn.Close()
	frame := map[string]any{"cmd": "add_light", "req_id": "r2"}
	cl.send(frame)
	cl.recv(t)
	cl.send(frame)
	if m := cl.recv(t); !okTrue(m) || count.Load() != 1 {
		t.Fatalf("repeat got %v after %d runs; want the cached reply and 1 run", m, count.Load())
	}
}

func TestOfflineAnswerIsNotRemembered(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	cl := dial(t, addr)
	defer cl.conn.Close()
	frame := map[string]any{"cmd": "add_light", "req_id": "r3"}
	cl.send(frame)
	if m := cl.recv(t); codeOf(m) != "offline" {
		t.Fatalf("got %v; want offline", m)
	}
	agent, count := countingAgent(t, addr, 0)
	defer agent.conn.Close()
	cl.send(frame)
	if m := cl.recv(t); !okTrue(m) || count.Load() != 1 {
		t.Fatalf("retry after Dungeondraft came back got %v (%d runs); want it to run", m, count.Load())
	}
}

func TestMismatchedAgentReplyIgnored(t *testing.T) {
	addr, _, _ := startBridge(t, Options{})
	agent := helloAgent(t, addr)
	defer agent.conn.Close()
	go func() {
		if _, err := agent.reader.ReadString('\n'); err != nil {
			return
		}
		for _, id := range []string{"stale", "r4"} {
			b, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"for": id}, "req_id": id})
			agent.conn.Write(append(b, '\n'))
		}
	}()

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_status", "req_id": "r4"})
	if got := resultOf(t, cl.recv(t))["for"]; got != "r4" {
		t.Fatalf("got the reply for %v; want r4's", got)
	}
}
