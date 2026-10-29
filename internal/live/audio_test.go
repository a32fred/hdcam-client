package live

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestAudioEndpointDisabledAndAuthenticated(t *testing.T) {
	for _, tt := range []struct {
		name   string
		hub    *Hub
		user   string
		pass   string
		status int
	}{
		{"disabled", NewWithAudio(false), "hdcam", "token", http.StatusServiceUnavailable},
		{"unauthenticated", NewWithAudio(true), "", "", http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.hub.Handler("token"))
			defer server.Close()
			resp := request(t, server.Client(), http.MethodGet, server.URL+"/audio", tt.user, tt.pass)
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
		})
	}
}

func TestAudioEndpointStreamsExactPackets(t *testing.T) {
	hub := NewWithAudio(true)
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/audio", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("hdcam", "token")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("content type = %q", got)
	}
	if got := resp.Header.Get("X-Audio-Format"); got != "pcm_s16le;rate=8000;channels=1" {
		t.Fatalf("audio format = %q", got)
	}
	for _, packet := range [][]byte{{0, 1, 2, 3}, {0xff, 0x7f}} {
		hub.PublishAudio(packet)
		got := make([]byte, len(packet))
		if _, err := io.ReadFull(resp.Body, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, packet) {
			t.Fatalf("packet = %v, want %v", got, packet)
		}
	}
}

func TestCanceledAudioViewerReleasesSharedViewerSlot(t *testing.T) {
	hub := NewWithAudio(true)
	for range 15 {
		hub.viewers <- struct{}{}
	}
	defer func() {
		for len(hub.viewers) > 0 {
			<-hub.viewers
		}
	}()
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/audio", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("hdcam", "token")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(hub.viewers); got != cap(hub.viewers) {
		resp.Body.Close()
		cancel()
		t.Fatalf("occupied viewer slots = %d, want %d", got, cap(hub.viewers))
	}
	cancel()
	resp.Body.Close()
	waitAudioState(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.audioSubscribers) == 0 && len(hub.viewers) == 15
	})
	select {
	case hub.viewers <- struct{}{}:
	default:
		t.Fatal("canceled audio viewer did not release its shared slot")
	}
}

func TestPublishAudioBoundsQueuesAndIgnoresInvalidPackets(t *testing.T) {
	hub := NewWithAudio(true)
	hub.PublishAudio([]byte{9, 0})
	subscriber := make(chan []byte, 8)
	hub.mu.Lock()
	hub.audioSubscribers[subscriber] = struct{}{}
	hub.mu.Unlock()
	if len(subscriber) != 0 {
		t.Fatal("audio published without subscribers was retained")
	}
	for _, packet := range [][]byte{nil, {}, {1}, make([]byte, 8194)} {
		hub.PublishAudio(packet)
	}
	if len(subscriber) != 0 {
		t.Fatalf("invalid packets entered queue: %d", len(subscriber))
	}
	for i := 0; i < 100; i++ {
		hub.PublishAudio([]byte{byte(i), 0})
	}
	if len(subscriber) != cap(subscriber) {
		t.Fatalf("queue length = %d, want bounded capacity %d", len(subscriber), cap(subscriber))
	}
	for i := 0; i < cap(subscriber); i++ {
		packet, ok := <-subscriber
		if !ok {
			t.Fatalf("subscriber channel closed after %d queued packets", i)
		}
		if !bytes.Equal(packet, []byte{byte(92 + i), 0}) {
			t.Fatalf("queued packet %d = %v, want packet %d", i, packet, 92+i)
		}
	}
	select {
	case _, ok := <-subscriber:
		if !ok {
			t.Fatal("queue overrun closed subscriber channel")
		}
		t.Fatal("unexpected extra queued audio packet")
	default:
	}
	hub.mu.Lock()
	_, retained := hub.audioSubscribers[subscriber]
	count := len(hub.audioSubscribers)
	hub.mu.Unlock()
	if !retained || count != 1 {
		t.Fatalf("queue overrun removed subscriber: retained=%v count=%d", retained, count)
	}
	hub.mu.Lock()
	delete(hub.audioSubscribers, subscriber)
	hub.mu.Unlock()
}

func waitAudioState(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("audio handler state did not settle")
		}
		runtime.Gosched()
	}
}
