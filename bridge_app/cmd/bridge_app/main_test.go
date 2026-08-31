package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWatchDungeondraftNeverSeenDoesNotStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	stopped := false
	stop := func() { mu.Lock(); stopped = true; mu.Unlock() }

	isRunning := func(string) (bool, error) { return false, nil }

	watchDungeondraft(ctx, stop, isRunning, "Dungeondraft.exe", 10*time.Millisecond, 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if stopped {
		t.Fatal("watcher stopped the app even though DD was never observed running this run")
	}
}

func TestWatchDungeondraftExitsAfterGrace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stopCh := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(stopCh) }) }

	var mu sync.Mutex
	seen := 0
	isRunning := func(string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		seen++
		return seen <= 2, nil // running for the first two checks, gone after
	}

	go watchDungeondraft(ctx, stop, isRunning, "Dungeondraft.exe", 10*time.Millisecond, 30*time.Millisecond)

	select {
	case <-stopCh:
	case <-ctx.Done():
		t.Fatal("watcher never called stop after DD stayed gone past the grace period")
	}
}

func TestWatchDungeondraftRecoversBeforeGrace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var mu sync.Mutex
	stopped := false
	stop := func() { mu.Lock(); stopped = true; mu.Unlock() }

	// Gone for one tick (well under the 30ms grace), then running again for
	// the rest of the test window.
	pattern := []bool{true, false}
	i := 0
	isRunning := func(string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		var v bool
		if i < len(pattern) {
			v = pattern[i]
		} else {
			v = true
		}
		i++
		return v, nil
	}

	watchDungeondraft(ctx, stop, isRunning, "Dungeondraft.exe", 10*time.Millisecond, 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if stopped {
		t.Fatal("watcher stopped despite DD recovering within the grace period")
	}
}
