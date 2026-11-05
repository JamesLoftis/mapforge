package core

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Command log events (PROTOCOL.md, Command log).
const (
	eventSent     = "sent"     // about to be written to Dungeondraft
	eventDone     = "done"     // Dungeondraft answered, or the wait ended
	eventRefused  = "refused"  // answered by bridge_app; never reached Dungeondraft
	eventReplayed = "replayed" // a retry answered with the original's reply
)

// unnamedClient is the sender logged for a frame that carries no "client".
const unnamedClient = "other"

// commandEntry is one command log line. Optional fields are omitted when
// empty, so each event carries only what it knows.
type commandEntry struct {
	Time    string          `json:"time"`
	Event   string          `json:"event"`
	ReqID   string          `json:"req_id,omitempty"`
	Client  string          `json:"client"`
	Conn    int64           `json:"conn"`
	Cmd     string          `json:"cmd"`
	Request json.RawMessage `json:"request,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Code    string          `json:"code,omitempty"`
	Error   string          `json:"error,omitempty"`
	MS      *int64          `json:"ms,omitempty"`
}

// commandLog appends one JSON line per command event. Safe for concurrent
// use; a write failure is reported once and never blocks a command.
type commandLog struct {
	mu       sync.Mutex
	w        io.Writer
	failed   bool
	reportFn func(format string, args ...any)
}

func newCommandLog(w io.Writer, report func(format string, args ...any)) *commandLog {
	return &commandLog{w: w, reportFn: report}
}

// entryFor starts an entry describing jb's command.
func entryFor(event string, jb job) commandEntry {
	client := fstring(jb.req.parsed, "client")
	if client == "" {
		client = unnamedClient
	}
	return commandEntry{
		Time:   time.Now().Format(time.RFC3339Nano),
		Event:  event,
		ReqID:  fstring(jb.req.parsed, "req_id"),
		Client: client,
		Conn:   jb.conn.id,
		Cmd:    fstring(jb.req.parsed, "cmd"),
	}
}

// withRequest adds the whole request frame, so the line alone reproduces it.
func (e commandEntry) withRequest(jb job) commandEntry {
	e.Request = json.RawMessage(jb.req.raw)
	return e
}

// withOutcome adds the reply's ok/code/error, and how long it took.
func (e commandEntry) withOutcome(reply []byte, began time.Time) commandEntry {
	var r struct {
		OK    bool   `json:"ok"`
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(reply, &r)
	ms := time.Since(began).Milliseconds()
	e.OK, e.Code, e.Error, e.MS = &r.OK, r.Code, r.Error, &ms
	return e
}

func (l *commandLog) record(e commandEntry) {
	if l.w == nil {
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.w.Write(append(line, '\n')); err != nil && !l.failed {
		l.failed = true
		l.reportFn("command log write failed, continuing without it: %v", err)
	}
}
