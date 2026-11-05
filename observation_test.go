package main

import (
	"testing"
	"time"
)

func TestFocusObservationDoesNotFillGaps(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var o focusObservation
	if _, ok := o.observe(start, "browser", true, 0); ok {
		t.Fatal("first observation is not a duration")
	}
	if _, ok := o.observe(start.Add(500*time.Millisecond), "browser", true, 0); ok {
		t.Fatal("sub-second observation emitted an empty interval")
	}
	e, ok := o.observe(start.Add(2*time.Second), "browser", true, 0)
	if !ok || e.seconds != 2 || e.app != "browser" {
		t.Fatalf("bounded observation: %+v %v", e, ok)
	}
	if _, ok = o.observe(start.Add(3*time.Second), "editor", true, 0); ok {
		t.Fatal("focus change filled an unknown interval")
	}
	if _, ok = o.observe(start.Add(20*time.Second), "editor", true, 0); ok {
		t.Fatal("sleep or missed observations were counted")
	}
	if _, ok = o.observe(start.Add(22*time.Second), "editor", false, 1); ok {
		t.Fatal("disconnected observation counted")
	}
	if _, ok = o.observe(start.Add(24*time.Second), "editor", true, 1); ok {
		t.Fatal("reconnection filled unknown time")
	}
	if _, ok = o.observe(start.Add(25*time.Second), "editor", true, 2); ok {
		t.Fatal("IPC reconnection generation was ignored")
	}
	if _, ok = o.observe(start.Add(24*time.Second), "editor", true, 2); ok {
		t.Fatal("backward clock counted")
	}
}
func TestSuppressionRecordedOncePerDueEpisode(t *testing.T) {
	seen := map[int]string{}
	if !suppressionOnce(seen, 0, "pause") || suppressionOnce(seen, 0, "pause") {
		t.Fatal("pause episode duplicated")
	}
	if !suppressionOnce(seen, 0, "quiet") || suppressionOnce(seen, 0, "quiet") {
		t.Fatal("changed suppression reason not tracked once")
	}
	delete(seen, 0)
	if !suppressionOnce(seen, 0, "pause") {
		t.Fatal("new episode not recorded")
	}
}
