package bridge

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLoopbackAndConflict(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://"+r.Host {
			t.Error("incorrect origin")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"revision"`)
		if r.Method == http.MethodPut && r.Header.Get("If-Match") != `"revision"` {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"error":"conflict"}`)
			return
		}
		fmt.Fprint(w, `{"reminders":[]}`)
	}))
	defer s.Close()
	_, port, err := net.SplitHostPort(s.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(port)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Request("GET", "/api/v1/config", "", "")
	if err != nil || got.Status != 200 || got.ETag != `"revision"` {
		t.Fatalf("read: %+v, %v", got, err)
	}
	got, err = c.Request("PUT", "/api/v1/config", `{}`, `"stale"`)
	if err != nil || got.Status != 409 {
		t.Fatalf("conflict: %+v, %v", got, err)
	}
	if _, err = c.Request("PUT", "/api/v1/config", `{}`, ""); err == nil {
		t.Fatal("missing revision accepted")
	}
	if _, err = c.Request("GET", "//example.com/api/v1/config", "", ""); err == nil {
		t.Fatal("external URL accepted")
	}
	if _, err = New("0"); err == nil {
		t.Fatal("invalid port accepted")
	}
}
