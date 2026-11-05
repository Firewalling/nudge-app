package main

import (
	"strings"
	"sync"
	"time"
)

// schedulerSnapshot is written only by the scheduling loop; HTTP readers receive a copy.
type schedulerSnapshot struct {
	revision       string
	focusedApp     string
	niriError      string
	activeRules    []string
	next           time.Time
	description    string
	suppressed     bool
	schedulerError string
}
type schedulerState struct {
	mu       sync.RWMutex
	snapshot schedulerSnapshot
	insights *insightsRecorder
}

func (s *schedulerState) set(v schedulerSnapshot) { s.mu.Lock(); s.snapshot = v; s.mu.Unlock() }
func (s *schedulerState) get() schedulerSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}
func (s *schedulerState) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.schedulerError = err.Error()
	s.snapshot.next = time.Time{}
}

// snapshotSchedule reports earliest eligibility under the current focus and cooldown,
// not a guarantee of delivery; another app focus or a failure can change it.
func snapshotSchedule(c Config, app string, focusErr error, sessions map[int]watchSession, now time.Time, schedulerErr string) schedulerSnapshot {
	s := schedulerSnapshot{revision: configETag(c), focusedApp: app, activeRules: []string{}, schedulerError: schedulerErr}
	if focusErr != nil {
		s.niriError = focusErr.Error()
	}
	if now.Before(c.PausedUntil) || inQuietHours(c, now) {
		s.suppressed = true
		s.description = "Scheduling is suppressed."
		return s
	}
	if schedulerErr != "" {
		s.description = "Next reminder unavailable while the scheduler has an error."
		return s
	}
	var earliest time.Time
	for i, r := range c.Reminders {
		if r.Disabled {
			continue
		}
		var due time.Time
		switch r.Kind {
		case "watch":
			if focusErr != nil || app == "" || !strings.EqualFold(r.App, app) {
				continue
			}
			s.activeRules = append(s.activeRules, r.Message)
			session := sessions[i]
			if session.lastSent.IsZero() {
				due = now
			} else if d, err := duration(r.Every); err == nil {
				due = session.lastSent.Add(d)
			} else {
				continue
			}
		case "every":
			if r.Last.IsZero() {
				due = now
			} else if d, err := duration(r.Every); err == nil {
				due = r.Last.Add(d)
			} else {
				continue
			}
		default:
			continue
		}
		if due.Before(now) {
			due = now
		}
		if !c.LastNotification.IsZero() {
			d, err := duration(c.Cooldown)
			if err != nil {
				d = 5 * time.Minute
			}
			if until := c.LastNotification.Add(d); until.After(due) {
				due = until
			}
		}
		if earliest.IsZero() || due.Before(earliest) {
			earliest = due
		}
	}
	if earliest.IsZero() {
		s.description = "No reminder is currently scheduled; watched rules depend on focus."
		return s
	}
	if inQuietHours(c, earliest.In(now.Location())) {
		s.description = "The next eligible time falls during quiet hours."
		return s
	}
	s.next = earliest
	s.description = "Earliest eligibility with the current focus; delivery may be later."
	return s
}
