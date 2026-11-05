package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

type insightSettings struct {
	Enabled       bool `json:"enabled"`
	AppActivity   bool `json:"app_activity"`
	RetentionDays int  `json:"retention_days"`
}
type insightDay struct {
	Date     string `json:"date"`
	Attempts int    `json:"attempts"`
	Snoozes  int    `json:"snoozes"`
}
type insightHour struct {
	Hour     int `json:"hour"`
	Attempts int `json:"attempts"`
}
type insightApp struct {
	App               string `json:"app"`
	Attempts          int    `json:"attempts"`
	ForegroundSeconds int    `json:"foreground_seconds"`
}
type insightResponse struct {
	insightSettings
	Days  []insightDay  `json:"days"`
	Hours []insightHour `json:"hours"`
	Apps  []insightApp  `json:"apps"`
	Error string        `json:"error"`
}
type insightEvent struct {
	at, start time.Time
	kind, app string
	seconds   int
}

func insightsPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("XDG_DATA_HOME must be absolute")
	}
	return filepath.Join(base, "nudge", "history.db"), nil
}

func openInsights(ctx context.Context) (*sql.DB, error) {
	path, err := insightsPath()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Closing an unrelated descriptor for an open SQLite database can release
	// POSIX locks held by other connections in this process. Open only on creation.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		if err = f.Close(); err != nil {
			return nil, err
		}
	} else if !os.IsExist(err) {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*sql.DB, error) { db.Close(); return nil, e }
	for _, q := range []string{"PRAGMA busy_timeout=100", "PRAGMA journal_mode=WAL", "PRAGMA secure_delete=ON"} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			return fail(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	var version int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		tx.Rollback()
		return fail(err)
	}
	if version > 1 {
		tx.Rollback()
		return fail(fmt.Errorf("unsupported insights schema version %d", version))
	}
	if version == 0 {
		for _, q := range []string{
			"CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL, app_activity INTEGER NOT NULL, retention_days INTEGER NOT NULL, changed_ns INTEGER NOT NULL, cleared_ns INTEGER NOT NULL)",
			"INSERT INTO settings VALUES(1,0,0,30,0,0)",
			"CREATE TABLE events (at_ns INTEGER NOT NULL, kind TEXT NOT NULL, app TEXT NOT NULL DEFAULT '', seconds INTEGER NOT NULL DEFAULT 0)",
			"CREATE INDEX events_at ON events(at_ns)", "PRAGMA user_version=1",
		} {
			if _, err = tx.ExecContext(ctx, q); err != nil {
				tx.Rollback()
				return fail(err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return db, nil
}
func readInsightSettings(ctx context.Context, db *sql.DB) (insightSettings, error) {
	var s insightSettings
	err := db.QueryRowContext(ctx, "SELECT enabled, app_activity, retention_days FROM settings WHERE id=1").Scan(&s.Enabled, &s.AppActivity, &s.RetentionDays)
	return s, err
}
func setInsightSettings(ctx context.Context, db *sql.DB, s insightSettings) error {
	if s.RetentionDays < 1 || s.RetentionDays > 3650 {
		return fmt.Errorf("retention_days must be between 1 and 3650")
	}
	now := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET enabled=?, app_activity=?, retention_days=?, changed_ns=? WHERE id=1", s.Enabled, s.AppActivity, s.RetentionDays, now.UnixNano()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM events WHERE at_ns < ?", now.AddDate(0, 0, -s.RetentionDays).UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}
func pruneInsights(ctx context.Context, db *sql.DB, days int, now time.Time) error {
	_, err := db.ExecContext(ctx, "DELETE FROM events WHERE at_ns < ?", now.AddDate(0, 0, -days).UnixNano())
	return err
}

// Application IDs are untrusted client strings. Do not persist values that
// could contain URLs, paths, titles or other arbitrary window content.
func safeInsightApp(id string) bool {
	if len(id) == 0 || len(id) > 255 {
		return false
	}
	hasLetterOrDigit := false
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasLetterOrDigit = true
		} else if r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return hasLetterOrDigit
}

func insertInsight(ctx context.Context, db *sql.DB, e insightEvent) error {
	if !safeInsightApp(e.app) {
		if e.kind == "focus" {
			return nil
		}
		e.app = ""
	}
	// The settings row gates writes atomically, including queued events predating a change or clear.
	_, err := db.ExecContext(ctx, `INSERT INTO events(at_ns,kind,app,seconds)
	 SELECT ?, ?, CASE WHEN app_activity THEN ? ELSE '' END, CASE WHEN app_activity THEN ? ELSE 0 END
	 FROM settings WHERE id=1 AND enabled AND ? >= changed_ns AND ? > cleared_ns AND (? != 'focus' OR (app_activity AND ? >= changed_ns AND ? > cleared_ns))`,
		e.at.UnixNano(), e.kind, e.app, e.seconds, e.at.UnixNano(), e.at.UnixNano(), e.kind, e.start.UnixNano(), e.start.UnixNano())
	return err
}
func clearInsights(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET cleared_ns=? WHERE id=1", time.Now().UnixNano()); err == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM events")
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	checkpoint := func() error {
		var busy, pages, checkpointed int
		if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil {
			return err
		}
		if busy != 0 {
			return fmt.Errorf("history checkpoint busy")
		}
		return nil
	}
	if err = checkpoint(); err != nil {
		return fmt.Errorf("history rows deleted, but database cleanup failed: %w", err)
	}
	if _, err = db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("history rows deleted, but database cleanup failed: %w", err)
	}
	if err = checkpoint(); err != nil {
		return fmt.Errorf("history rows deleted, but database cleanup failed: %w", err)
	}
	return nil
}
func collectInsights(ctx context.Context, db *sql.DB, days int, now time.Time) (insightResponse, error) {
	var result insightResponse
	s, err := readInsightSettings(ctx, db)
	if err != nil {
		return result, err
	}
	result.insightSettings = s
	result.Days = make([]insightDay, days)
	result.Hours = make([]insightHour, 24)
	result.Apps = []insightApp{}
	local := now.In(time.Local)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, 1-days)
	indexes := make(map[string]int, days)
	for i := range result.Days {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		result.Days[i].Date = date
		indexes[date] = i
	}
	for i := range result.Hours {
		result.Hours[i].Hour = i
	}
	// Retention pruning runs in the recorder; this read excludes expired rows
	// without deleting data as a side effect of GET.
	from := start
	if cutoff := now.AddDate(0, 0, -s.RetentionDays); cutoff.After(from) {
		from = cutoff
	}
	end := today.AddDate(0, 0, 1)
	rows, err := db.QueryContext(ctx, "SELECT at_ns, kind FROM events WHERE at_ns >= ? AND at_ns < ? AND kind IN ('attempt', 'failure', 'snooze')", from.UnixNano(), end.UnixNano())
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var ns int64
		var kind string
		if err = rows.Scan(&ns, &kind); err != nil {
			rows.Close()
			return result, err
		}
		at := time.Unix(0, ns).In(time.Local)
		i, ok := indexes[at.Format("2006-01-02")]
		if !ok {
			continue
		}
		if kind == "attempt" || kind == "failure" {
			result.Days[i].Attempts++
			result.Hours[at.Hour()].Attempts++
		} else {
			result.Days[i].Snoozes++
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	rows, err = db.QueryContext(ctx, `SELECT app,
		SUM(CASE WHEN kind IN ('attempt', 'failure') THEN 1 ELSE 0 END),
		SUM(seconds)
		FROM events WHERE at_ns >= ? AND at_ns < ? AND app != ''
		GROUP BY app ORDER BY app`, from.UnixNano(), end.UnixNano())
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var app insightApp
		if err = rows.Scan(&app.App, &app.Attempts, &app.ForegroundSeconds); err != nil {
			return result, err
		}
		result.Apps = append(result.Apps, app)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	return result, nil
}

