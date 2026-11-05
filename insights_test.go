package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestInsightsPrivacyAggregationAndClear(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := readInsightSettings(ctx, db)
	if err != nil || s.Enabled || s.AppActivity {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	now := time.Now()
	if err = insertInsight(ctx, db, insightEvent{at: now, kind: "attempt", app: "private.app"}); err != nil {
		t.Fatal(err)
	}
	if err = setInsightSettings(ctx, db, insightSettings{Enabled: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	now = time.Now().Add(time.Millisecond)
	for _, event := range []insightEvent{{at: now, kind: "attempt", app: "private.app"}, {at: now, kind: "failure", app: "private.app"}, {at: now, kind: "snooze"}} {
		if err = insertInsight(ctx, db, event); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM events WHERE app != ''").Scan(&count); err != nil || count != 0 {
		t.Fatalf("app IDs recorded when disabled: %d %v", count, err)
	}
	result, err := collectInsights(ctx, db, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Days) != 7 || len(result.Hours) != 24 || len(result.Apps) != 0 || result.Days[6].Attempts != 2 || result.Days[6].Snoozes != 1 {
		t.Fatalf("aggregation: %+v", result)
	}
	if err = setInsightSettings(ctx, db, insightSettings{RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	result, err = collectInsights(ctx, db, 7, now)
	if err != nil || result.Enabled || result.Days[6].Attempts != 2 {
		t.Fatalf("disabled history: %+v %v", result, err)
	}
	if err = clearInsights(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = insertInsight(ctx, db, insightEvent{at: now.Add(-time.Second), kind: "attempt"}); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale event survived clear: %d %v", count, err)
	}
	path, err := insightsPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
}

func TestInsightsTimezoneFocusAndRetention(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	old := time.Local
	time.Local = time.FixedZone("test", -7*3600)
	defer func() { time.Local = old }()
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = setInsightSettings(ctx, db, insightSettings{Enabled: true, AppActivity: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE settings SET changed_ns=0"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 4, 2, 1, 30, 0, 0, time.UTC)
	for _, e := range []insightEvent{
		{at: now, kind: "attempt", app: "editor"},
		{at: now, start: now.Add(-5 * time.Second), kind: "focus", app: "editor", seconds: 5},
		{at: now.AddDate(0, 0, -40), kind: "attempt", app: "old"},
	} {
		if err = insertInsight(ctx, db, e); err != nil {
			t.Fatal(err)
		}
	}
	result, err := collectInsights(ctx, db, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Days[6].Date != "2026-04-01" || result.Days[6].Attempts != 1 || result.Hours[18].Attempts != 1 || len(result.Apps) != 1 || result.Apps[0].ForegroundSeconds != 5 {
		t.Fatalf("local aggregation: %+v", result)
	}
	var beforePrune int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&beforePrune); err != nil || beforePrune != 3 {
		t.Fatalf("read unexpectedly pruned history: %d %v", beforePrune, err)
	}
	if err = pruneInsights(ctx, db, 30, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&count); err != nil || count != 2 {
		t.Fatalf("retention: %d %v", count, err)
	}
}

func TestInsightsRejectsUnsafeAppContext(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := setInsightSettings(ctx, db, insightSettings{Enabled: true, AppActivity: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(time.Millisecond)
	for _, event := range []insightEvent{
		{at: now, kind: "attempt", app: "https://private.example/path"},
		{at: now, start: now.Add(-time.Second), kind: "focus", app: "private document", seconds: 1},
	} {
		if err := insertInsight(ctx, db, event); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var app string
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(MAX(app), '') FROM events").Scan(&count, &app); err != nil || count != 1 || app != "" {
		t.Fatalf("unsafe context was stored: count=%d app=%q err=%v", count, app, err)
	}
}

func TestInsightsAPISecurityAndConcurrentSettings(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	h := handler()
	request := func(method, path, origin string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+address+path, bytes.NewReader(body))
		r.Host = address
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
	body := []byte(`{"enabled":true,"app_activity":false,"retention_days":30}`)
	if w := request(http.MethodPut, "/api/v1/insights/settings", "http://evil.example", body); w.Code != 403 {
		t.Fatalf("origin: %d", w.Code)
	}
	if w := request(http.MethodDelete, "/api/v1/insights/history", "", nil); w.Code != 403 {
		t.Fatalf("missing origin: %d", w.Code)
	}
	for _, invalid := range [][]byte{
		[]byte(`{"enabled":true,"app_activity":false,"retention_days":0}`),
		[]byte(`{"enabled":true,"retention_days":30}`),
	} {
		if w := request(http.MethodPut, "/api/v1/insights/settings", "http://"+address, invalid); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid settings accepted: %d", w.Code)
		}
	}
	if w := request(http.MethodGet, "/api/v1/insights?days=90", "", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid range accepted: %d", w.Code)
	}
	if w := request(http.MethodPut, "/api/v1/insights/settings", "http://"+address, body); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	var wg sync.WaitGroup
	results := make(chan int, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- request(http.MethodPut, "/api/v1/insights/settings", "http://"+address, body).Code
		}()
	}
	wg.Wait()
	close(results)
	for code := range results {
		if code != http.StatusOK {
			t.Errorf("concurrent settings status: %d", code)
		}
	}
	w := request(http.MethodGet, "/api/v1/insights?days=7", "", nil)
	var result insightResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Enabled || len(result.Days) != 7 || len(result.Hours) != 24 || result.Apps == nil {
		t.Fatalf("response: %d %s", w.Code, w.Body)
	}
}

func TestInsightsConcurrentReadersAndWriter(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = setInsightSettings(ctx, db, insightSettings{Enabled: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() {
		for range 40 {
			if err := insertInsight(ctx, db, insightEvent{at: time.Now(), kind: "attempt"}); err != nil {
				results <- err
				return
			}
		}
		results <- nil
	}()
	go func() {
		for range 40 {
			other, err := openInsights(ctx)
			if err != nil {
				results <- err
				return
			}
			_, err = collectInsights(ctx, other, 7, time.Now())
			other.Close()
			if err != nil {
				results <- err
				return
			}
		}
		results <- nil
	}()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&count); err != nil || count != 40 {
		t.Fatalf("recorded %d of 40 attempts: %v", count, err)
	}
}

func TestNotificationSurvivesHistoryFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	broken := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(broken, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", broken)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s' \"$2\" > \"$NUDGE_TEST_NOTIFICATION\"\n"
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "delivered-to-command")
	t.Setenv("NUDGE_TEST_NOTIFICATION", marker)
	t.Setenv("PATH", bin)
	if err := run([]string{"now", "test reminder"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "test reminder" {
		t.Fatalf("notification command did not run: %q %v", content, err)
	}
}

func TestFailedNotificationIsOneRecordedAttempt(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "notify-send"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := setInsightSettings(ctx, db, insightSettings{Enabled: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"now", "example"}); err == nil {
		t.Fatal("failed notification command succeeded")
	}
	db, err = openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var failures, total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(kind='failure'), 0) FROM events").Scan(&total, &failures); err != nil || total != 1 || failures != 1 {
		t.Fatalf("failure recording: total=%d failures=%d err=%v", total, failures, err)
	}
}

func TestInsightsMigrationAndRestart(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := setInsightSettings(ctx, db, insightSettings{Enabled: true, RetentionDays: 90}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("schema version=%d: %v", version, err)
	}
	if settings, err := readInsightSettings(ctx, db); err != nil || !settings.Enabled || settings.RetentionDays != 90 {
		t.Fatalf("settings not persisted: %+v %v", settings, err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if future, err := openInsights(ctx); err == nil {
		future.Close()
		t.Fatal("future schema was accepted")
	}
}

func TestHistorySettingsRollbackWhenPruningFails(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := time.Now().AddDate(0, 0, -60).UnixNano()
	if _, err := db.ExecContext(ctx, "INSERT INTO events(at_ns,kind) VALUES(?, 'attempt')", old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TRIGGER fail_prune BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT, 'prune failed'); END"); err != nil {
		t.Fatal(err)
	}
	if err := setInsightSettings(ctx, db, insightSettings{Enabled: true, RetentionDays: 30}); err == nil {
		t.Fatal("failed prune reported success")
	}
	settings, err := readInsightSettings(ctx, db)
	if err != nil || settings.Enabled {
		t.Fatalf("failed update changed privacy setting: %+v %v", settings, err)
	}
}

func TestInsightsAggregatesAcrossDaylightSaving(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = loc
	defer func() { time.Local = previous }()
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := setInsightSettings(ctx, db, insightSettings{Enabled: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE settings SET changed_ns=0"); err != nil {
		t.Fatal(err)
	}
	for _, hour := range []int{6, 7} {
		at := time.Date(2026, 3, 8, hour, 30, 0, 0, time.UTC)
		if err := insertInsight(ctx, db, insightEvent{at: at, kind: "attempt"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	result, err := collectInsights(ctx, db, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Days[5].Date != "2026-03-08" || result.Days[5].Attempts != 2 || result.Hours[1].Attempts != 1 || result.Hours[2].Attempts != 0 || result.Hours[3].Attempts != 1 {
		t.Fatalf("daylight-saving aggregation: %+v %+v", result.Days, result.Hours)
	}
}

func TestClearRemovesAppContextFromSQLiteFiles(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := setInsightSettings(ctx, db, insightSettings{Enabled: true, AppActivity: true, RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	if err := insertInsight(ctx, db, insightEvent{at: time.Now().Add(time.Millisecond), kind: "attempt", app: "sample.private.app"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE app = ?", "sample.private.app").Scan(&count); err != nil || count != 1 {
		t.Fatalf("fixture event missing: %d %v", count, err)
	}
	if err := clearInsights(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := insightsPath()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("sample.private.app")) {
			t.Fatalf("cleared app identifier remains in %s", filepath.Base(file))
		}
	}
}

func TestInsightsReadExcludesExpiredWithoutDeleting(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := context.Background()
	db, err := openInsights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.Local)
	if _, err := db.ExecContext(ctx, "UPDATE settings SET retention_days=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO events(at_ns,kind) VALUES(?, 'attempt'), (?, 'attempt')", now.Add(-48*time.Hour).UnixNano(), now.Add(-time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	result, err := collectInsights(ctx, db, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, day := range result.Days {
		total += day.Attempts
	}
	var stored int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events").Scan(&stored); err != nil || total != 1 || stored != 2 {
		t.Fatalf("expired data: visible=%d stored=%d err=%v", total, stored, err)
	}
}
