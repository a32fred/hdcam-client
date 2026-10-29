package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testViewerPassword = "synthetic-status-password"

func statusTokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(testViewerPassword+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func statusArgs(serverURL, tokenFile string) []string {
	return []string{"status", "--url", serverURL, "--username", "viewer-test", "--token-file", tokenFile}
}

func TestStatusCommandReportsLiveAndLegacyHealth(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		available  bool
	}{
		{"new diagnostics", `{"online":true,"frames":3,"last_frame":"2026-10-29T12:00:00Z","capture_phase":"streaming","capture_attempts":2,"capture_sessions":1,"capture_failures":0,"source_fps":2,"last_frame_age_ms":3}`, http.StatusOK, true},
		{"legacy health", `{"online":true,"frames":1,"last_frame":"2026-10-29T12:00:00Z"}`, http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if !ok || user != "viewer-test" || password != testViewerPassword {
					t.Errorf("unexpected credentials")
				}
				if r.URL.Path != "/health" {
					t.Errorf("path = %q, want /health", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			var out, diagnostics bytes.Buffer
			code := run(statusArgs(server.URL, statusTokenFile(t)), &out, &diagnostics)
			if code != 0 {
				t.Fatalf("exit = %d, diagnostics %q", code, diagnostics.String())
			}
			var report struct {
				Online               bool   `json:"online"`
				DiagnosticsAvailable bool   `json:"diagnostics_available"`
				CapturePhase         string `json:"capture_phase"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("report %q: %v", out.String(), err)
			}
			if !report.Online || report.DiagnosticsAvailable != tc.available {
				t.Fatalf("report = %+v", report)
			}
		})
	}
}

func TestStatusCommandOfflineKeepsReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"online":false,"frames":0,"last_frame":"0001-01-01T00:00:00Z","capture_phase":"retrying"}`))
	}))
	defer server.Close()
	var out, diagnostics bytes.Buffer
	if code := run(statusArgs(server.URL, statusTokenFile(t)), &out, &diagnostics); code != 1 {
		t.Fatalf("exit = %d, want 1; diagnostics %q", code, diagnostics.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report["online"] != false || report["diagnostics_available"] != true {
		t.Fatalf("offline report = %q, err %v", out.String(), err)
	}
}

func TestStatusCommandAuthenticationFailureDoesNotReflectBody(t *testing.T) {
	const malicious = "raw-private-server-response"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(malicious))
	}))
	defer server.Close()
	var out, diagnostics bytes.Buffer
	if code := run(statusArgs(server.URL, statusTokenFile(t)), &out, &diagnostics); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if strings.Contains(out.String()+diagnostics.String(), malicious) || strings.Contains(out.String()+diagnostics.String(), testViewerPassword) {
		t.Fatalf("sensitive response or credential reflected: out=%q diagnostics=%q", out.String(), diagnostics.String())
	}
	if out.Len() != 0 {
		t.Fatalf("authentication failure emitted a report: %q", out.String())
	}
}

func TestStatusInvalidArgumentsDoNotReadTokenOrContactServer(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	nonexistent := filepath.Join(t.TempDir(), "must-not-be-read")
	for _, extra := range [][]string{
		{"--username", "bad:name"},
		{"--timeout", "0s"},
		{"--timeout", "61s"},
		{"unexpected"},
	} {
		args := append(statusArgs(server.URL, nonexistent), extra...)
		var out, diagnostics bytes.Buffer
		if code := run(args, &out, &diagnostics); code != 2 {
			t.Errorf("args %v: exit = %d, want 2 (diagnostics %q)", extra, code, diagnostics.String())
		}
	}
	if called {
		t.Fatal("invalid arguments contacted server")
	}
	if _, err := os.Stat(nonexistent); !os.IsNotExist(err) {
		t.Fatalf("invalid arguments touched missing token file: %v", err)
	}
}

func TestStatusInvalidTimeoutDoesNotWait(t *testing.T) {
	start := time.Now()
	var out, diagnostics bytes.Buffer
	args := append([]string{"status"}, []string{"--timeout", "61s", "--token-file", filepath.Join(t.TempDir(), "absent")}...)
	if code := run(args, &out, &diagnostics); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("invalid timeout took too long")
	}
}
