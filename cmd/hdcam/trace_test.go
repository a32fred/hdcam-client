package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"

	mjpeg "github.com/a32fred/hdcam-client/internal/export"
)

func TestWriteOutputWithArrivalTrace(t *testing.T) {
	base := t.TempDir()
	destination := filepath.Join(base, "capture")
	var output bytes.Buffer
	const mode = 2
	const audioRequested = true

	err := writeOutput(destination, &output, func(dst io.Writer) (mjpeg.Report, error) {
		return withArrivalTrace(destination, mode, audioRequested, func(observe func(mjpeg.FrameObservation) error) (mjpeg.Report, error) {
			frame := mjpeg.FrameObservation{Type: 3, StreamID: 7, Sequence: 41, RawTime: 1234, PayloadBytes: 99}
			if err := observe(frame); err != nil {
				return mjpeg.Report{}, err
			}
			if err := jpeg.Encode(dst, image.NewRGBA(image.Rect(0, 0, 4, 3)), nil); err != nil {
				return mjpeg.Report{}, err
			}
			return mjpeg.Report{Frames: 1}, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	tracePath := filepath.Join(destination, "frame-arrivals.jsonl")
	traceData, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(traceData))
	for decoder.More() {
		var row map[string]json.RawMessage
		if err := decoder.Decode(&row); err != nil {
			t.Fatalf("decode trace row: %v", err)
		}
		rows = append(rows, row)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d trace rows, want 1", len(rows))
	}
	for key, value := range rows[0] {
		if len(value) == 0 || (value[0] != '-' && (value[0] < '0' || value[0] > '9')) {
			t.Errorf("trace field %q is not numeric: %s", key, value)
		}
	}
	var row struct {
		MonotonicNS  int64  `json:"monotonic_ns"`
		Type         uint8  `json:"type"`
		StreamID     uint8  `json:"stream_id"`
		Sequence     uint32 `json:"sequence"`
		RawTime      uint32 `json:"raw_time_u32"`
		PayloadBytes uint32 `json:"payload_bytes"`
	}
	if err := json.Unmarshal(traceData[:bytes.IndexByte(traceData, '\n')], &row); err != nil {
		t.Fatal(err)
	}
	if row.MonotonicNS < 0 || row.Type != 3 || row.StreamID != 7 || row.Sequence != 41 || row.RawTime != 1234 || row.PayloadBytes != 99 {
		t.Fatalf("unexpected trace row: %+v", row)
	}
	var summary struct {
		VideoMode       int    `json:"video_mode"`
		AudioRequested  bool   `json:"audio_requested"`
		Width           int    `json:"decoded_width"`
		Height          int    `json:"decoded_height"`
		ValidJPEGFrames uint64 `json:"valid_jpeg_frames"`
		Complete        bool   `json:"trace_complete"`
	}
	readJSONFile(t, filepath.Join(destination, "frame-trace-summary.json"), &summary)
	if summary.VideoMode != mode || !summary.AudioRequested || summary.Width != 4 || summary.Height != 3 || summary.ValidJPEGFrames != 1 || !summary.Complete {
		t.Fatalf("unexpected trace summary: %+v", summary)
	}
	for _, name := range []string{"frame-arrivals.jsonl", "frame-trace-summary.json", "video.mjpg", "result.json"} {
		assertFileMode(t, filepath.Join(destination, name), 0600)
	}
	assertFileMode(t, destination, 0700)
	if _, err := os.Stat(filepath.Join(destination, "frame-arrivals.jsonl.part")); !os.IsNotExist(err) {
		t.Fatalf("successful trace left partial file: %v", err)
	}
}

func TestWriteOutputWithArrivalTraceKeepsFailurePartial(t *testing.T) {
	base := t.TempDir()
	destination := filepath.Join(base, "failed")
	wantErr := errors.New("capture failed")
	var output bytes.Buffer
	err := writeOutput(destination, &output, func(dst io.Writer) (mjpeg.Report, error) {
		return withArrivalTrace(destination, 1, false, func(observe func(mjpeg.FrameObservation) error) (mjpeg.Report, error) {
			frame := mjpeg.FrameObservation{Type: 3, StreamID: 2, Sequence: 9, RawTime: 77, PayloadBytes: 100}
			if err := observe(frame); err != nil {
				return mjpeg.Report{}, err
			}
			if err := jpeg.Encode(dst, image.NewRGBA(image.Rect(0, 0, 5, 2)), nil); err != nil {
				return mjpeg.Report{}, err
			}
			return mjpeg.Report{Frames: 1}, wantErr
		})
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("writeOutput error = %v, want %v", err, wantErr)
	}
	if _, err := os.Stat(filepath.Join(destination, "frame-arrivals.jsonl.part")); err != nil {
		t.Fatalf("missing partial trace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "frame-arrivals.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("published failed trace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "video.mjpg")); !os.IsNotExist(err) {
		t.Fatalf("published failed video: %v", err)
	}
	var result struct{ Complete bool }
	readJSONFile(t, filepath.Join(destination, "result.json"), &result)
	if result.Complete {
		t.Fatal("failed capture marked complete")
	}
	var summary struct {
		Complete bool `json:"trace_complete"`
	}
	readJSONFile(t, filepath.Join(destination, "frame-trace-summary.json"), &summary)
	if summary.Complete {
		t.Fatal("failed trace marked complete")
	}
	assertFileMode(t, filepath.Join(destination, "frame-arrivals.jsonl.part"), 0600)
	assertFileMode(t, filepath.Join(destination, "frame-trace-summary.json"), 0600)
}

func TestWriteOutputWithArrivalTraceRefusesExistingFinalTrace(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "capture")
	var output bytes.Buffer
	if err := writeOutput(destination, &output, func(dst io.Writer) (mjpeg.Report, error) {
		tracePath := filepath.Join(destination, "frame-arrivals.jsonl")
		if err := os.WriteFile(tracePath, []byte("existing trace\n"), 0600); err != nil {
			return mjpeg.Report{}, err
		}
		return withArrivalTrace(destination, 1, false, func(observe func(mjpeg.FrameObservation) error) (mjpeg.Report, error) {
			return mjpeg.Report{}, nil
		})
	}); err == nil {
		t.Fatal("accepted output when final trace file already existed")
	}
	got, err := os.ReadFile(filepath.Join(destination, "frame-arrivals.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing trace\n" {
		t.Fatalf("existing trace was overwritten: %q", got)
	}
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %#o, want %#o", path, got, want)
	}
}
