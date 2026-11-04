package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type Reminder struct {
	Kind    string    `json:"kind"`
	App     string    `json:"app,omitempty"`
	Every   string    `json:"every"`
	Message string    `json:"message"`
	Last    time.Time `json:"last,omitempty"`
}
type Config struct {
	Reminders        []Reminder `json:"reminders"`
	PausedUntil      time.Time  `json:"paused_until,omitempty"`
	Cooldown         string     `json:"cooldown,omitempty"`
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
		return Config{Cooldown: "5m"}, p, nil
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
	tmp := p + ".tmp"
	if e = os.WriteFile(tmp, append(b, '\n'), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, p)
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
	if e := exec.Command("notify-send", "Nudge", m).Run(); e != nil {
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
	b, err := exec.Command("niri", "msg", "--json", "focused-window").Output()
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
	sessions := map[int]watchSession{}
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for range t.C {
		c, _, err := load()
		if err != nil {
			continue
		}
		if time.Now().Before(c.PausedUntil) {
			sessions = map[int]watchSession{}
			continue
		}
		now := time.Now()
		app, _, _ := focusedWindow()
		allowed := cooldownAllows(c, now)
		changed := false
		for i := range c.Reminders {
			r := &c.Reminders[i]
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
			_ = save(c, p)
		}
	}
	return nil
}
func run(a []string) error {
	if len(a) == 0 {
		return errors.New("usage: nudge now MESSAGE | every DURATION MESSAGE | watch APP_ID --every DURATION | watch --focused --every DURATION | inspect | pause DURATION | resume | status | list | daemon")
	}
	if a[0] == "inspect" {
		if len(a) != 1 {
			return errors.New("usage: nudge inspect")
		}
		return inspect()
	}
	c, p, e := load()
	if e != nil {
		return e
	}
	switch a[0] {
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
	case "pause":
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
	case "daemon":
		return runDaemon()
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
