package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

//go:embed frontend/dist
var assets embed.FS

const version = "0.3.0"

var address = "127.0.0.1:8788"

type statusResponse struct {
	Active                bool      `json:"active"`
	PausedUntil           time.Time `json:"paused_until"`
	FocusedApp            string    `json:"focused_app"`
	ActiveRule            string    `json:"active_rule"`
	NextReminder          string    `json:"next_reminder"`
	NiriError             string    `json:"niri_error"`
	NotificationAvailable bool      `json:"notification_available"`
	SuppressionReason     string    `json:"suppression_reason"`
	NextDescription       string    `json:"next_description"`
	ActiveRules           []string  `json:"active_rules"`
	SchedulerReady        bool      `json:"scheduler_ready"`
	FocusKnown            bool      `json:"focus_known"`
	MatchingRulesKnown    bool      `json:"matching_rules_known"`
	SchedulerError        string    `json:"scheduler_error"`
	Version               string    `json:"version"`
}
type diagnosticsResponse struct {
	Version               string `json:"version"`
	NiriError             string `json:"niri_error"`
	NotificationAvailable bool   `json:"notification_available"`
	ConfigError           string `json:"config_error"`
}
type controlRequest struct {
	Action   string `json:"action"`
	Duration string `json:"duration"`
}

func configETag(c Config) string {
	c.PausedUntil = time.Time{}
	c.LastNotification = time.Time{}
	c.Reminders = append([]Reminder{}, c.Reminders...)
	for i := range c.Reminders {
		c.Reminders[i].Last = time.Time{}
	}
	b, _ := json.Marshal(c)
	return fmt.Sprintf("\"%x\"", sha256.Sum256(b))
}

func jsonResponse(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, code int, message string) {
	jsonResponse(w, code, map[string]string{"error": message})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("Content-Type must be application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
func status(c Config, states ...*schedulerState) statusResponse {
	now := time.Now()
	s := statusResponse{Active: !now.Before(c.PausedUntil) && !inQuietHours(c, now), PausedUntil: c.PausedUntil, Version: version, ActiveRules: []string{}}
	if now.Before(c.PausedUntil) {
		s.SuppressionReason = "paused"
	} else if inQuietHours(c, now) {
		s.SuppressionReason = "quiet_hours"
	}
	_, err := exec.LookPath("notify-send")
	s.NotificationAvailable = err == nil
	if len(states) != 0 && states[0] != nil {
		snapshot := states[0].get()
		s.SchedulerError = snapshot.schedulerError
		s.NiriError = snapshot.niriError
		s.FocusedApp = snapshot.focusedApp
		s.SchedulerReady = snapshot.revision != "" && snapshot.schedulerError == ""
		s.FocusKnown = s.SchedulerReady && snapshot.niriError == ""
		s.NextDescription = "Waiting for the scheduler to update."
		if snapshot.revision == configETag(c) && s.Active && !snapshot.suppressed && snapshot.schedulerError == "" {
			s.MatchingRulesKnown = s.FocusKnown
			s.ActiveRules = snapshot.activeRules
			if s.ActiveRules == nil {
				s.ActiveRules = []string{}
			}
			if len(s.ActiveRules) > 0 {
				s.ActiveRule = s.ActiveRules[0]
			}
			if !snapshot.next.IsZero() {
				s.NextReminder = snapshot.next.Format(time.RFC3339)
			}
			s.NextDescription = snapshot.description
		} else if !s.Active {
			s.NextDescription = "Scheduling is suppressed."
		} else if snapshot.schedulerError != "" {
			s.NextDescription = "Next reminder unavailable while the scheduler has an error."
		}
		return s
	}
	w, found, err := focusedWindow()
	if err != nil {
		s.NiriError = err.Error()
	} else if found {
		s.FocusedApp = w.AppID
		for _, r := range c.Reminders {
			if !r.Disabled && r.Kind == "watch" && strings.EqualFold(r.App, w.AppID) {
				s.ActiveRules = append(s.ActiveRules, r.Message)
				if s.ActiveRule == "" {
					s.ActiveRule = r.Message
				}
			}
		}
	}
	return s
}
func handler(states ...*schedulerState) http.Handler {
	mux := http.NewServeMux()
	registerInsights(mux, states...)
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			apiError(w, 405, "method not allowed")
			return
		}
		c, _, err := load()
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		jsonResponse(w, 200, status(c, states...))
	})
	mux.HandleFunc("/api/v1/apps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			apiError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		jsonResponse(w, http.StatusOK, openApps())
	})
	mux.HandleFunc("/api/v1/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			apiError(w, 405, "method not allowed")
			return
		}
		c, _, err := load()
		d := diagnosticsResponse{Version: version}
		if err != nil {
			d.ConfigError = err.Error()
		} else if err = validateConfig(c); err != nil {
			d.ConfigError = err.Error()
		}
		s := status(c, states...)
		d.NiriError = s.NiriError
		d.NotificationAvailable = s.NotificationAvailable
		jsonResponse(w, 200, d)
	})
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			c, _, err := load()
			if err != nil {
				apiError(w, 500, err.Error())
				return
			}
			w.Header().Set("ETag", configETag(c))
			jsonResponse(w, 200, c)
			return
		}
		if r.Method != "PUT" {
			apiError(w, 405, "method not allowed")
			return
		}
		var next Config
		if err := decodeJSON(w, r, &next); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if err := validateConfig(next); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		unlock, err := lockConfig()
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		defer unlock()
		old, p, err := load()
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		if tag := r.Header.Get("If-Match"); tag != "" && tag != configETag(old) {
			apiError(w, http.StatusConflict, "Configuration changed elsewhere. Copy any edits, then reload the page.")
			return
		}
		next.PausedUntil = old.PausedUntil
		next.LastNotification = old.LastNotification
		for i := range next.Reminders {
			next.Reminders[i].Last = time.Time{}
			for _, prev := range old.Reminders {
				if prev.Kind == next.Reminders[i].Kind && prev.App == next.Reminders[i].App && prev.Every == next.Reminders[i].Every && prev.Message == next.Reminders[i].Message {
					next.Reminders[i].Last = prev.Last
					break
				}
			}
		}
		if err = save(next, p); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		w.Header().Set("ETag", configETag(next))
		jsonResponse(w, 200, next)
	})
	mux.HandleFunc("/api/v1/control", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			apiError(w, 405, "method not allowed")
			return
		}
		var req controlRequest
		if err := decodeJSON(w, r, &req); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		var until time.Time
		switch req.Action {
		case "pause", "snooze":
			d, err := duration(req.Duration)
			if err != nil {
				apiError(w, 400, err.Error())
				return
			}
			until = time.Now().Add(d)
		case "resume":
		default:
			apiError(w, 400, "unknown action")
			return
		}
		unlock, err := lockConfig()
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		defer unlock()
		c, p, err := load()
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		c.PausedUntil = until
		if err = save(c, p); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		recordControl(states, req.Action)
		jsonResponse(w, 200, status(c, states...))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, http.StatusNotFound, "unknown endpoint")
	})
	sub, _ := fs.Sub(assets, "frontend/dist")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Host != address {
			apiError(w, 403, "invalid Host")
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+address {
			apiError(w, 403, "invalid Origin")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && origin != "http://"+address {
			apiError(w, 403, "same-origin request required")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func startHTTP(state *schedulerState) (func(), error) {
	l, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, fmt.Errorf("dashboard: %w", err)
	}
	server := &http.Server{Handler: handler(state), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(l); err != nil && err != http.ErrServerClosed {
			fmt.Printf("nudge: dashboard: %v\n", err)
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}, nil
}
