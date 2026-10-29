package live

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCaptureDiagnosticsSamples(t *testing.T) {
	h := New()
	now := time.Now()
	for i := 0; i < 140; i++ {
		at := now.Add(time.Duration(i-139) * 500 * time.Millisecond)
		h.noteVideoLocked(at)
		h.updated = at
	}
	got := h.diagnosticsLocked(now)
	if got.SourceFPS != 2 || got.CaptureSessions != 1 || h.diagnostics.count != 128 {
		t.Fatalf("status=%+v count=%d", got, h.diagnostics.count)
	}
	got = h.diagnosticsLocked(now.Add(6 * time.Second))
	if got.SourceFPS != 0 {
		t.Fatalf("stale FPS: %+v", got)
	}
	got = h.diagnosticsLocked(now.Add(15 * time.Second))
	if got.CapturePhase != "stalled" {
		t.Fatalf("phase: %+v", got)
	}
	h.CaptureAttemptStarted()
	h.noteVideoLocked(now.Add(time.Minute))
	h.updated = now.Add(time.Minute)
	got = h.diagnosticsLocked(now.Add(time.Minute))
	if got.SourceFPS != 0 || got.CaptureAttempts != 1 || got.CaptureSessions != 2 {
		t.Fatalf("after outage: %+v", got)
	}
}

func TestCaptureDiagnosticsRetriesAndCopies(t *testing.T) {
	h := New()
	if got := h.Diagnostics(); got.CapturePhase != "idle" || got.LastFrameAgeMS != -1 {
		t.Fatal(got)
	}
	h.CaptureRetry("secret token", -time.Second)
	got := h.Diagnostics()
	if got.CaptureFailures != 1 || got.LastFailureCode != "capture_error" || got.LastFailureAt == nil || got.RetryAt == nil {
		t.Fatal(got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "secret") {
		t.Fatal(string(b))
	}
	failureAt := *got.LastFailureAt
	*got.LastFailureAt = time.Time{}
	*got.RetryAt = time.Time{}
	if fresh := h.Diagnostics(); fresh.LastFailureAt == nil || !fresh.LastFailureAt.Equal(failureAt) || fresh.RetryAt.IsZero() {
		t.Fatal(fresh)
	}
	h.CaptureRetry("session_completed", time.Second)
	h.CaptureAttemptStarted()
	h.noteVideoLocked(time.Now())
	got = h.Diagnostics()
	if got.CaptureFailures != 1 || got.LastFailureCode != "capture_error" || got.RetryAt != nil || got.CaptureSessions != 1 {
		t.Fatal(got)
	}
}

func TestCaptureDiagnosticsConcurrent(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				switch i {
				case 0:
					h.Publish([]byte{1})
				case 1:
					h.CaptureAttemptStarted()
					h.CaptureRetry("timeout", time.Second)
				case 2:
					h.Diagnostics()
				}
			}
		}(i)
	}
	wg.Wait()
}
