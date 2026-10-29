package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mjpeg "example.com/hdcam-client/internal/export"
)

func TestWithAudioFileWritesWAV(t *testing.T) {
	dir := t.TempDir()
	pcm := []byte{5, 0, 5, 0}
	report, err := withAudioFile(dir, func(writePCM func([]byte) error) (mjpeg.Report, error) {
		if err := writePCM(pcm); err != nil {
			return mjpeg.Report{}, err
		}
		return mjpeg.Report{AudioFrames: 1, AudioSamples: 2}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.AudioFrames != 1 || report.AudioSamples != 2 {
		t.Fatalf("report = %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(dir, "audio.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 44+len(pcm) || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || !bytes.Equal(data[44:], pcm) {
		t.Fatalf("unexpected WAV bytes: %v", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "audio.wav.part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file remains after success: %v", err)
	}
}

func TestWithAudioFileProducerErrorLeavesPartial(t *testing.T) {
	dir := t.TempDir()
	wantErr := errors.New("capture failed")
	report, err := withAudioFile(dir, func(writePCM func([]byte) error) (mjpeg.Report, error) {
		if err := writePCM([]byte{1, 0}); err != nil {
			t.Fatal(err)
		}
		return mjpeg.Report{AudioFrames: 1}, wantErr
	})
	if !errors.Is(err, wantErr) || report.AudioFrames != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	assertAudioNotFinalized(t, dir)
}

func TestWithAudioFileRejectsNoAudioFrames(t *testing.T) {
	dir := t.TempDir()
	_, err := withAudioFile(dir, func(func([]byte) error) (mjpeg.Report, error) {
		return mjpeg.Report{}, nil
	})
	if err == nil {
		t.Fatal("expected no-audio error")
	}
	assertAudioNotFinalized(t, dir)
}

func TestWithAudioFilePCMWriteFailureLeavesPartial(t *testing.T) {
	dir := t.TempDir()
	_, err := withAudioFile(dir, func(writePCM func([]byte) error) (mjpeg.Report, error) {
		if err := writePCM([]byte{1}); err == nil {
			t.Fatal("odd PCM packet accepted")
		}
		return mjpeg.Report{AudioFrames: 1}, nil
	})
	if err == nil {
		t.Fatal("latched PCM write error was lost")
	}
	assertAudioNotFinalized(t, dir)
}

func assertAudioNotFinalized(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "audio.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final WAV exists after failure: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "audio.wav.part")); err != nil || info.Size() == 0 {
		t.Fatalf("partial WAV missing after failure: info=%v err=%v", info, err)
	}
}
