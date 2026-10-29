package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/hdcam-client/internal/archive"
)

func archiveJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	img.Set(0, 0, color.White)
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func archiveToken(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func archiveSource(t *testing.T, sendFrame bool, cancelled chan<- struct{}) *httptest.Server {
	t.Helper()
	frame := archiveJPEG(t)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "hdcam" || password != strings.Repeat("a", 64) {
			t.Errorf("missing viewer authentication")
		}
		if r.URL.Path != "/stream" {
			t.Errorf("stream path = %q", r.URL.Path)
		}
		writer := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+writer.Boundary())
		if sendFrame {
			header := textproto.MIMEHeader{}
			header.Set("Content-Type", "image/jpeg")
			part, err := writer.CreatePart(header)
			if err != nil {
				t.Errorf("CreatePart: %v", err)
				return
			}
			if _, err := part.Write(frame); err != nil {
				t.Errorf("write frame: %v", err)
				return
			}
			nextHeader := textproto.MIMEHeader{}
			nextHeader.Set("Content-Type", "image/jpeg")
			if _, err := writer.CreatePart(nextHeader); err != nil {
				t.Errorf("start next part: %v", err)
				return
			}
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
		if cancelled != nil {
			close(cancelled)
		}
	}))
}

func TestRecordCommandWritesPrivateArchiveAndCancelsSource(t *testing.T) {
	cancelled := make(chan struct{})
	source := archiveSource(t, true, cancelled)
	defer source.Close()
	directory := filepath.Join(t.TempDir(), "archive")
	args := []string{"--source", source.URL, "--token-file", archiveToken(t), "--archive-dir", directory, "--duration", "250ms", "--segment-frames", "100", "--keep-segments", "2"}
	var out, diagnostics bytes.Buffer
	if got := recordCommand(args, &out, &diagnostics); got != 0 {
		t.Fatalf("recordCommand exit=%d stdout=%q stderr=%q", got, out.String(), diagnostics.String())
	}
	var result struct {
		VideoFrames uint64 `json:"video_frames"`
		Complete    bool   `json:"complete"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid summary %q: %v", out.String(), err)
	}
	if !result.Complete || result.VideoFrames == 0 {
		t.Fatalf("summary = %+v", result)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("source handler did not exit after duration cancellation")
	}
	r, err := archive.NewReader(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	segments, err := r.List()
	if err != nil || len(segments) == 0 || segments[0].Frames == 0 {
		t.Fatalf("archive list = %+v, %v", segments, err)
	}
}

func TestRecordCommandRejectsUnsafeSourceBeforeCreatingArchive(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "must-not-exist")
	args := []string{"--source", "http://example.com", "--token-file", archiveToken(t), "--archive-dir", directory}
	var out, diagnostics bytes.Buffer
	if got := recordCommand(args, &out, &diagnostics); got != 2 {
		t.Fatalf("recordCommand exit = %d, want 2; stderr=%q", got, diagnostics.String())
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("unsafe source created archive path: stat error %v", err)
	}
}

func TestRecordCommandUsageAndEmptySourceFailure(t *testing.T) {
	t.Run("negative duration", func(t *testing.T) {
		var out, diagnostics bytes.Buffer
		if got := recordCommand([]string{"--duration", "-1s"}, &out, &diagnostics); got != 2 {
			t.Fatalf("exit=%d, want 2; stderr=%q", got, diagnostics.String())
		}
	})
	t.Run("missing flags", func(t *testing.T) {
		var out, diagnostics bytes.Buffer
		if got := recordCommand(nil, &out, &diagnostics); got != 2 {
			t.Fatalf("exit=%d, want 2; stderr=%q", got, diagnostics.String())
		}
	})
	t.Run("no frames is incomplete", func(t *testing.T) {
		cancelled := make(chan struct{})
		source := archiveSource(t, false, cancelled)
		defer source.Close()
		var out, diagnostics bytes.Buffer
		args := []string{"--source", source.URL, "--token-file", archiveToken(t), "--archive-dir", filepath.Join(t.TempDir(), "archive"), "--duration", "100ms"}
		if got := recordCommand(args, &out, &diagnostics); got != 1 {
			t.Fatalf("exit=%d, want 1; stdout=%q stderr=%q", got, out.String(), diagnostics.String())
		}
		var result struct {
			Complete bool `json:"complete"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Complete {
			t.Fatalf("summary=%q, err=%v; expected complete=false", out.String(), err)
		}
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("empty stream handler did not exit after cancellation")
		}
	})
}

func TestRecordingsCommandRejectsArgumentsBeforeReadingFilesOrBinding(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"missing flags", nil},
		{"non-loopback", []string{"--archive-dir", "missing-archive", "--token-file", "missing-token", "--listen", "0.0.0.0:0"}},
		{"hostname", []string{"--archive-dir", "missing-archive", "--token-file", "missing-token", "--listen", "localhost:0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			if got := recordingsCommand(tt.args, &out, &diagnostics); got != 2 {
				t.Fatalf("exit=%d, want 2; stderr=%q", got, diagnostics.String())
			}
			if strings.Contains(diagnostics.String(), "no such file") || strings.Contains(out.String(), "available at") {
				t.Fatalf("command read files or bound listener before rejecting: stdout=%q stderr=%q", out.String(), diagnostics.String())
			}
		})
	}
}

func TestServeCommandRejectsBothRecordingFormatsBeforeConfigLoad(t *testing.T) {
	args := []string{"--config", "missing-config", "--token-file", "missing-token", "--record-dir", "one", "--archive-dir", "two"}
	var out, diagnostics bytes.Buffer
	if got := serveCommand(args, &out, &diagnostics); got != 2 {
		t.Fatalf("serveCommand exit=%d, want 2; stderr=%q", got, diagnostics.String())
	}
	if strings.Contains(diagnostics.String(), "no such file") {
		t.Fatalf("serveCommand loaded configuration before rejecting formats: %q", diagnostics.String())
	}
}
