package main

import "time"

// focusObservation records only intervals bounded by consistent observations.
type focusObservation struct {
	at         time.Time
	app        string
	generation uint64
}

func (o *focusObservation) observe(now time.Time, app string, valid bool, generation uint64) (insightEvent, bool) {
	if !valid {
		*o = focusObservation{}
		return insightEvent{}, false
	}
	if o.at.IsZero() {
		*o = focusObservation{at: now, app: app, generation: generation}
		return insightEvent{}, false
	}
	gap := now.Sub(o.at)
	wallGap := now.UnixNano() - o.at.UnixNano()
	drift := wallGap - int64(gap)
	if app == o.app && generation == o.generation && gap > 0 && gap <= 10*time.Second && wallGap >= 0 && drift > -int64(time.Second) && drift < int64(time.Second) {
		if gap < time.Second {
			return insightEvent{}, false
		}
		event := insightEvent{at: now, start: o.at, kind: "focus", app: app, seconds: int(gap / time.Second)}
		*o = focusObservation{at: now, app: app, generation: generation}
		return event, true
	}
	*o = focusObservation{at: now, app: app, generation: generation}
	return insightEvent{}, false
}

// suppressionOnce records only the first check of each continuous due episode.
func suppressionOnce(seen map[int]string, rule int, reason string) bool {
	if seen[rule] == reason {
		return false
	}
	seen[rule] = reason
	return true
}
