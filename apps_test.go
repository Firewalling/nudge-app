package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAppsEndpoint(t *testing.T) {
	withNiriResponse(t, `[{"app_id":"brave-browser","title":"private title"},{"app_id":"BRAVE-BROWSER"},{"app_id":"org.mozilla.firefox"},{"app_id":null}]`)
	r := httptest.NewRequest(http.MethodGet, "http://"+address+"/api/v1/apps", nil)
	w := httptest.NewRecorder()
	handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "private title") {
		t.Fatal("window title leaked")
	}
	var result appsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error != "" || len(result.Apps) != 2 || result.Apps[0] != (appChoice{ID: "brave-browser", Name: "Brave Browser"}) || result.Apps[1] != (appChoice{ID: "org.mozilla.firefox", Name: "Firefox"}) {
		t.Fatalf("choices: %+v", result)
	}
}
func TestOpenAppsUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	result := openApps()
	if result.Error == "" || result.Apps == nil {
		t.Fatalf("unavailable: %+v", result)
	}
}
