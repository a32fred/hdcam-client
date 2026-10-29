package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"time"

	mjpeg "example.com/hdcam-client/internal/export"
	"example.com/hdcam-client/internal/frametrace"
)

// withArrivalTrace stamps complete envelopes before decoding; it does not infer sensor timing.
func withArrivalTrace(directory string, mode int, audio bool, produce func(func(mjpeg.FrameObservation) error) (mjpeg.Report, error)) (mjpeg.Report, error) {
	var report mjpeg.Report
	partial := filepath.Join(directory, "frame-arrivals.jsonl.part")
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return report, err
	}
	buffered := bufio.NewWriterSize(file, 64<<10)
	trace := frametrace.New(buffered)
	start := time.Now()
	report, runErr := produce(func(frame mjpeg.FrameObservation) error {
		arrived := time.Since(start).Nanoseconds()
		return trace.Record(frametrace.Entry{MonotonicNS: arrived, Type: frame.Type, StreamID: frame.StreamID, Sequence: frame.Sequence, RawTime: frame.RawTime, PayloadBytes: frame.PayloadBytes})
	})
	runErr = errors.Join(runErr, buffered.Flush(), file.Sync(), file.Close())
	if runErr == nil {
		runErr = os.Link(partial, filepath.Join(directory, "frame-arrivals.jsonl"))
		if runErr == nil {
			runErr = os.Remove(partial)
		}
	}
	summary := struct {
		Schema          int    `json:"schema"`
		Clock           string `json:"clock"`
		Boundary        string `json:"boundary"`
		VideoMode       int    `json:"video_mode"`
		AudioRequested  bool   `json:"audio_requested"`
		Width           int    `json:"decoded_width,omitempty"`
		Height          int    `json:"decoded_height,omitempty"`
		ValidJPEGFrames uint64 `json:"valid_jpeg_frames"`
		Complete        bool   `json:"trace_complete"`
		frametrace.Summary
	}{Schema: 1, Clock: "go_elapsed_monotonic_ns", Boundary: "complete_envelope_before_jpeg_decode", VideoMode: mode, AudioRequested: audio, ValidJPEGFrames: report.Frames, Complete: runErr == nil, Summary: trace.Summary()}
	if report.Frames > 0 {
		video, err := os.Open(filepath.Join(directory, "video.mjpg.part"))
		if err == nil {
			config, decodeErr := jpeg.DecodeConfig(video)
			video.Close()
			if decodeErr == nil {
				summary.Width = config.Width
				summary.Height = config.Height
			}
		}
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return report, errors.Join(runErr, err)
	}
	meta, err := os.OpenFile(filepath.Join(directory, "frame-trace-summary.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return report, errors.Join(runErr, err)
	}
	_, writeErr := meta.Write(append(encoded, '\n'))
	metaErr := errors.Join(writeErr, meta.Sync(), meta.Close())
	return report, errors.Join(runErr, metaErr)
}
