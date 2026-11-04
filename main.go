package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type Reminder struct {
	Kind     string    `json:"kind"`
	App      string    `json:"app,omitempty"`
	Every    string    `json:"every"`
	Message  string    `json:"message"`
	Disabled bool      `json:"disabled,omitempty"`
	Last     time.Time `json:"last,omitempty"`
}
type Config struct {
	Reminders        []Reminder `json:"reminders"`
	PausedUntil      time.Time  `json:"paused_until,omitempty"`
	Cooldown         string     `json:"cooldown,omitempty"`
	QuietStart       string     `json:"quiet_start,omitempty"`
	QuietEnd         string     `json:"quiet_end,omitempty"`
	LastNotification time.Time  `json:"last_notification,omitempty"`
}
type focusedWindowInfo struct {
	AppID string `json:"app_id"`
	Title string `json:"title"`
}
type watchSession struct{ started, lastSent time.Time }

func configPath() (string, error) {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		h, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		d = filepath.Join(h, ".config")
	}
	return filepath.Join(d, "nudge", "config.json"), nil
}
func load() (Config, string, error) {
	p, e := configPath()
	if e != nil {
		return Config{}, "", e
	}
	b, e := os.ReadFile(p)
	if errors.Is(e, os.ErrNotExist) {
		return Config{Cooldown: "5m", Reminders: []Reminder{}}, p, nil
	}
	if e != nil {
		return Config{}, "", e
	}
	var c Config
	if e = json.Unmarshal(b, &c); e != nil {
		return c, "", fmt.Errorf("read %s: %w", p, e)
	}
	if c.Cooldown == "" {
		c.Cooldown = "5m"
	}
	if c.Reminders == nil {
		c.Reminders = []Reminder{}
	}
	return c, p, nil
}
func save(c Config, p string) error {
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".config-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(append(b, '\n')); e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(f.Name(), p); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(p))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
func duration(s string) (time.Duration, error) {
	d, e := time.ParseDuration(s)
	if e != nil || d <= 0 {
		return 0, fmt.Errorf("invalid positive duration %q (e.g. 30m, 1h)", s)
	}
	return d, nil
}
func cooldownAllows(c Config, now time.Time) bool {
	if c.LastNotification.IsZero() {
		return true
	}
	d, e := duration(c.Cooldown)
	if e != nil {
		d = 5 * time.Minute
	}
	return now.Sub(c.LastNotification) >= d
}
func watchReminderDue(r Reminder, app string, now time.Time, s *watchSession) bool {
	if r.Kind != "watch" || !strings.EqualFold(r.App, app) {
		*s = watchSession{}
		return false
	}
	if s.started.IsZero() {
		s.started = now
		return true
	}
	if s.lastSent.IsZero() {
		return true
	}
	d, e := duration(r.Every)
	return e == nil && now.Sub(s.lastSent) >= d
}
func notify(m string) error {
	if _, e := exec.LookPath("notify-send"); e != nil {
		return fmt.Errorf("notify-send is required: %w", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := exec.CommandContext(ctx, "notify-send", "Nudge", m).Run(); e != nil {
		return fmt.Errorf("send desktop notification: %w", e)
	}
	return nil
}
func parseFocusedWindow(raw []byte) (focusedWindowInfo, bool, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return focusedWindowInfo{}, false, nil
	}
	var w focusedWindowInfo
	if err := json.Unmarshal(raw, &w); err != nil {
		return focusedWindowInfo{}, false, err
	}
	return w, true, nil
}
func validateAppID(id string) error {
	if id == "" || id != strings.TrimSpace(id) || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid app ID %q: expected a non-empty Niri app_id without control characters", id)
	}
	return nil
}
func focusedWindow() (focusedWindowInfo, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "niri", "msg", "--json", "focused-window").Output()
	if err != nil {
		return focusedWindowInfo{}, false, fmt.Errorf("Niri IPC unavailable (niri msg --json focused-window): %w", err)
	}
	w, found, err := parseFocusedWindow(b)
	if err != nil {
		return focusedWindowInfo{}, false, fmt.Errorf("invalid Niri focused-window response: %w", err)
	}
	return w, found, nil
}
func inspect() error {
	w, found, err := focusedWindow()
	if err != nil {
		return err
	}
	if !found {
		fmt.Println("No focused window.")
		return nil
	}
	app := w.AppID
	if app == "" {
		app = "(app_id unavailable)"
	}
	fmt.Printf("App ID: %s\nTitle: %s\n", app, w.Title)
	return nil
}
func runDaemon() error {
	_, p, e := load()
	if e != nil {
		return e
	}
	stopHTTP, err := startHTTP()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nudge: dashboard unavailable: %v\n", err)
	} else {
		defer stopHTTP()
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wake := make(chan struct{}, 1)
	go watchNiri(ctx, wake)
	sessions := map[int]watchSession{}
	var previousRules string
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-t.C:
		case <-wake:
		}
		unlock, err := lockConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "nudge: lock config: %v\n", err)
			continue
		}
		c, _, err := load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "nudge: load config: %v\n", err)
			unlock()
			continue
		}
		if time.Now().Before(c.PausedUntil) || inQuietHours(c, time.Now()) {
			sessions = map[int]watchSession{}
			unlock()
			continue
		}
		rules := make([]Reminder, len(c.Reminders))
		copy(rules, c.Reminders)
		for i := range rules {
			rules[i].Last = time.Time{}
		}
		rulesJSON, _ := json.Marshal(rules)
		if string(rulesJSON) != previousRules {
			sessions = map[int]watchSession{}
			previousRules = string(rulesJSON)
		}
		now := time.Now()
		app, _, _ := focusedWindow()
		allowed := cooldownAllows(c, now)
		changed := false
		for i := range c.Reminders {
			r := &c.Reminders[i]
			if r.Disabled {
				delete(sessions, i)
				continue
			}
			due := false
			if r.Kind == "watch" {
				s := sessions[i]
				due = watchReminderDue(*r, app.AppID, now, &s)
				sessions[i] = s
			} else if r.Kind == "every" {
				d, err := duration(r.Every)
				due = err == nil && (r.Last.IsZero() || now.Sub(r.Last) >= d)
			}
			if !due || !allowed {
				continue
			}
			if err := notify(r.Message); err != nil {
				fmt.Fprintf(os.Stderr, "nudge: %v\n", err)
				continue
			}
			if r.Kind == "every" {
				r.Last = now
			}
			if r.Kind == "watch" {
				s := sessions[i]
				s.lastSent = now
				sessions[i] = s
			}
			c.LastNotification = now
			allowed = false
			changed = true
		}
		if changed {
			if err := save(c, p); err != nil {
				fmt.Fprintf(os.Stderr, "nudge: save config: %v\n", err)
			}
		}
		unlock()
	}
}
func run(a []string) error {
	if len(a) == 0 {
		return errors.New("usage: nudge now MESSAGE | every DURATION MESSAGE | watch APP_ID --every DURATION | watch --focused --every DURATION | inspect | pause DURATION | resume | status | list | daemon")
	}
	if a[0] == "--version" || a[0] == "version" {
		fmt.Println("nudge " + version)
		return nil
	}
	if a[0] == "help" || a[0] == "--help" {
		fmt.Println("nudge now MESSAGE | every DURATION MESSAGE | watch APP_ID --every DURATION | watch --focused --every DURATION | inspect | pause DURATION | snooze DURATION | resume | status | list | daemon | version")
		return nil
	}
	if a[0] == "inspect" {
		if len(a) != 1 {
			return errors.New("usage: nudge inspect")
		}
		return inspect()
	}
	if a[0] == "daemon" {
		return runDaemon()
	}
	unlock, err := lockConfig()
	if err != nil {
		return err
	}
	defer unlock()
	c, p, e := load()
	if e != nil {
		return e
	}
	switch a[0] {
	case "config":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(c)
	case "diagnostics":
		s := status(c)
		configError := ""
		if err := validateConfig(c); err != nil {
			configError = err.Error()
		}
		return json.NewEncoder(os.Stdout).Encode(diagnosticsResponse{Version: version, NiriError: s.NiriError, NotificationAvailable: s.NotificationAvailable, ConfigError: configError})
	case "web":
		fmt.Println("http://" + address)
		return nil
	case "now":
		if len(a) < 2 {
			return errors.New("usage: nudge now MESSAGE")
		}
		return notify(strings.Join(a[1:], " "))
	case "every":
		if len(a) < 3 {
			return errors.New("usage: nudge every DURATION MESSAGE")
		}
		if _, e = duration(a[1]); e != nil {
			return e
		}
		c.Reminders = append(c.Reminders, Reminder{Kind: "every", Every: a[1], Message: strings.Join(a[2:], " ")})
		return save(c, p)
	case "watch":
		focused := len(a) > 1 && a[1] == "--focused"
		if focused {
			if len(a) < 4 || a[2] != "--every" {
				return errors.New("usage: nudge watch --focused --every DURATION [MESSAGE]")
			}
		} else if len(a) < 4 || a[2] != "--every" {
			return errors.New("usage: nudge watch APP_ID --every DURATION [MESSAGE]")
		}
		interval := 3
		app := a[1]
		if focused {
			w, found, err := focusedWindow()
			if err != nil {
				return err
			}
			if !found {
				return errors.New("no focused window; focus an app and retry")
			}
			app = w.AppID
			interval = 3
		}
		if err := validateAppID(app); err != nil {
			return err
		}
		every := a[interval]
		if _, e = duration(every); e != nil {
			return e
		}
		messageIndex := interval + 1
		if focused {
			messageIndex = 4
		}
		message := strings.Join(a[messageIndex:], " ")
		if message == "" {
			message = "Stay focused on what matters."
		}
		c.Reminders = append(c.Reminders, Reminder{Kind: "watch", App: app, Every: every, Message: message})
		return save(c, p)
	case "pause", "snooze":
		if len(a) != 2 {
			return errors.New("usage: nudge pause DURATION")
		}
		d, err := duration(a[1])
		if err != nil {
			return err
		}
		c.PausedUntil = time.Now().Add(d)
		return save(c, p)
	case "resume":
		c.PausedUntil = time.Time{}
		return save(c, p)
	case "status":
		if time.Now().Before(c.PausedUntil) {
			fmt.Printf("paused until %s\n", c.PausedUntil.Format(time.RFC3339))
		} else {
			fmt.Println("active")
		}
		fmt.Printf("%d reminders; config %s\n", len(c.Reminders), p)
		return nil
	case "list":
		for i, r := range c.Reminders {
			fmt.Printf("%d. %s", i+1, r.Kind)
			if r.App != "" {
				fmt.Printf(" %s", r.App)
			}
			fmt.Printf(" every %s — %s\n", r.Every, r.Message)
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q", a[0])
	}
}
func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "nudge:", e)
		os.Exit(1)
	}
}
