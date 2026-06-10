//go:build integration

// Package integration exercises the running stack over plain HTTP through the
// gateway. The same suite runs against any deployment of this system: locally
// (make test-integration, API_BASE_URL=http://localhost:8080) or against the
// real ALB later (API_BASE_URL=http://<alb-dns>) — no code changes.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"testing"
	"time"
)

func baseURL() string {
	if u := os.Getenv("API_BASE_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

type client struct {
	t     *testing.T
	token string
}

type response struct {
	status int
	body   map[string]interface{}
}

func (c *client) do(method, path string, payload interface{}) response {
	c.t.Helper()

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			c.t.Fatalf("marshal request: %v", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, baseURL()+path, body)
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read response: %v", err)
	}

	out := map[string]interface{}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			c.t.Fatalf("%s %s: non-JSON response (status %d): %s", method, path, resp.StatusCode, raw)
		}
	}
	return response{status: resp.StatusCode, body: out}
}

func (r response) mustStatus(t *testing.T, want int) response {
	t.Helper()
	if r.status != want {
		t.Fatalf("expected status %d, got %d: %v", want, r.status, r.body)
	}
	return r
}

func (r response) data(t *testing.T) map[string]interface{} {
	t.Helper()
	d, ok := r.body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response has no data object: %v", r.body)
	}
	return d
}

func (r response) dataList(t *testing.T) []interface{} {
	t.Helper()
	d, ok := r.body["data"].([]interface{})
	if !ok {
		t.Fatalf("response has no data array: %v", r.body)
	}
	return d
}

// signup creates a unique user per call so the suite is re-runnable against
// persistent databases (local volume or RDS).
func signup(t *testing.T, role string) (*client, string) {
	t.Helper()
	email := fmt.Sprintf("it-%d-%04d@example.com", time.Now().UnixNano(), rand.Intn(10000))
	c := &client{t: t}
	resp := c.do("POST", "/api/v1/auth/signup", map[string]interface{}{
		"name":     "Integration Tester",
		"email":    email,
		"password": "password123",
		"role":     role,
	}).mustStatus(t, http.StatusCreated)

	data := resp.data(t)
	c.token, _ = data["access_token"].(string)
	if c.token == "" {
		t.Fatalf("signup returned no access_token: %v", resp.body)
	}
	user, _ := data["user"].(map[string]interface{})
	userID, _ := user["id"].(string)
	if userID == "" {
		t.Fatalf("signup returned no user id: %v", resp.body)
	}
	return c, userID
}
