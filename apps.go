package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"
)

type appChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type appsResponse struct {
	Apps  []appChoice `json:"apps"`
	Error string      `json:"error"`
}

// openApps only exposes application IDs, not window titles or other window data.
func openApps() appsResponse {
	result := appsResponse{Apps: []appChoice{}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "niri", "msg", "--json", "windows").Output()
	if err != nil {
		result.Error = "Couldn't find open apps. You can enter an app ID instead."
		return result
	}
	var windows []struct {
		AppID *string `json:"app_id"`
	}
	if err = json.Unmarshal(raw, &windows); err != nil {
		result.Error = "Couldn't read the list of open apps."
		return result
	}
	seen := map[string]bool{}
	for _, window := range windows {
		if window.AppID == nil || validateAppID(*window.AppID) != nil {
			continue
		}
		id := *window.AppID
		key := strings.ToLower(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		result.Apps = append(result.Apps, appChoice{ID: id, Name: appName(id)})
	}
	sort.Slice(result.Apps, func(i, j int) bool {
		return strings.ToLower(result.Apps[i].Name) < strings.ToLower(result.Apps[j].Name)
	})
	return result
}
func appName(id string) string {
	parts := strings.Split(id, ".")
	name := parts[len(parts)-1]
	if strings.EqualFold(name, "desktop") && len(parts) > 1 {
		name = parts[len(parts)-2]
	}
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, word := range words {
		r := []rune(word)
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
	}
	if len(words) == 0 {
		return id
	}
	return strings.Join(words, " ")
}
