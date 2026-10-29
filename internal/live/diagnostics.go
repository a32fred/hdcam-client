package live

import (
	"math"
	"time"
)

type CaptureStatus struct {
	CapturePhase    string     `json:"capture_phase"`
	CaptureAttempts uint64     `json:"capture_attempts"`
	CaptureSessions uint64     `json:"capture_sessions"`
	CaptureFailures uint64     `json:"capture_failures"`
	LastFailureCode string     `json:"last_failure_code,omitempty"`
	LastFailureAt   *time.Time `json:"last_failure_at,omitempty"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	SourceFPS       float64    `json:"source_fps"`
	LastFrameAgeMS  int64      `json:"last_frame_age_ms"`
}

type captureDiagnosticState struct {
	phase                        string
	attempts, sessions, failures uint64
	lastFailureCode              string
	lastFailureAt, retryAt       time.Time
	samples                      [128]time.Time
	next, count                  int
}

func (h *Hub) CaptureAttemptStarted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := &h.diagnostics
	d.attempts++
	d.phase = "connecting"
	d.retryAt = time.Time{}
	d.next, d.count = 0, 0
}

func (h *Hub) CaptureRetry(code string, delay time.Duration) {
	switch code {
	case "timeout", "stalled", "startup_failed", "stream_failed", "runtime_missing", "invalid_media", "no_video", "capture_error", "session_completed":
	default:
		code = "capture_error"
	}
	if delay < 0 {
		delay = 0
	}
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}
	now := time.Now().UTC()
	h.mu.Lock()
	defer h.mu.Unlock()
	d := &h.diagnostics
	d.phase = "retrying"
	d.retryAt = now.Add(delay)
	d.next, d.count = 0, 0
	if code != "session_completed" {
		d.failures++
		d.lastFailureCode, d.lastFailureAt = code, now
	}
}

func (h *Hub) noteVideoLocked(now time.Time) {
	d := &h.diagnostics
	if d.phase != "streaming" {
		d.sessions++
		d.phase = "streaming"
		d.retryAt = time.Time{}
	}
	d.samples[d.next] = now
	d.next = (d.next + 1) % len(d.samples)
	if d.count < len(d.samples) {
		d.count++
	}
}

func (h *Hub) Diagnostics() CaptureStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.diagnosticsLocked(time.Now())
}

func (h *Hub) diagnosticsLocked(now time.Time) CaptureStatus {
	d := &h.diagnostics
	phase := d.phase
	if phase == "" {
		phase = "idle"
	}
	status := CaptureStatus{CapturePhase: phase, CaptureAttempts: d.attempts, CaptureSessions: d.sessions, CaptureFailures: d.failures, LastFailureCode: d.lastFailureCode, LastFrameAgeMS: -1}
	// Keep the most recent failure after a successful reconnection.
	if !d.lastFailureAt.IsZero() {
		t := d.lastFailureAt
		status.LastFailureAt = &t
	}
	if !d.retryAt.IsZero() {
		t := d.retryAt
		status.RetryAt = &t
	}
	if !h.updated.IsZero() {
		age := now.Sub(h.updated)
		if age < 0 {
			age = 0
		}
		status.LastFrameAgeMS = age.Milliseconds()
		if phase == "streaming" && age >= 15*time.Second {
			status.CapturePhase = "stalled"
		}
	}
	if status.CapturePhase == "streaming" && d.count >= 2 {
		var first, last time.Time
		n := 0
		for i := 0; i < d.count; i++ {
			sample := d.samples[(d.next-d.count+i+len(d.samples))%len(d.samples)]
			if sample.After(now) || now.Sub(sample) > 5*time.Second {
				continue
			}
			if n == 0 {
				first = sample
			}
			last = sample
			n++
		}
		if n >= 2 && last.After(first) {
			fps := float64(n-1) / last.Sub(first).Seconds()
			if !math.IsNaN(fps) && !math.IsInf(fps, 0) && fps >= 0 {
				status.SourceFPS = fps
			}
		}
	}
	return status
}
