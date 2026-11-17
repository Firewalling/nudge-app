package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

var errToastBusy = errors.New("an in-app reminder is awaiting a response")

// toastHub offers at most one reminder to a focused desktop client. If no
// client claims an offer in time, the scheduler delivers it natively instead.
type toastHub struct {
	mu          sync.Mutex
	activeUntil time.Time
	nextID      uint64
	current     *toastOffer
}

type toastOffer struct {
	ID       uint64 `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Accepted bool   `json:"accepted"`
	claimed  chan struct{}
	native   string
	send     func(string) error
}

type toastView struct {
	ID       uint64 `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Accepted bool   `json:"accepted"`
}

func (h *toastHub) poll(now time.Time) *toastView {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeUntil = now.Add(2 * time.Second)
	if h.current == nil {
		return nil
	}
	return &toastView{ID: h.current.ID, Title: h.current.Title, Body: h.current.Body, Accepted: h.current.Accepted}
}

// expire transfers an abandoned accepted reminder to native delivery once.
func (h *toastHub) expire(now time.Time) error {
	h.mu.Lock()
	offer := h.current
	if offer == nil || !offer.Accepted || now.Before(h.activeUntil) {
		h.mu.Unlock()
		return nil
	}
	h.current = nil
	h.mu.Unlock()
	return offer.send(offer.native)
}

func (h *toastHub) claim(id uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current == nil || h.current.ID != id || h.current.Accepted || !time.Now().Before(h.activeUntil) {
		return false
	}
	h.current.Accepted = true
	close(h.current.claimed)
	return true
}

func (h *toastHub) dismiss(id uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current == nil || h.current.ID != id || !h.current.Accepted {
		return false
	}
	h.current = nil
	return true
}

func (h *toastHub) deliver(foreground bool, title, body, native string, wait time.Duration, send func(string) error) error {
	if foreground {
		h.mu.Lock()
		busy := h.current != nil && h.current.Accepted && time.Now().Before(h.activeUntil)
		h.mu.Unlock()
		if busy {
			return errToastBusy
		}
	}
	if foreground && h.offer(title, body, native, wait, send) {
		return nil
	}
	return send(native)
}

func (h *toastHub) offer(title, body, native string, wait time.Duration, send func(string) error) bool {
	h.mu.Lock()
	if h.current != nil || !time.Now().Before(h.activeUntil) {
		h.mu.Unlock()
		return false
	}
	if h.nextID == 0 {
		var seed [8]byte
		if _, err := rand.Read(seed[:]); err != nil {
			h.mu.Unlock()
			return false
		}
		// Keep IDs exactly representable in JavaScript, including after increment.
		h.nextID = binary.LittleEndian.Uint64(seed[:]) & ((1 << 52) - 1)
	}
	h.nextID++
	offer := &toastOffer{ID: h.nextID, Title: title, Body: body, claimed: make(chan struct{}), native: native, send: send}
	h.current = offer
	h.mu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-offer.claimed:
		return true
	case <-timer.C:
		h.mu.Lock()
		defer h.mu.Unlock()
		if offer.Accepted {
			return true
		}
		if h.current == offer {
			h.current = nil
		}
		return false
	}
}
