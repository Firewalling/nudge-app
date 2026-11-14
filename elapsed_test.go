package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestElapsedSessionThresholdRepeatAndContinuity(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rule := ElapsedReminder{App: "brave", After: "10s", Repeat: "10s", Message: "Still doing what you intended?"}
	var session elapsedSession
	session.observe(start, "brave", rule.App, true, 1)
	session.observe(start.Add(5*time.Second), "brave", rule.App, true, 1)
	if session.due(rule) {
		t.Fatal("notification before threshold")
	}
	session.observe(start.Add(10*time.Second), "brave", rule.App, true, 1)
	if !session.due(rule) {
		t.Fatal("threshold did not become due")
	}
	if got := elapsedMessage(rule, session.observed); got != "10 seconds in Brave. Still doing what you intended?" {
		t.Fatalf("notification = %q", got)
	}
	session.sent, session.sentAt = true, session.observed
	if session.due(rule) {
		t.Fatal("duplicate notification at threshold")
	}
	session.observe(start.Add(15*time.Second), "brave", rule.App, true, 1)
	if session.due(rule) {
		t.Fatal("repeat before repeat interval")
	}
	session.observe(start.Add(20*time.Second), "brave", rule.App, true, 1)
	if !session.due(rule) {
		t.Fatal("repeat did not become due")
	}
	rule.Repeat = ""
	if session.due(rule) {
		t.Fatal("one-shot rule repeated")
	}

	session.observe(start.Add(21*time.Second), "editor", rule.App, true, 2)
	session.observe(start.Add(22*time.Second), "brave", rule.App, true, 3)
	session.observe(start.Add(27*time.Second), "brave", rule.App, true, 3)
	if session.due(rule) || session.observed != 5*time.Second {
		t.Fatal("switch did not start a new session")
	}
	session.observe(start.Add(28*time.Second), "brave", rule.App, false, 4)
	session.observe(start.Add(29*time.Second), "brave", rule.App, true, 4)
	if session.observed != 0 {
		t.Fatal("disconnect gap counted")
	}
	session.observe(start.Add(50*time.Second), "brave", rule.App, true, 4)
	if session.observed != 0 {
		t.Fatal("suspend or missed observations counted")
	}
	session.observe(start.Add(55*time.Second), "brave", rule.App, true, 5)
	if session.observed != 0 {
		t.Fatal("coalesced focus change counted")
	}
}

func TestElapsedSuppressionAndValidation(t *testing.T) {
	rule := ElapsedReminder{App: "brave", After: "10s", Message: "Check in"}
	s := elapsedSession{observed: 10 * time.Second}
	if !s.eligible(rule, false, false, true) {
		t.Fatal("due rule suppressed")
	}
	if s.eligible(rule, true, false, true) || s.eligible(rule, false, true, true) || s.eligible(rule, false, false, false) {
		t.Fatal("pause, quiet hours or cooldown ignored")
	}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	config := Config{Cooldown: "5m", PausedUntil: now.Add(time.Minute), QuietStart: "11:00", QuietEnd: "13:00", LastNotification: now.Add(-time.Minute)}
	if s.eligible(rule, now.Before(config.PausedUntil), inQuietHours(config, now), cooldownAllows(config, now)) {
		t.Fatal("configured suppression was ignored")
	}
	later := now.Add(2 * time.Hour)
	if !s.eligible(rule, later.Before(config.PausedUntil), inQuietHours(config, later), cooldownAllows(config, later)) {
		t.Fatal("rule did not become eligible after suppression ended")
	}
	rule.Disabled = true
	if s.eligible(rule, false, false, true) {
		t.Fatal("disabled rule eligible")
	}
	for _, bad := range []ElapsedReminder{{App: "", After: "10s", Message: "x"}, {App: "brave", After: "0s", Message: "x"}, {App: "brave", After: "10s", Repeat: "bad", Message: "x"}, {App: "brave", After: "10s", Message: " "}} {
		if err := validateConfig(Config{Cooldown: "5m", ElapsedReminders: []ElapsedReminder{bad}}); err == nil || !strings.Contains(err.Error(), "elapsed rule") {
			t.Fatalf("invalid rule accepted: %+v, %v", bad, err)
		}
	}
	var watch watchSession
	if !watchReminderDue(Reminder{Kind: "watch", App: "brave", Every: "1h"}, "brave", time.Now(), &watch) {
		t.Fatal("existing watch rule no longer fires immediately")
	}
}

func TestElapsedConfigurationAPIRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	h := handler()
	config := Config{Cooldown: "5m", ElapsedReminders: []ElapsedReminder{{App: "brave", After: "20m", Repeat: "10m", Message: "Check in"}}}
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "http://"+address+"/api/v1/config", bytes.NewReader(body))
	request.Host = address
	request.Header.Set("Origin", "http://"+address)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save elapsed configuration: %d %s", response.Code, response.Body)
	}
	saved, _, err := load()
	if err != nil || len(saved.ElapsedReminders) != 1 || saved.ElapsedReminders[0] != config.ElapsedReminders[0] {
		t.Fatalf("elapsed configuration not preserved: %+v, %v", saved.ElapsedReminders, err)
	}
}
