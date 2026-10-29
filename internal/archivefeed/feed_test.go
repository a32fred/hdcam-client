package archivefeed

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"example.com/hdcam-client/internal/archive"
)

func testWriter(t *testing.T, audio bool) *archive.Writer {
	t.Helper()
	w, err := archive.Open(filepath.Join(t.TempDir(), "archive"), archive.Options{MaxFrames: 100, MaxBytes: 32 << 20, Keep: 2, Audio: audio})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}
func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestRunStreamsAndCancellation(t *testing.T) {
	frame := jpegBytes(t)
	ready := make(chan struct{})
	audioReady := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != "secret" {
			t.Error("missing authentication")
		}
		if r.URL.Path == "/audio" {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Audio-Format", "pcm_s16le;rate=8000;channels=1")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-ready
			_, _ = w.Write(make([]byte, 1024))
			w.(http.Flusher).Flush()
			close(audioReady)
			<-r.Context().Done()
			return
		}
		m := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+m.Boundary())
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", "image/jpeg")
		p1, _ := m.CreatePart(h)
		_, _ = p1.Write(frame)
		_, _ = m.CreatePart(h)
		w.(http.Flusher).Flush()
		close(ready)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var report Report
	var err error
	go func() {
		report, err = Run(ctx, Options{Source: srv.URL, Username: "user", Password: "secret", Audio: true}, testWriter(t, true))
		close(done)
	}()
	<-audioReady
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop streams")
	}
	if err != context.Canceled || report.VideoFrames != 1 || report.AudioSamples != 512 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
func TestInvalidSourcesAndRedirect(t *testing.T) {
	w := testWriter(t, false)
	for _, s := range []string{"http://localhost", "http://example.com", "http://127.0.0.1/path", "http://user:secret@127.0.0.1", "http://127.0.0.1/?q=secret", "http://127.0.0.1/#secret"} {
		_, err := Run(context.Background(), Options{Source: s, Username: "user", Password: "secret"}, w)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("source %q: %v", s, err)
		}
	}
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	_, err := Run(context.Background(), Options{Source: origin.URL, Username: "user", Password: "secret"}, w)
	if err == nil || hits.Load() != 0 {
		t.Fatalf("redirect: %v hits=%d", err, hits.Load())
	}
}
func TestBadVideo(t *testing.T) {
	frame := jpegBytes(t)
	cases := []struct {
		name   string
		status int
		ctype  string
		data   []byte
	}{{"status", 401, "", nil}, {"type", 200, "text/plain", nil}, {"jpeg", 200, "image/jpeg", []byte("bad")}, {"oversize", 200, "image/jpeg", bytes.Repeat([]byte{1}, (4<<20)+1)}, {"part type", 200, "text/plain", frame}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 200 {
					w.WriteHeader(tc.status)
					return
				}
				if tc.name == "type" {
					w.Header().Set("Content-Type", tc.ctype)
					_, _ = io.WriteString(w, "secret body")
					return
				}
				m := multipart.NewWriter(w)
				w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+m.Boundary())
				h := textproto.MIMEHeader{}
				h.Set("Content-Type", tc.ctype)
				p, _ := m.CreatePart(h)
				_, _ = p.Write(tc.data)
				_ = m.Close()
			}))
			defer srv.Close()
			report, err := Run(context.Background(), Options{Source: srv.URL, Username: "user", Password: "secret"}, testWriter(t, false))
			if err == nil || strings.Contains(err.Error(), "secret") || report.VideoFrames != 0 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}

func TestAudioPartialAndInvalidFormat(t *testing.T) {
	for _, tc := range []struct {
		name, format string
		data         []byte
		samples      uint64
	}{{"partial", "pcm_s16le;rate=8000;channels=1", []byte{1, 2, 3, 4}, 2}, {"odd", "pcm_s16le;rate=8000;channels=1", []byte{1, 2, 3}, 0}, {"format", "other", nil, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/audio" {
					w.Header().Set("Content-Type", "application/octet-stream")
					w.Header().Set("X-Audio-Format", tc.format)
					_, _ = w.Write(tc.data)
					return
				}
				w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=x")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer srv.Close()
			report, err := Run(context.Background(), Options{Source: srv.URL, Username: "user", Password: "secret", Audio: true}, testWriter(t, true))
			if err == nil || report.AudioSamples != tc.samples {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}

func TestValidateCredentials(t *testing.T) {
	good := Options{Source: "http://127.0.0.1", Username: "user", Password: "secret"}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ user, pass string }{{"", "secret"}, {strings.Repeat("u", 129), "secret"}, {"u:secret", "secret"}, {"u\nsecret", "secret"}, {"user", ""}, {"user", strings.Repeat("p", 1025)}, {"user", "secret\r"}, {"user", "secret\x00"}} {
		opts := good
		opts.Username = tc.user
		opts.Password = tc.pass
		err := Validate(opts)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid credential accepted or exposed: %v", err)
		}
	}
}

func TestStreamFailureCancelsBlockedSibling(t *testing.T) {
	audioStarted := make(chan struct{})
	audioStopped := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio" {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Audio-Format", "pcm_s16le;rate=8000;channels=1")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(audioStarted)
			<-r.Context().Done()
			close(audioStopped)
			return
		}
		<-audioStarted
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	writer := testWriter(t, true)
	go func() {
		_, err := Run(ctx, Options{Source: srv.URL, Username: "user", Password: "secret", Audio: true}, writer)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected video format error")
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after video failure")
	}
	select {
	case <-audioStopped:
	case <-time.After(time.Second):
		t.Fatal("audio request was not canceled")
	}
}
