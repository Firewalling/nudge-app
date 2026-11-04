package main

import (
	"errors"
	"testing"
	"time"
)

func TestSchedulerSnapshotEligibility(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := Config{Cooldown: "5m", LastNotification: now.Add(-2 * time.Minute), Reminders: []Reminder{
		{Kind: "watch", App: "firefox", Every: "10m", Message: "Look up"},
		{Kind: "every", Every: "1h", Message: "Rest", Last: now.Add(-30 * time.Minute)},
		{Kind: "watch", App: "other", Every: "1m", Message: "Other"},
	}}
	sessions := map[int]watchSession{0: {started: now.Add(-time.Minute), lastSent: now.Add(-time.Minute)}}
	snap := snapshotSchedule(c, "Firefox", nil, sessions, now, "")
	if len(snap.activeRules) != 1 || snap.activeRules[0] != "Look up" {
		t.Fatalf("active rules: %v", snap.activeRules)
	}
	if want := now.Add(9 * time.Minute); !snap.next.Equal(want) {
		t.Fatalf("next = %v want %v", snap.next, want)
	}
	sessions[0] = watchSession{started: now}
	snap = snapshotSchedule(c, "Firefox", nil, sessions, now, "")
	if want := now.Add(3 * time.Minute); !snap.next.Equal(want) {
		t.Fatalf("cooldown eligibility = %v want %v", snap.next, want)
	}
	snap = snapshotSchedule(c, "", errors.New("niri unavailable"), sessions, now, "")
	if len(snap.activeRules) != 0 || snap.niriError == "" || !snap.next.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("disconnected: %+v", snap)
	}
	quiet := Config{Cooldown: "5m", QuietStart: "12:15", QuietEnd: "13:00", Reminders: []Reminder{{Kind: "every", Every: "1h", Last: now.Add(-30 * time.Minute)}}}
	if got := snapshotSchedule(quiet, "", nil, nil, now, ""); !got.next.IsZero() {
		t.Fatalf("eligibility during quiet hours must not be shown: %+v", got)
	}
	c.PausedUntil = now.Add(time.Hour)
	snap = snapshotSchedule(c, "firefox", nil, sessions, now, "")
	if !snap.suppressed || !snap.next.IsZero() {
		t.Fatalf("paused: %+v", snap)
	}
}

func TestSchedulerStatusHidesStaleOrFailedPredictions(t *testing.T) {
	c := Config{Cooldown: "5m", Reminders: []Reminder{{Kind: "every", Every: "1h", Message: "Rest"}}}
	state := &schedulerState{}
	if got := status(c, state); got.SchedulerReady || got.FocusKnown || got.MatchingRulesKnown {
		t.Fatalf("startup was reported as known: %+v", got)
	}
	state.set(snapshotSchedule(c, "", nil, nil, time.Now(), ""))
	if got := status(c, state); got.NextReminder == "" {
		t.Fatalf("missing eligibility: %+v", got)
	}
	changed := c
	changed.Cooldown = "10m"
	if got := status(changed, state); got.NextReminder != "" {
		t.Fatalf("stale eligibility exposed: %+v", got)
	}
	state.fail(errors.New("write failed"))
	if got := status(c, state); got.NextReminder != "" || got.SchedulerError == "" {
		t.Fatalf("failed scheduler: %+v", got)
	}
}
