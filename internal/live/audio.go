package live

import (
	"net/http"
	"time"
)

// NewWithAudio enables explicit, independently timed PCM playback.
func NewWithAudio(enabled bool) *Hub { h := New(); h.audioEnabled = enabled; return h }
func (h *Hub) PublishAudio(pcm []byte) {
	if !h.audioEnabled || len(pcm) == 0 || len(pcm) > 8192 || len(pcm)%2 != 0 {
		return
	}
	immutable := append([]byte(nil), pcm...)
	h.mu.Lock()
	defer h.mu.Unlock()
	for subscriber := range h.audioSubscribers {
		select {
		case subscriber <- immutable:
		default:
			// SDK reads arrive in bursts: retain recent packets without ending playback.
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- immutable:
			default:
			}
		}
	}
}
func (h *Hub) streamAudio(w http.ResponseWriter, r *http.Request) {
	if !h.audioEnabled {
		http.Error(w, "audio is disabled", 503)
		return
	}
	select {
	case h.viewers <- struct{}{}:
		defer func() { <-h.viewers }()
	default:
		http.Error(w, "viewer limit reached", 503)
		return
	}
	packets := make(chan []byte, 8)
	h.mu.Lock()
	h.audioSubscribers[packets] = struct{}{}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.audioSubscribers, packets); h.mu.Unlock() }()
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Audio-Format", "pcm_s16le;rate=8000;channels=1")
	if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case packet, ok := <-packets:
			if !ok {
				return
			}
			if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return
			}
			if _, err := w.Write(packet); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
			if err := controller.SetWriteDeadline(time.Time{}); err != nil {
				return
			}
		}
	}
}
