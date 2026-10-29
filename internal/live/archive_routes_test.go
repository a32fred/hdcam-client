package live

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArchiveRoutesRemainAuthenticatedAndScoped(t *testing.T) {
	hub := New()
	called := 0
	hub.SetRecordings(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	handler := hub.HandlerWithCredentials("viewer", "secret")
	for _, path := range []string{"/recordings/list", "/recordings/segment-1-01234567/audio"} {
		t.Run(path, func(t *testing.T) {
			before := called
			unauthenticated := httptest.NewRecorder()
			handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
			if unauthenticated.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status=%d, want 401", unauthenticated.Code)
			}
			if called != before {
				t.Fatal("recordings handler called without authentication")
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.SetBasicAuth("viewer", "secret")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusOK || response.Body.String() != path {
				t.Fatalf("authenticated response=%d %q", response.Code, response.Body.String())
			}
		})
	}
	before := called
	for _, path := range []string{"/health", "/unrelated"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetBasicAuth("viewer", "secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if called != before {
			t.Fatalf("recordings handler captured non-recordings route %s", path)
		}
	}
}
