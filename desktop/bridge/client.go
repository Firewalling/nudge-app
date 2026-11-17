// Package bridge provides the desktop application's loopback daemon client.
package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponse = 4 << 20

type Client struct {
	base string
	http *http.Client
}

type Response struct {
	Status int    `json:"status"`
	ETag   string `json:"etag"`
	Body   string `json:"body"`
}

// New connects only to the daemon's IPv4 loopback address.
func New(port string) (*Client, error) {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return nil, errors.New("invalid daemon port")
	}
	base := "http://" + net.JoinHostPort("127.0.0.1", port)
	return &Client{base: base, http: &http.Client{
		Timeout:       5 * time.Second,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Request exposes only the documented API routes and methods. Config writes
// require a revision so a desktop client cannot silently overwrite edits.
func (c *Client) Request(method, path, body, etag string) (Response, error) {
	u, err := url.ParseRequestURI(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return Response{}, errors.New("invalid API path")
	}
	allowed := false
	switch u.Path {
	case "/api/v1/status", "/api/v1/apps", "/api/v1/diagnostics", "/api/v1/insights":
		allowed = method == http.MethodGet
	case "/api/v1/config":
		allowed = method == http.MethodGet || (method == http.MethodPut && etag != "")
	case "/api/v1/control":
		allowed = method == http.MethodPost
	case "/api/v1/toast":
		allowed = method == http.MethodPost
	case "/api/v1/insights/settings":
		allowed = method == http.MethodPut
	case "/api/v1/insights/history":
		allowed = method == http.MethodDelete
	}
	if !allowed || (u.RawQuery != "" && u.Path != "/api/v1/insights") {
		return Response{}, errors.New("API operation not allowed")
	}
	if len(body) > 1<<20 {
		return Response{}, errors.New("request too large")
	}
	req, err := http.NewRequest(method, c.base+u.RequestURI(), strings.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Origin", c.base)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("daemon unavailable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return Response{}, err
	}
	if len(data) > maxResponse || !json.Valid(bytes.TrimSpace(data)) {
		return Response{}, errors.New("invalid daemon response")
	}
	return Response{Status: resp.StatusCode, ETag: resp.Header.Get("ETag"), Body: string(data)}, nil
}
