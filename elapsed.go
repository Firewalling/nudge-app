package main

import (
	"fmt"
	"strings"
	"time"
)

// elapsedSession counts only bounded, consistent foreground observations.
// It is independent of Insights and is discarded whenever continuity is uncertain.
type elapsedSession struct {
	last       time.Time
	generation uint64
	observed   time.Duration
	sentAt     time.Duration
	sent       bool
}

func (s *elapsedSession) observe(now time.Time, focused, target string, valid bool, generation uint64) {
	if !valid || !strings.EqualFold(focused, target) {
		*s = elapsedSession{}
		return
	}
	if s.last.IsZero() {
		s.last, s.generation = now, generation
		return
	}
	gap := now.Sub(s.last)
	wallGap := now.UnixNano() - s.last.UnixNano()
	drift := wallGap - int64(gap)
	if generation != s.generation || gap < 0 || gap > 10*time.Second || wallGap < 0 || drift <= -int64(time.Second) || drift >= int64(time.Second) {
		*s = elapsedSession{last: now, generation: generation}
		return
	}
	s.observed += gap
	s.last = now
}

func (s elapsedSession) due(rule ElapsedReminder) bool {
	after, err := duration(rule.After)
	if err != nil || s.observed < after {
		return false
	}
	if !s.sent {
		return true
	}
	if rule.Repeat == "" {
		return false
	}
	repeat, err := duration(rule.Repeat)
	return err == nil && s.observed-s.sentAt >= repeat
}

func (s elapsedSession) eligible(rule ElapsedReminder, paused, quiet, allowed bool) bool {
	return !rule.Disabled && !paused && !quiet && allowed && s.due(rule)
}

func elapsedTitle(rule ElapsedReminder, observed time.Duration) string {
	seconds := int64(observed / time.Second)
	duration := ""
	if minutes := seconds / 60; minutes > 0 {
		unit := "minutes"
		if minutes == 1 {
			unit = "minute"
		}
		duration = fmt.Sprintf("%d %s", minutes, unit)
	}
	if remainder := seconds % 60; remainder > 0 || duration == "" {
		unit := "seconds"
		if remainder == 1 {
			unit = "second"
		}
		if duration != "" {
			duration += " "
		}
		duration += fmt.Sprintf("%d %s", remainder, unit)
	}
	return fmt.Sprintf("%s in %s", duration, appName(rule.App))
}
