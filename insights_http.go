package main

import (
	"context"
	"net/http"
	"time"
)

func registerInsights(mux *http.ServeMux, states ...*schedulerState) {
	mux.HandleFunc("/api/v1/insights", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			apiError(w, 405, "method not allowed")
			return
		}
		days := 7
		switch r.URL.Query().Get("days") {
		case "", "7":
		case "30":
			days = 30
		default:
			apiError(w, 400, "days must be 7 or 30")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		db, err := openInsights(ctx)
		if err != nil {
			jsonResponse(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer db.Close()
		result, err := collectInsights(ctx, db, days, time.Now())
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		if len(states) > 0 && states[0] != nil && states[0].insights != nil {
			result.Error = states[0].insights.errorString()
		}
		jsonResponse(w, 200, result)
	})
	mux.HandleFunc("/api/v1/insights/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			apiError(w, 405, "method not allowed")
			return
		}
		var input struct {
			Enabled       *bool `json:"enabled"`
			AppActivity   *bool `json:"app_activity"`
			RetentionDays *int  `json:"retention_days"`
		}
		if err := decodeJSON(w, r, &input); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		if input.Enabled == nil || input.AppActivity == nil || input.RetentionDays == nil {
			apiError(w, 400, "all settings are required")
			return
		}
		s := insightSettings{Enabled: *input.Enabled, AppActivity: *input.AppActivity, RetentionDays: *input.RetentionDays}
		if s.RetentionDays < 1 || s.RetentionDays > 3650 {
			apiError(w, 400, "retention_days must be between 1 and 3650")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		db, err := openInsights(ctx)
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		defer db.Close()
		if err = setInsightSettings(ctx, db, s); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		jsonResponse(w, 200, s)
	})
	mux.HandleFunc("/api/v1/insights/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			apiError(w, 405, "method not allowed")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		db, err := openInsights(ctx)
		if err != nil {
			apiError(w, 500, err.Error())
			return
		}
		defer db.Close()
		if err = clearInsights(ctx, db); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
}
func recordControl(states []*schedulerState, kind string) {
	e := insightEvent{at: time.Now(), kind: kind}
	if len(states) > 0 && states[0] != nil && states[0].insights != nil {
		states[0].insights.record(e)
		return
	}
	r := newInsightsRecorder()
	r.record(e)
	r.close()
}
