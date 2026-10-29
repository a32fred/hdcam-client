package live

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Hub stores one immutable JPEG and wakes viewers without queuing old frames.
type Hub struct {
	mu         sync.Mutex
	frame      []byte
	updated    time.Time
	changed    chan struct{}
	generation uint64
	viewers    chan struct{}
}

func New() *Hub { return &Hub{changed: make(chan struct{}), viewers: make(chan struct{}, 16)} }
func (h *Hub) Publish(frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.frame = append([]byte(nil), frame...)
	h.updated = time.Now()
	h.generation++
	close(h.changed)
	h.changed = make(chan struct{})
}
func (h *Hub) snapshot() ([]byte, time.Time, uint64, <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.frame, h.updated, h.generation, h.changed
}
func (h *Hub) Handler(token string) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		user, password, ok := r.BasicAuth()
		actual := sha256.Sum256([]byte(password))
		if token == "" || !ok || user != "hdcam" || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="hdcam", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", 405)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'none'; frame-ancestors 'none'")
			fmt.Fprint(w, `<!doctype html><html lang="pt-BR"><meta name="viewport" content="width=device-width"><title>Câmera</title><h1>Câmera</h1><p>Vídeo sem áudio. Consulte <a href="/health">o estado da captura</a> se a imagem parar.</p><img src="/stream" alt="Vídeo da câmera" style="max-width:100%;height:auto"></html>`)
		case "/health":
			_, updated, sequence, _ := h.snapshot()
			fresh := !updated.IsZero() && time.Since(updated) < 15*time.Second
			w.Header().Set("Content-Type", "application/json")
			if !fresh {
				w.WriteHeader(503)
			}
			json.NewEncoder(w).Encode(struct {
				Online    bool      `json:"online"`
				Frames    uint64    `json:"frames"`
				LastFrame time.Time `json:"last_frame"`
			}{fresh, sequence, updated})
		case "/snapshot":
			select {
			case h.viewers <- struct{}{}:
				defer func() { <-h.viewers }()
			default:
				http.Error(w, "viewer limit reached", 503)
				return
			}
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return
			}
			frame, updated, _, _ := h.snapshot()
			if updated.IsZero() || time.Since(updated) >= 15*time.Second {
				http.Error(w, "camera unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(frame)
		case "/stream":
			h.stream(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
func (h *Hub) stream(w http.ResponseWriter, r *http.Request) {
	select {
	case h.viewers <- struct{}{}:
		defer func() { <-h.viewers }()
	default:
		http.Error(w, "viewer limit reached", 503)
		return
	}
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=hdcamframe")
	if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		return
	}
	var previous uint64
	for {
		frame, updated, generation, changed := h.snapshot()
		if generation != previous && time.Since(updated) < 15*time.Second {
			if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "--hdcamframe\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
			if err := controller.SetWriteDeadline(time.Time{}); err != nil {
				return
			}
			previous = generation
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		}
	}
}

// Watch cancels a capture that never starts or stops producing complete frames.
func Watch(ctx context.Context, startup, stall time.Duration, attempt func(context.Context, func()) error) error {
	bounded, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	last := time.Now()
	limit := startup
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-bounded.Done():
				return
			case <-ticker.C:
				mu.Lock()
				expired := time.Since(last) >= limit
				mu.Unlock()
				if expired {
					cancel()
					return
				}
			}
		}
	}()
	return attempt(bounded, func() { mu.Lock(); last = time.Now(); limit = stall; mu.Unlock() })
}
