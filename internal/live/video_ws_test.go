package live

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestVideoWebSocketAuthentication(t *testing.T) {
	h := New()
	h.Publish([]byte("frame"))
	s := httptest.NewServer(h.HandlerWithCredentials("synthetic-viewer", "4826"))
	defer s.Close()
	url := "ws" + strings.TrimPrefix(s.URL, "http") + "/live"
	credentials := base64.StdEncoding.EncodeToString([]byte("synthetic-viewer:4826"))
	for _, tc := range []struct {
		name, auth, origin string
		status             int
	}{
		{"missing", "", "", http.StatusUnauthorized},
		{"wrong", "Basic " + base64.StdEncoding.EncodeToString([]byte("synthetic-viewer:wrong")), "", http.StatusUnauthorized},
		{"foreign origin", "Basic " + credentials, "https://evil.example", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			if tc.auth != "" {
				header.Set("Authorization", tc.auth)
			}
			if tc.origin != "" {
				header.Set("Origin", tc.origin)
			}
			c, resp, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{HTTPHeader: header})
			if err == nil {
				c.CloseNow()
				t.Fatal("unexpected upgrade")
			}
			if resp == nil || resp.StatusCode != tc.status {
				t.Fatalf("response %v, error %v", resp, err)
			}
			waitSlot(t, h)
		})
	}
	header := http.Header{"Authorization": []string{"Basic " + credentials}}
	c, _, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	if id, frame := wsRead(t, c); id != 1 || frame != "frame" {
		t.Fatalf("%d %q", id, frame)
	}
	c.CloseNow()
	waitSlot(t, h)
}

func wsTestServer(h *Hub) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(h.streamWebSocket))
}
func wsConnect(t *testing.T, s *httptest.Server) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func wsRead(t *testing.T, c *websocket.Conn) (uint64, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary || len(b) < 9 || b[0] != 1 {
		t.Fatalf("invalid packet: %v %v", typ, b)
	}
	return binary.LittleEndian.Uint64(b[1:9]), string(b[9:])
}
func waitSlot(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(h.viewers) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(h.viewers) != 0 {
		t.Fatal("viewer slot leaked")
	}
}
func TestVideoWebSocketBackpressure(t *testing.T) {
	h := New()
	h.Publish([]byte("one"))
	s := wsTestServer(h)
	defer s.Close()
	c := wsConnect(t, s)
	defer c.CloseNow()
	if id, frame := wsRead(t, c); id != 1 || frame != "one" {
		t.Fatalf("%d %q", id, frame)
	}
	h.Publish([]byte("two"))
	if id, frame := wsRead(t, c); id != 2 || frame != "two" {
		t.Fatalf("%d %q", id, frame)
	}
	h.Publish([]byte("three"))
	if id, frame := wsRead(t, c); id != 3 || frame != "three" {
		t.Fatalf("%d %q", id, frame)
	}
	h.Publish([]byte("four"))
	if id, frame := wsRead(t, c); id != 4 || frame != "four" {
		t.Fatalf("%d %q", id, frame)
	}
	h.Publish([]byte("five"))
	h.Publish([]byte("six"))
	// The fifth frame must wait for an acknowledgement, without cancelling Read.
	received := make(chan uint64, 1)
	go func() {
		_, b, err := c.Read(context.Background())
		if err == nil && len(b) >= 9 {
			received <- binary.LittleEndian.Uint64(b[1:9])
		}
	}()
	select {
	case <-received:
		t.Fatal("sent more than four unacknowledged frames")
	case <-time.After(100 * time.Millisecond):
	}
	if err := c.Write(context.Background(), websocket.MessageText, []byte("1")); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-received:
		if id != 6 {
			t.Fatalf("got generation %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("no latest frame")
	}
	c.CloseNow()
	waitSlot(t, h)
}
func TestVideoWebSocketInvalidAck(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  websocket.MessageType
		value string
		code  websocket.StatusCode
	}{{"invalid", websocket.MessageText, "abc", websocket.StatusPolicyViolation}, {"future", websocket.MessageText, "2", websocket.StatusPolicyViolation}, {"binary", websocket.MessageBinary, "1", websocket.StatusPolicyViolation}, {"oversized", websocket.MessageText, strings.Repeat("1", 33), websocket.StatusMessageTooBig}} {
		t.Run(tc.name, func(t *testing.T) {
			h := New()
			h.Publish([]byte("x"))
			s := wsTestServer(h)
			defer s.Close()
			c := wsConnect(t, s)
			defer c.CloseNow()
			wsRead(t, c)
			if err := c.Write(context.Background(), tc.kind, []byte(tc.value)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, err := c.Read(ctx)
			if websocket.CloseStatus(err) != tc.code {
				t.Fatalf("close %v", err)
			}
			waitSlot(t, h)
		})
	}
}
func TestVideoWebSocketOriginAndCancel(t *testing.T) {
	h := New()
	s := wsTestServer(h)
	defer s.Close()
	header := http.Header{"Origin": []string{"https://evil.example"}}
	c, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(s.URL, "http"), &websocket.DialOptions{HTTPHeader: header})
	if err == nil {
		c.CloseNow()
		t.Fatal("cross origin accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("response %v error %v", resp, err)
	}
	c = wsConnect(t, s)
	c.CloseNow()
	waitSlot(t, h)
}
func TestVideoWebSocketBaseContextCancel(t *testing.T) {
	h := New()
	ctx, cancel := context.WithCancel(context.Background())
	s := httptest.NewUnstartedServer(http.HandlerFunc(h.streamWebSocket))
	s.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	s.Start()
	c := wsConnect(t, s)
	defer c.CloseNow()
	cancel()
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not close")
	}
	waitSlot(t, h)
}
