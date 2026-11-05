package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for the bridge's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		out = append(out, m)
	}
	return out
}

func recordsWithEvent(recs []map[string]any, event string) []map[string]any {
	var out []map[string]any
	for _, r := range recs {
		if r["event"] == event {
			out = append(out, r)
		}
	}
	return out
}

// Spec goal 4: after a freeze, the last line must name the culprit, so the
// line is written before Dungeondraft receives the command.
func TestCommandIsLoggedBeforeItIsForwarded(t *testing.T) {
	log := &syncBuffer{}
	addr, _, _ := startBridge(t, Options{CommandLog: log})
	atForward := make(chan []map[string]any, 1)
	agent := fakeAgentFunc(t, addr, func(req map[string]any) map[string]any {
		atForward <- recordsWithEvent(log.records(t), "sent")
		return map[string]any{"ok": true, "result": map[string]any{"id": 7}}
	})
	defer agent.conn.Close()

	cl := dial(t, addr)
	defer cl.conn.Close()
	request := map[string]any{"cmd": "place_object", "client": "terraform", "req_id": "r1", "x": 1.5}
	cl.send(request)
	cl.recv(t)

	sentAtForward := <-atForward
	if len(sentAtForward) != 1 {
		t.Fatalf("when the agent got the command the log held %d sent lines, want 1", len(sentAtForward))
	}
	got := sentAtForward[0]
	if got["cmd"] != "place_object" || got["client"] != "terraform" || got["req_id"] != "r1" {
		t.Errorf("sent line = %v; want cmd place_object, client terraform, req_id r1", got)
	}
	if fmt.Sprint(got["request"]) != fmt.Sprint(request) {
		t.Errorf("sent line request = %v, want the frame sent %v", got["request"], request)
	}
}

func TestCommandLogRecordsOutcome(t *testing.T) {
	log := &syncBuffer{}
	addr, _, _ := startBridge(t, Options{CommandLog: log})
	agent := fakeAgentFunc(t, addr, func(map[string]any) map[string]any {
		return map[string]any{"ok": false, "code": "not_found", "error": "no element 9"}
	})
	defer agent.conn.Close()

	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_element", "id": 9, "req_id": "r2"})
	cl.recv(t)

	done := recordsWithEvent(log.records(t), "done")
	if len(done) != 1 {
		t.Fatalf("got %d done lines, want 1", len(done))
	}
	d := done[0]
	ms, hasMS := d["ms"].(float64)
	if d["req_id"] != "r2" || d["ok"] != false || d["code"] != "not_found" || d["error"] != "no element 9" ||
		!hasMS || ms < 0 {
		t.Fatalf("done line = %v; want req_id r2, ok false, code not_found, its error, ms >= 0", d)
	}
}

func TestUnsentCommandLoggedAsRefused(t *testing.T) {
	log := &syncBuffer{}
	addr, _, _ := startBridge(t, Options{CommandLog: log})
	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "add_light"})
	cl.recv(t)

	recs := log.records(t)
	refused := recordsWithEvent(recs, "refused")
	if len(refused) != 1 || refused[0]["code"] != "offline" {
		t.Fatalf("refused lines = %v; want one with code offline", refused)
	}
	if sent := recordsWithEvent(recs, "sent"); len(sent) != 0 {
		t.Fatalf("got sent lines %v for a command Dungeondraft never received", sent)
	}
}

func TestCommandWithoutClientLoggedAsOther(t *testing.T) {
	log := &syncBuffer{}
	addr, _, _ := startBridge(t, Options{CommandLog: log})
	agent := fakeAgent(t, addr, map[string]any{})
	defer agent.conn.Close()
	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_status"})
	cl.recv(t)

	sent := recordsWithEvent(log.records(t), "sent")
	if len(sent) != 1 || sent[0]["client"] != "other" {
		t.Fatalf("sent lines = %v; want one with client other", sent)
	}
}

// Its value is under -race: many users write log lines at once.
func TestCommandLogLinesStayWhole(t *testing.T) {
	log := &syncBuffer{}
	addr, _, _ := startBridge(t, Options{CommandLog: log, CommandTimeout: 5 * time.Second})
	agent := fakeAgent(t, addr, map[string]any{})
	defer agent.conn.Close()

	const users, commands = 20, 20
	var wg sync.WaitGroup
	for u := 0; u < users; u++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cl := dial(t, addr)
			defer cl.conn.Close()
			for i := 0; i < commands; i++ {
				cl.send(map[string]any{"cmd": "get_status"})
				cl.recvWithin(t, 5*time.Second)
			}
		}()
	}
	wg.Wait()

	if got, want := len(log.records(t)), users*commands*2; got != want {
		t.Fatalf("got %d log lines, want %d (a sent and a done per command)", got, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestCommandLogWriteFailureDoesNotBlock(t *testing.T) {
	addr, _, _ := startBridge(t, Options{CommandLog: failingWriter{}})
	agent := fakeAgent(t, addr, map[string]any{"fine": true})
	defer agent.conn.Close()
	cl := dial(t, addr)
	defer cl.conn.Close()
	cl.send(map[string]any{"cmd": "get_status"})
	if m := cl.recvWithin(t, 3*time.Second); !okTrue(m) {
		t.Fatalf("with a failing command log the reply was %v; want the agent's ok reply", m)
	}
}
