package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFocusedWindowParsingAndValidation(t *testing.T) {
	window, found, err := parseFocusedWindow([]byte(`{"app_id":"firefox","title":"Research"}`))
	if err != nil || !found || window != (focusedWindowInfo{AppID: "firefox", Title: "Research"}) {
		t.Fatalf("parse focused window = %+v, %v, %v", window, found, err)
	}
	if _, found, err = parseFocusedWindow([]byte("null")); err != nil || found {
		t.Fatalf("null focus = %v, %v", found, err)
	}
	if _, _, err = parseFocusedWindow([]byte("{")); err == nil {
		t.Fatal("malformed Niri JSON was accepted")
	}
	for _, app := range []string{"", "  ", "bad\napp"} {
		if validateAppID(app) == nil {
			t.Errorf("invalid app ID %q was accepted", app)
		}
	}
	if err = validateAppID("org.mozilla.firefox"); err != nil {
		t.Fatalf("valid app ID rejected: %v", err)
	}
}

func TestInspectPrintsFocusedAppIDAndTitle(t *testing.T) {
	withNiriResponse(t, `{"app_id":"Chatgpt","title":"A window"}`)
	old := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	runErr := run([]string{"inspect"})
	_ = write.Close()
	os.Stdout = old
	output, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	if runErr != nil || string(output) != "App ID: Chatgpt\nTitle: A window\n" {
		t.Fatalf("inspect = %q, %v", output, runErr)
	}
}

func TestWatchFocusedRegistersTheNiriAppID(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	withNiriResponse(t, `{"app_id":"Chatgpt","title":"A window"}`)
	if err := run([]string{"watch", "--focused", "--every", "15m", "Focus gently"}); err != nil {
		t.Fatal(err)
	}
	c, _, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Reminders) != 1 || c.Reminders[0].App != "Chatgpt" || c.Reminders[0].Every != "15m" || c.Reminders[0].Message != "Focus gently" {
		t.Fatalf("focused reminder not persisted correctly: %+v", c.Reminders)
	}
}

func TestWatchFocusedReportsNoFocusInvalidAppAndIPCFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	args := []string{"watch", "--focused", "--every", "15m"}
	withNiriResponse(t, "null")
	if err := run(args); err == nil || !strings.Contains(err.Error(), "no focused window") {
		t.Fatalf("no-focus error = %v", err)
	}
	withNiriResponse(t, `{"app_id":"bad\napp"}`)
	if err := run(args); err == nil || !strings.Contains(err.Error(), "invalid app ID") {
		t.Fatalf("invalid-app error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nudge", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid focus command changed config: %v", err)
	}
	missing := t.TempDir()
	t.Setenv("PATH", missing)
	if err := run(args); err == nil || !strings.Contains(err.Error(), "Niri IPC unavailable") {
		t.Fatalf("IPC failure = %v", err)
	}
}

func TestWatchManualAppIDRemainsSupported(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := run([]string{"watch", "firefox", "--every", "15m"}); err != nil {
		t.Fatal(err)
	}
	c, _, err := load()
	if err != nil || len(c.Reminders) != 1 || c.Reminders[0].App != "firefox" {
		t.Fatalf("manual watch failed: %+v, %v", c, err)
	}
}

func withNiriResponse(t *testing.T, response string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + response + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "niri"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDurationAndNiriFocusedWindow(t *testing.T) {
	for _, input := range []string{"0s", "-1m", "tomorrow"} {
		if _, err := duration(input); err == nil {
			t.Errorf("duration(%q) unexpectedly succeeded", input)
		}
	}
	if got, err := duration("30m"); err != nil || got != 30*time.Minute {
		t.Fatalf("duration(30m) = %v, %v", got, err)
	}
}

func TestWatchReminderStartsRepeatsAndResetsOnFocusLeave(t *testing.T) {
	r := Reminder{Kind: "watch", App: "firefox", Every: "15m"}
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	var session watchSession
	if !watchReminderDue(r, "firefox", now, &session) {
		t.Fatal("focus start should remind immediately")
	}
	session.lastSent = now
	if watchReminderDue(r, "firefox", now.Add(14*time.Minute), &session) {
		t.Fatal("reminded before interval elapsed")
	}
	if !watchReminderDue(r, "firefox", now.Add(15*time.Minute), &session) {
		t.Fatal("continuous focus should repeat at interval")
	}
	if watchReminderDue(r, "terminal", now.Add(16*time.Minute), &session) {
		t.Fatal("unfocused app should not remind")
	}
	if session != (watchSession{}) {
		t.Fatal("leaving app should reset its focus session")
	}
	if !watchReminderDue(r, "firefox", now.Add(17*time.Minute), &session) {
		t.Fatal("regaining focus should start a new immediate session")
	}
}

func TestCooldownAndCommandsPersistRemindersAndPause(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	if cooldownAllows(Config{Cooldown: "5m", LastNotification: now}, now.Add(4*time.Minute)) {
		t.Fatal("cooldown allowed a notification too early")
	}
	if !cooldownAllows(Config{Cooldown: "5m", LastNotification: now}, now.Add(5*time.Minute)) {
		t.Fatal("cooldown did not expire at its configured duration")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := run([]string{"every", "30m", "Are you being intentional?"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"watch", "firefox", "--every", "15m"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"pause", "1h"}); err != nil {
		t.Fatal(err)
	}
	c, p, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Reminders) != 2 || c.Reminders[0].Message != "Are you being intentional?" || c.Reminders[1].App != "firefox" || c.Reminders[1].Message != "Stay focused on what matters." {
		t.Fatalf("unexpected persisted reminders: %+v", c.Reminders)
	}
	if !time.Now().Before(c.PausedUntil) {
		t.Fatalf("pause did not persist: %s", c.PausedUntil)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	if filepath.Dir(p) != filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nudge") {
		t.Fatalf("unexpected config location %s", p)
	}
	if err = run([]string{"resume"}); err != nil {
		t.Fatal(err)
	}
	c, _, err = load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.PausedUntil.IsZero() {
		t.Fatalf("resume left pause active until %s", c.PausedUntil)
	}
}
