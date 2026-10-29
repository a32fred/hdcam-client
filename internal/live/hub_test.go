package live

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"testing"
	"time"
)

func request(t *testing.T, client *http.Client, method, url, user, password string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" || password != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHandlerBasicAuth(t *testing.T) {
	hub := New()
	server := httptest.NewServer(hub.Handler("secret"))
	defer server.Close()
	for _, tt := range []struct {
		name, user, password string
		want                 int
	}{
		{"missing", "", "", http.StatusUnauthorized},
		{"wrong password", "hdcam", "incorrect", http.StatusUnauthorized},
		{"wrong user", "other", "secret", http.StatusUnauthorized},
		{"valid", "hdcam", "secret", http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := request(t, server.Client(), http.MethodGet, server.URL+"/health", tt.user, tt.password)
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if tt.want == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") == "" {
				t.Error("unauthorized response missing WWW-Authenticate")
			}
		})
	}
}

func TestHealthOfflineOnlineAndSnapshotExactBytes(t *testing.T) {
	hub := New()
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()
	client := server.Client()
	get := func(path string) *http.Response {
		t.Helper()
		return request(t, client, http.MethodGet, server.URL+path, "hdcam", "token")
	}

	resp := get("/health")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("empty hub health status = %d, want 503", resp.StatusCode)
	}
	var health struct {
		Online bool   `json:"online"`
		Frames uint64 `json:"frames"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if health.Online || health.Frames != 0 {
		t.Fatalf("empty hub health = %+v, want offline and zero frames", health)
	}

	frame := []byte{0xff, 0xd8, 0x00, 0x7f, 0xff, 0xd9}
	hub.Publish(frame)
	resp = get("/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("published hub health status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if !health.Online || health.Frames != 1 {
		t.Fatalf("published hub health = %+v, want online with one frame", health)
	}

	resp = get("/snapshot")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("snapshot content type = %q, want image/jpeg", got)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(frame) {
		t.Errorf("snapshot = %v, want exact bytes %v", got, frame)
	}
	resp.Body.Close()

	hub.mu.Lock()
	hub.updated = time.Now().Add(-16 * time.Second)
	hub.mu.Unlock()
	resp = get("/health")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("stale hub health status = %d, want 503", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()
	if health.Online {
		t.Errorf("stale hub reported online: %+v", health)
	}
	resp = get("/snapshot")
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("stale snapshot status = %d, want 503", resp.StatusCode)
	}
}

func TestHandlerUnknownPathAndMethod(t *testing.T) {
	server := httptest.NewServer(New().Handler("token"))
	defer server.Close()
	for _, tt := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/missing", http.StatusNotFound},
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
	} {
		resp := request(t, server.Client(), tt.method, server.URL+tt.path, "hdcam", "token")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.want {
			t.Errorf("%s %s status = %d, want %d", tt.method, tt.path, resp.StatusCode, tt.want)
		}
	}
}

func TestStreamPublishesMultipartFramesAndStopsOnCancellation(t *testing.T) {
	hub := New()
	hub.Publish([]byte("jpeg-one"))
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("hdcam", "token")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "multipart/x-mixed-replace; boundary=hdcamframe" {
		resp.Body.Close()
		t.Fatalf("stream content type = %q", got)
	}
	reader := bufio.NewReader(resp.Body)
	readMultipartFrame(t, reader, []byte("jpeg-one"))
	hub.Publish([]byte("jpeg-two"))
	readMultipartFrame(t, reader, []byte("jpeg-two"))
	cancel()
	resp.Body.Close()
}

func readMultipartFrame(t *testing.T, reader *bufio.Reader, want []byte) {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read multipart boundary: %v", err)
	}
	if line != "--hdcamframe\r\n" {
		t.Fatalf("boundary = %q", line)
	}
	headers, err := textproto.NewReader(reader).ReadMIMEHeader()
	if err != nil {
		t.Fatalf("read multipart headers: %v", err)
	}
	if got := headers.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("part content type = %q", got)
	}
	n, err := strconv.Atoi(headers.Get("Content-Length"))
	if err != nil || n != len(want) {
		t.Fatalf("part content length = %q, want %d", headers.Get("Content-Length"), len(want))
	}
	got := make([]byte, n)
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read multipart payload: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("multipart payload = %q, want %q", got, want)
	}
	if crlf, err := reader.ReadString('\n'); err != nil || crlf != "\r\n" {
		t.Fatalf("part terminator = %q, err %v", crlf, err)
	}
}

func TestSlowViewerDoesNotBlockPublish(t *testing.T) {
	hub := New()
	hub.Publish([]byte("initial-frame"))
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("hdcam", "token")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	frame := make([]byte, 64<<10)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			hub.Publish(frame)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked behind a viewer that is not reading")
	}
	cancel()
	resp.Body.Close()
}

func TestWatchStartupTimeoutCancelsAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, 120*time.Millisecond, time.Second, func(attemptCtx context.Context, _ func()) error {
			<-attemptCtx.Done()
			return attemptCtx.Err()
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch error = %v, want context.Canceled", err)
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("startup timeout did not cancel attempt")
	}
}

func TestWatchStallAfterFrameCancelsAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, time.Second, 120*time.Millisecond, func(attemptCtx context.Context, frame func()) error {
			frame()
			<-attemptCtx.Done()
			return attemptCtx.Err()
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch error = %v, want context.Canceled", err)
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("stall timeout did not cancel attempt")
	}
}

func TestWatchParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, time.Hour, time.Hour, func(attemptCtx context.Context, _ func()) error {
			close(started)
			<-attemptCtx.Done()
			return attemptCtx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("attempt did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not stop Watch")
	}
}
