package live

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTP2StreamSurvivesIdleWriteDeadlines(t *testing.T) {
	hub := New()
	handler := hub.Handler("token")
	handlerDone := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		handler.ServeHTTP(w, r)
	}))
	server.EnableHTTP2 = true
	server.Config.WriteTimeout = 5 * time.Second
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %s %d, want HTTP/2 200", resp.Proto, resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	for _, frame := range [][]byte{[]byte("after-startup-idle"), []byte("after-frame-idle")} {
		// Exercise both the header flush and a completed frame's flush deadline.
		select {
		case <-handlerDone:
			t.Fatal("stream ended before frame publication")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5500 * time.Millisecond):
		}
		hub.Publish(frame)
		readMultipartFrame(t, reader, frame)
	}
	cancel()
	resp.Body.Close()
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP/2 stream handler did not stop after cancellation")
	}
	if len(hub.viewers) != 0 {
		t.Fatal("canceled HTTP/2 stream retained a viewer slot")
	}
}
