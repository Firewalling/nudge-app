package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToastDeliveryClaimAndNativeFallback(t *testing.T) {
	var hub toastHub
	native := make(chan string, 2)
	send := func(message string) error { native <- message; return nil }
	if err := hub.deliver(false, "Title", "Body", "Native", time.Second, send); err != nil {
		t.Fatal(err)
	}
	if got := <-native; got != "Native" {
		t.Fatalf("background notification = %q", got)
	}

	hub.poll(time.Now())
	done := make(chan error, 1)
	go func() { done <- hub.deliver(true, "20 minutes in Brave", "Check in", "Native", time.Second, send) }()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	var offer *toastView
	for offer == nil {
		select {
		case <-deadline:
			t.Fatal("foreground offer not available")
		case <-tick.C:
			offer = hub.poll(time.Now())
		}
	}
	if offer.Title != "20 minutes in Brave" || offer.Body != "Check in" || offer.Accepted {
		t.Fatalf("unexpected offer: %+v", offer)
	}
	if !hub.claim(offer.ID) || hub.claim(offer.ID) {
		t.Fatal("claim must succeed exactly once")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-native:
		t.Fatalf("claimed reminder also sent natively: %q", got)
	default:
	}
	if current := hub.poll(time.Now()); current == nil || !current.Accepted || current.ID != offer.ID {
		t.Fatalf("claimed reminder did not persist: %+v", current)
	}
	if err := hub.deliver(true, "Another", "Body", "Should not stack", time.Second, send); !errors.Is(err, errToastBusy) {
		t.Fatalf("second foreground reminder was not deferred: %v", err)
	}
	if err := hub.deliver(false, "Another", "Body", "Background", time.Second, send); err != nil {
		t.Fatal(err)
	}
	if got := <-native; got != "Background" {
		t.Fatalf("background fallback = %q", got)
	}
	if !hub.dismiss(offer.ID) || hub.dismiss(offer.ID) || hub.poll(time.Now()) != nil {
		t.Fatal("dismiss did not remove the claimed reminder exactly once")
	}

	if err := hub.deliver(true, "Title", "Body", "Fallback", 10*time.Millisecond, send); err != nil {
		t.Fatal(err)
	}
	if got := <-native; got != "Fallback" {
		t.Fatalf("unclaimed notification = %q", got)
	}
	if hub.claim(offer.ID+1) || hub.poll(time.Now()) != nil {
		t.Fatal("expired offer could still be claimed")
	}
}

func TestToastEndpointRequiresSameOrigin(t *testing.T) {
	h := handler(&schedulerState{})
	for _, origin := range []string{"", "http://attacker.example"} {
		request := httptest.NewRequest(http.MethodPost, "http://"+address+"/api/v1/toast", bytes.NewBufferString(`{"action":"poll"}`))
		request.Host = address
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("origin %q: got %d", origin, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://"+address+"/api/v1/toast", bytes.NewBufferString(`{"action":"poll"}`))
	request.Host = address
	request.Header.Set("Origin", "http://"+address)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	var body struct {
		Toast *toastView `json:"toast"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Toast != nil {
		t.Fatalf("valid empty poll: %d %s", response.Code, response.Body)
	}
}

func TestToastClaimRequiresFocusedDesktop(t *testing.T) {
	dir := t.TempDir()
	niri := filepath.Join(dir, "niri")
	t.Setenv("PATH", dir)
	setFocus := func(app string) {
		t.Helper()
		if err := os.WriteFile(niri, []byte("#!/bin/sh\nprintf '%s' '{\"app_id\":\""+app+"\"}'\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	state := &schedulerState{}
	state.toasts.current = &toastOffer{ID: 1, claimed: make(chan struct{})}
	h := handler(state)
	claim := func() int {
		request := httptest.NewRequest(http.MethodPost, "http://"+address+"/api/v1/toast", bytes.NewBufferString(`{"action":"claim","id":1}`))
		request.Host = address
		request.Header.Set("Origin", "http://"+address)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		return response.Code
	}
	setFocus("other-app")
	if got := claim(); got != http.StatusConflict || state.toasts.current.Accepted {
		t.Fatalf("background claim: status %d", got)
	}
	setFocus("org.wails.nudge")
	if got := claim(); got != http.StatusOK || !state.toasts.current.Accepted {
		t.Fatalf("foreground claim: status %d", got)
	}
}
