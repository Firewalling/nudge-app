package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// lockConfig serializes read-modify-write transactions across daemon and CLI processes.
func lockConfig() (func(), error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func validateConfig(c Config) error {
	if _, err := duration(c.Cooldown); err != nil {
		return fmt.Errorf("cooldown: %w", err)
	}
	if (c.QuietStart == "") != (c.QuietEnd == "") {
		return fmt.Errorf("quiet hours require both start and end")
	}
	if c.QuietStart != "" {
		for _, s := range []string{c.QuietStart, c.QuietEnd} {
			if _, err := time.Parse("15:04", s); err != nil {
				return fmt.Errorf("quiet hours must use HH:MM")
			}
		}
		if c.QuietStart == c.QuietEnd {
			return fmt.Errorf("quiet hours start and end must differ")
		}
	}
	if len(c.Reminders) > 500 {
		return fmt.Errorf("too many rules")
	}
	for i, r := range c.Reminders {
		if r.Kind != "watch" && r.Kind != "every" {
			return fmt.Errorf("rule %d: invalid kind", i+1)
		}
		if r.Kind == "watch" {
			if err := validateAppID(r.App); err != nil {
				return fmt.Errorf("rule %d: %w", i+1, err)
			}
		}
		if _, err := duration(r.Every); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
		if strings.TrimSpace(r.Message) == "" || len(r.Message) > 4096 {
			return fmt.Errorf("rule %d: message must be 1–4096 bytes", i+1)
		}
	}
	return nil
}

func inQuietHours(c Config, now time.Time) bool {
	if c.QuietStart == "" || c.QuietEnd == "" {
		return false
	}
	current := now.Format("15:04")
	if c.QuietStart < c.QuietEnd {
		return current >= c.QuietStart && current < c.QuietEnd
	}
	return current >= c.QuietStart || current < c.QuietEnd
}
