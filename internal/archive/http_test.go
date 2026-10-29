package archive

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type deadlineResponse struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
	writes    int
}

func (w *deadlineResponse) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *deadlineResponse) Write(b []byte) (int, error) {
	w.writes++
	if len(w.deadlines) != w.writes {
		panic("write deadline was not renewed before writing")
	}
	return w.ResponseRecorder.Write(b)
}

func TestPrivateResponseRenewsWriteDeadline(t *testing.T) {
	underlying := &deadlineResponse{ResponseRecorder: httptest.NewRecorder()}
	w := privateResponse{underlying}
	for range 3 {
		before := time.Now()
		if _, err := w.Write([]byte("chunk")); err != nil {
			t.Fatal(err)
		}
		deadline := underlying.deadlines[len(underlying.deadlines)-1]
		if deadline.Before(before.Add(5*time.Second)) || deadline.After(time.Now().Add(5*time.Second)) {
			t.Fatalf("unexpected idle deadline %s", deadline)
		}
	}
	if underlying.writes != 3 || len(underlying.deadlines) != 3 {
		t.Fatal("missing per-chunk deadline")
	}
}

func TestHTTPArchive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "archive")
	w, err := Open(dir, Options{MaxFrames: 10, MaxBytes: 1 << 20, Keep: 1, Audio: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteVideo(jpegBytes.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteAudio([]byte{1, 0, 2, 0}); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	h := Handler(r)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr
	}
	list := get("/recordings/list")
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var segments []Segment
	if err := json.Unmarshal(list.Body.Bytes(), &segments); err != nil || len(segments) != 1 {
		t.Fatalf("list: %v %#v", err, segments)
	}
	id := segments[0].ID
	if len(segments[0].VideoMS) != 0 {
		t.Fatal("list contains frame timing arrays")
	}
	frame := get("/recordings/" + id + "/frame/0")
	if frame.Code != 200 || !bytes.Equal(frame.Body.Bytes(), jpegBytes.Bytes()) {
		t.Fatalf("frame: %d", frame.Code)
	}
	index := get("/recordings/" + id + "/index.json")
	var segment Segment
	if err := json.Unmarshal(index.Body.Bytes(), &segment); err != nil || len(segment.VideoMS) != 1 {
		t.Fatalf("index: %s %v", index.Body.String(), err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/recordings/"+id+"/audio.wav", nil)
	req.Header.Set("Range", "bytes=44-47")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusPartialContent || !bytes.Equal(rr.Body.Bytes(), []byte{1, 0, 2, 0}) {
		t.Fatalf("range: %d %x", rr.Code, rr.Body.Bytes())
	}
	rr = httptest.NewRecorder()
	req.Header.Set("Range", "bytes=999999-")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestedRangeNotSatisfiable || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid range: %d %#v", rr.Code, rr.Header())
	}
	for _, path := range []string{"/recordings/", "/recordings/../.lock", "/recordings/%2e%2e/.lock", "/recordings/" + id + "/video.mjpg", "/recordings/" + id + "/frame/-1", "/recordings/" + id + "/frame/01", "/recordings/" + id + "/frame/0/extra", "/recordings/" + id + "/frame/999"} {
		if got := get(path); got.Code != 404 {
			t.Errorf("%s: %d", path, got.Code)
		}
	}
	for _, path := range []string{"/recordings", "/recordings/app.js", "/recordings/list", "/recordings/" + id + "/frame/0"} {
		got := get(path)
		if got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" || got.Header().Get("X-Content-Type-Options") != "nosniff" || got.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("headers %s: %#v", path, got.Result())
		}
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/recordings/list", nil))
	if rr.Code != 405 {
		t.Fatalf("POST: %d", rr.Code)
	}
	// Removal after listing must never leave a stale playable cache.
	if err := os.RemoveAll(filepath.Join(dir, id)); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"index.json", "frame/0", "audio.wav"} {
		if got := get("/recordings/" + id + "/" + suffix); got.Code != 404 {
			t.Errorf("removed %s: %d", suffix, got.Code)
		}
	}
}
