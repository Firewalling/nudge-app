package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFocusChangesInvalidateCoalescedObservation(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "niri")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"WindowFocusChanged\":{\"id\":2}}' '{\"WindowFocusChanged\":{\"id\":1}}'\nexec sleep 30\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var generation atomic.Uint64
	var connected atomic.Bool
	wake := make(chan struct{}, 1)
	go watchNiri(ctx, wake, &generation, &connected)
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for generation.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("focus transitions did not invalidate the observation")
		case <-tick.C:
		}
	}
	if !connected.Load() {
		t.Fatal("event stream disconnected before the observation")
	}
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var observation focusObservation
	observation.observe(start, "browser", true, 0)
	if _, ok := observation.observe(start.Add(5*time.Second), "browser", true, generation.Load()); ok {
		t.Fatal("counted the interval despite intervening focus changes")
	}
}
