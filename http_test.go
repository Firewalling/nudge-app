package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIConfigControlAndSecurity(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := handler()
	request := func(method, path, host, origin string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://"+address+path, bytes.NewReader(body))
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	config := Config{Cooldown: "2m", Reminders: []Reminder{{Kind: "watch", App: "firefox", Every: "10m", Message: "Take a break"}}}
	body, _ := json.Marshal(config)
	if w := request(http.MethodPut, "/api/v1/config", "evil.example", "http://"+address, body); w.Code != 403 {
		t.Fatalf("bad host: %d", w.Code)
	}
	if w := request(http.MethodPut, "/api/v1/config", address, "http://evil.example", body); w.Code != 403 {
		t.Fatalf("bad origin: %d", w.Code)
	}
	if w := request(http.MethodPut, "/api/v1/config", address, "", body); w.Code != 403 {
		t.Fatalf("missing origin: %d", w.Code)
	}
	if w := request(http.MethodPut, "/api/v1/config", address, "http://"+address, body); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	stale := httptest.NewRequest(http.MethodPut, "http://"+address+"/api/v1/config", bytes.NewReader(body))
	stale.Host = address
	stale.Header.Set("Origin", "http://"+address)
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("If-Match", "\"stale\"")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, stale)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale write: %d %s", w.Code, w.Body)
	}
	config.Reminders[0].Every = "invalid"
	invalid, _ := json.Marshal(config)
	if w := request(http.MethodPut, "/api/v1/config", address, "http://"+address, invalid); w.Code != 400 {
		t.Fatalf("invalid config: %d", w.Code)
	}
	c, _, err := load()
	if err != nil || len(c.Reminders) != 1 || c.Reminders[0].Every != "10m" {
		t.Fatalf("persisted config: %+v %v", c, err)
	}
	if w := request(http.MethodPost, "/api/v1/control", address, "http://"+address, []byte(`{"action":"snooze","duration":"10m"}`)); w.Code != 200 {
		t.Fatalf("snooze: %d %s", w.Code, w.Body)
	}
	c, _, err = load()
	if err != nil || time.Until(c.PausedUntil) < 9*time.Minute {
		t.Fatalf("pause not persisted: %+v %v", c, err)
	}
	if w := request(http.MethodPost, "/api/v1/control", address, "http://"+address, []byte(`{"action":"resume"}`)); w.Code != 200 {
		t.Fatalf("resume: %d", w.Code)
	}
	c, _, err = load()
	if err != nil || !c.PausedUntil.IsZero() {
		t.Fatalf("resume not persisted: %+v %v", c, err)
	}
}
func TestQuietHours(t *testing.T) {
	c := Config{QuietStart: "22:00", QuietEnd: "07:00"}
	for _, tc := range []struct {
		hour int
		want bool
	}{{0, true}, {6, true}, {7, false}, {21, false}, {22, true}} {
		now := time.Date(2026, 1, 1, tc.hour, 0, 0, 0, time.Local)
		if got := inQuietHours(c, now); got != tc.want {
			t.Errorf("hour %d: got %v", tc.hour, got)
		}
	}
	c.QuietStart = "09:00"
	c.QuietEnd = "17:00"
	if !inQuietHours(c, time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)) {
		t.Fatal("daytime quiet hours")
	}
}
