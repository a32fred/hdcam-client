package live

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthDiagnosticsRemainLegacyCompatibleAndSanitized(t *testing.T) {
	hub := New()
	hub.CaptureAttemptStarted()
	hub.Publish([]byte("synthetic-frame"))
	server := httptest.NewServer(hub.HandlerWithCredentials("viewer-test", "synthetic-password"))
	defer server.Close()

	getHealth := func() (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("viewer-test", "synthetic-password")
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}
	status, body := getHealth()
	if status != http.StatusOK {
		t.Fatalf("health status = %d, want 200", status)
	}
	var legacy struct {
		Online    bool   `json:"online"`
		Frames    uint64 `json:"frames"`
		LastFrame string `json:"last_frame"`
	}
	if err := json.Unmarshal(body, &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.Online || legacy.Frames != 1 || legacy.LastFrame == "" {
		t.Fatalf("legacy health fields = %+v", legacy)
	}
	var diagnostics struct {
		CapturePhase    string `json:"capture_phase"`
		CaptureAttempts uint64 `json:"capture_attempts"`
		CaptureSessions uint64 `json:"capture_sessions"`
		CaptureFailures uint64 `json:"capture_failures"`
	}
	if err := json.Unmarshal(body, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if diagnostics.CapturePhase != "streaming" || diagnostics.CaptureAttempts != 1 || diagnostics.CaptureSessions != 1 || diagnostics.CaptureFailures != 0 {
		t.Fatalf("capture diagnostics = %+v", diagnostics)
	}

	const secret = "synthetic-private-runtime-secret"
	hub.CaptureRetry(secret, 0)
	status, body = getHealth()
	if status != http.StatusOK {
		t.Fatalf("health after retry status = %d, want 200", status)
	}
	var retry struct {
		CapturePhase    string `json:"capture_phase"`
		CaptureFailures uint64 `json:"capture_failures"`
		LastFailureCode string `json:"last_failure_code"`
	}
	if err := json.Unmarshal(body, &retry); err != nil {
		t.Fatal(err)
	}
	if retry.CapturePhase != "retrying" || retry.CaptureFailures != 1 || retry.LastFailureCode != "capture_error" {
		t.Fatalf("retry diagnostics = %+v", retry)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("health response leaked runtime detail: %s", body)
	}
}
