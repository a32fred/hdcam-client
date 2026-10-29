package main

import (
	"errors"
	"example.com/hdcam-client/internal/audio"
	mjpeg "example.com/hdcam-client/internal/export"
	"io"
	"os"
	"path/filepath"
)

func jpegSink(w io.Writer) func([]byte) error {
	return func(frame []byte) error {
		n, err := w.Write(frame)
		if err == nil && n != len(frame) {
			return io.ErrShortWrite
		}
		return err
	}
}
func withAudioFile(directory string, produce func(func([]byte) error) (mjpeg.Report, error)) (mjpeg.Report, error) {
	var report mjpeg.Report
	part := filepath.Join(directory, "audio.wav.part")
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return report, err
	}
	wav, err := audio.NewWAV(file)
	if err != nil {
		file.Close()
		return report, err
	}
	report, produceErr := produce(func(pcm []byte) error {
		n, err := wav.Write(pcm)
		if err == nil && n != len(pcm) {
			return io.ErrShortWrite
		}
		return err
	})
	err = errors.Join(produceErr, wav.Close(), file.Close())
	if err == nil && report.AudioFrames == 0 {
		err = errors.New("audio requested but no valid audio packets received")
	}
	if err == nil {
		err = os.Rename(part, filepath.Join(directory, "audio.wav"))
	}
	return report, err
}
