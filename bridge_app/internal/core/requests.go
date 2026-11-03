package core

import (
	"sync"
	"time"
)

// requestMemory is how long a finished request's reply is replayed to a
// retry carrying the same req_id.
const requestMemory = 2 * time.Minute

// pendingRequest is one logical request, however many times a client sent
// it. done closes once reply is final.
type pendingRequest struct {
	id         string
	done       chan struct{}
	reply      []byte
	finishedAt time.Time
}

// requestTable runs a request at most once per req_id: a client retrying
// after a timeout joins the original instead of executing the edit again
// (spec finding 2). Safe for concurrent use.
type requestTable struct {
	mu      sync.Mutex
	entries map[string]*pendingRequest
}

func newRequestTable() *requestTable {
	return &requestTable{entries: map[string]*pendingRequest{}}
}

// join returns the request for id and whether it was already known (a
// repeat, to be answered with its reply rather than run).
func (rt *requestTable) join(id string, now time.Time) (*pendingRequest, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if p, ok := rt.entries[id]; ok {
		return p, true
	}
	rt.pruneLocked(now)
	p := &pendingRequest{id: id, done: make(chan struct{})}
	rt.entries[id] = p
	return p, false
}

// finish records the reply of a request Dungeondraft received (it ran, or
// may have) and wakes every repeat waiting on it.
func (rt *requestTable) finish(p *pendingRequest, reply []byte, now time.Time) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	p.reply, p.finishedAt = reply, now
	close(p.done)
}

// forget answers the current waiters of a request that never reached
// Dungeondraft (offline, stale mod), and lets a later repeat run afresh.
func (rt *requestTable) forget(p *pendingRequest, reply []byte) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.entries[p.id] == p {
		delete(rt.entries, p.id)
	}
	p.reply = reply
	close(p.done)
}

func (rt *requestTable) pruneLocked(now time.Time) {
	for id, p := range rt.entries {
		if !p.finishedAt.IsZero() && now.Sub(p.finishedAt) > requestMemory {
			delete(rt.entries, id)
		}
	}
}
