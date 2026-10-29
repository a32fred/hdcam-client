package status

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRead(t *testing.T) {
	good := `{"online":true,"frames":1,"last_frame":"2026-01-01T00:00:00Z"}`
	tests := []struct {
		name  string
		code  int
		body  string
		valid bool
	}{
		{"legacy", 200, good, true},
		{"diagnostics", 200, strings.TrimSuffix(good, "}") + `,"capture_phase":"streaming","source_fps":12.5,"last_frame_age_ms":1}`, true},
		{"offline", 503, `{"online":false,"frames":0,"last_frame":"0001-01-01T00:00:00Z"}`, true},
		{"missing", 200, `{"online":true,"frames":1}`, false},
		{"wrong type", 200, `{"online":"true","frames":1,"last_frame":"2026-01-01T00:00:00Z"}`, false},
		{"trailing", 200, good + ` {}`, false},
		{"oversized", 200, good + strings.Repeat(" ", 65536), false},
		{"phase", 200, strings.TrimSuffix(good, "}") + `,"capture_phase":"bad"}`, false},
		{"fps", 200, strings.TrimSuffix(good, "}") + `,"source_fps":-1}`, false},
		{"incoherent", 503, good, false},
		{"auth", 401, "secret", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/health" || r.Header.Get("Cache-Control") != "no-store" {
					t.Error("invalid request")
				}
				u, p, ok := r.BasicAuth()
				if !ok || u != "user" || p != "secret" {
					t.Error("invalid auth")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			_, err := Read(context.Background(), Options{URL: s.URL, Username: "user", Password: "secret", Timeout: time.Second})
			if (err == nil) != tc.valid {
				t.Fatalf("error = %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), s.URL)) {
				t.Fatal("error leaks credentials or URL")
			}
		})
	}
}

func TestRedirectAndCancellation(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	opts := Options{URL: source.URL, Username: "user", Password: "secret", Timeout: time.Second}
	if _, err := Read(context.Background(), opts); err == nil || calls.Load() != 0 {
		t.Fatal("redirect followed or accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, opts); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	opts.URL = "http://localhost:1234"
	if _, err := Read(context.Background(), opts); err == nil {
		t.Fatal("invalid origin accepted")
	}
}

func TestLegacyFrameAge(t *testing.T) {
	for _, tc := range []struct {
		field string
		want  int64
	}{{"", -1}, {`,"last_frame_age_ms":0`, 0}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"online":true,"frames":1,"last_frame":"2026-01-01T00:00:00Z"%s}`, tc.field)
		}))
		got, err := Read(context.Background(), Options{URL: s.URL, Username: "user", Password: "secret", Timeout: time.Second})
		s.Close()
		if err != nil || got.LastFrameAgeMS != tc.want {
			t.Fatalf("field %q: age = %d, error = %v", tc.field, got.LastFrameAgeMS, err)
		}
	}
}

func TestDeadlineDuringResponse(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			done := make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				if partial {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"synthetic_secret":"sensitive-fragment"`)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer s.Close()
			start := time.Now()
			_, err := Read(context.Background(), Options{URL: s.URL, Username: "user", Password: "secret", Timeout: 75 * time.Millisecond})
			if err == nil || time.Since(start) > time.Second || strings.Contains(err.Error(), "sensitive-fragment") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("deadline error = %v, elapsed = %v", err, time.Since(start))
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("handler context was not cancelled")
			}
		})
	}
}
