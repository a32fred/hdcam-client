package live

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerWithCredentialsCustomShortPassword(t *testing.T) {
	hub := NewWithAudio(true)
	hub.Publish([]byte("synthetic-jpeg"))
	handler := hub.HandlerWithCredentials("viewer-one", "4826")
	server := httptest.NewServer(handler)
	defer server.Close()

	request := func(path, username, password string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth(username, password)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	if resp := request("/health", "viewer-one", "4826"); resp.StatusCode != http.StatusOK {
		t.Errorf("custom username and short password health status = %d, want 200", resp.StatusCode)
	}
	for _, tt := range []struct {
		name, username, password string
	}{
		{"old default username", "hdcam", "4826"},
		{"wrong password", "viewer-one", "wrong"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if resp := request("/health", tt.username, tt.password); resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

func TestHandlerWithCredentialsProtectsEveryRoute(t *testing.T) {
	handler := NewWithAudio(true).HandlerWithCredentials("viewer-one", "4826")
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, path := range []string{"/", "/health", "/snapshot", "/stream", "/viewer.js", "/audio"} {
		t.Run(path, func(t *testing.T) {
			resp, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("unauthenticated %s status = %d, want 401", path, resp.StatusCode)
			}
		})
	}
}
