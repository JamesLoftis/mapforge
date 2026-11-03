//go:build integration

package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bridge "dungeondraft-mcp/bridgeclient"
)

const (
	parallelEdits    = 10
	editReplyTimeout = 50 * time.Millisecond // shorter than the queue: forces retries
)

// TestSlowEditRetriedCreatesOneElement reproduces spec finding 2: a client
// that times out on a queued edit and retries must not create a duplicate.
func TestSlowEditRetriedCreatesOneElement(t *testing.T) {
	scratchMap(t)
	var retried atomic.Int32
	var edits sync.WaitGroup
	for i := 0; i < parallelEdits; i++ {
		edits.Add(1)
		go func(i int) {
			defer edits.Done()
			c := bridge.New(bridgeHost, bridgePort)
			c.Timeout, c.ReplyTimeout, c.Attempts = 5*time.Second, editReplyTimeout, 3
			began := time.Now()
			_, err := c.Request(context.Background(), "add_light", map[string]any{"x": 100 + 50*i, "y": 100})
			if err != nil || time.Since(began) > editReplyTimeout {
				retried.Add(1)
			}
		}(i)
	}
	edits.Wait()
	if retried.Load() == 0 {
		t.Fatalf("no edit waited past its %s reply timeout, so nothing was retried; the test proves nothing",
			editReplyTimeout)
	}
	if lights := settledCount(t, "lights"); lights != parallelEdits {
		t.Fatalf("%d retried edits made %d lights; want exactly %d", retried.Load(), lights, parallelEdits)
	}
}

// settledCount waits until the open map's count for kind stops changing
// (queued edits finish after their clients gave up) and returns it.
func settledCount(t *testing.T, kind string) int {
	t.Helper()
	last, stableSince := -1, time.Now()
	for time.Since(stableSince) < time.Second {
		st, err := dd.Status(ctx(t))
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if n := st.Counts[kind]; n != last {
			last, stableSince = n, time.Now()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return last
}