type insightsRecorder struct {
	queue  chan insightEvent
	done   chan struct{}
	mu     sync.Mutex
	err    string
	closed bool
}

func newInsightsRecorder() *insightsRecorder {
	r := &insightsRecorder{queue: make(chan insightEvent, 256), done: make(chan struct{})}
	go r.run()
	return r
}
func (r *insightsRecorder) report(err error) {
	if err != nil {
		r.mu.Lock()
		r.err = err.Error()
		r.mu.Unlock()
		fmt.Fprintf(os.Stderr, "nudge: insights: %v\n", err)
	}
}
func (r *insightsRecorder) errorString() string { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *insightsRecorder) record(e insightEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	select {
	case r.queue <- e:
	default:
		r.err = "history queue full; event dropped"
	}
}
func (r *insightsRecorder) run() {
	defer close(r.done)
	var db *sql.DB
	var lastPrune time.Time
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		var e insightEvent
		var event bool
		select {
		case next, ok := <-r.queue:
			if !ok {
				return
			}
			e, event = next, true
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		var err error
		if db == nil {
			db, err = openInsights(ctx)
		}
		if err == nil && time.Since(lastPrune) >= time.Hour {
			var s insightSettings
			s, err = readInsightSettings(ctx, db)
			if err == nil {
				err = pruneInsights(ctx, db, s.RetentionDays, time.Now())
				lastPrune = time.Now()
			}
		}
		if err == nil && event {
			err = insertInsight(ctx, db, e)
		}
		cancel()
		r.report(err)
	}
}
func (r *insightsRecorder) close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	select {
	case <-r.done:
	case <-time.After(time.Second):
	}
}
