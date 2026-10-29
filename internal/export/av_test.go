package export

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestAVFramesRoutesAudioAndVideo(t *testing.T) {
	video := jpegFixture(t, 16, 16)
	data := append(envelope(11, []byte{0x30}), envelope(3, video)...)
	data = append(data, envelope(11, []byte{0x30})...)
	data = append(data, envelope(2, []byte("other"))...)
	var videos, audio [][]byte
	report, err := AVFrames(context.Background(), bytes.NewReader(data), func(frame []byte) error {
		videos = append(videos, append([]byte(nil), frame...))
		return nil
	}, func(pcm []byte) error {
		audio = append(audio, append([]byte(nil), pcm...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 || !bytes.Equal(videos[0], video) {
		t.Fatalf("video callbacks: %d", len(videos))
	}
	wantPCM := []byte{5, 0, 5, 0}
	if len(audio) != 2 || !bytes.Equal(audio[0], wantPCM) || !bytes.Equal(audio[1], wantPCM) {
		t.Fatalf("audio packets did not independently reset: %v", audio)
	}
	if report.Frames != 1 || report.AudioFrames != 2 || report.AudioSamples != 4 || report.NonVideoFrames != 3 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestAVFramesRejectsMalformedAudioWithoutCallback(t *testing.T) {
	video := jpegFixture(t, 8, 8)
	data := append(envelope(11, nil), envelope(11, bytes.Repeat([]byte{0x30}, 2049))...)
	data = append(data, envelope(3, video)...)
	calls := 0
	report, err := AVFrames(context.Background(), bytes.NewReader(data), func([]byte) error { return nil }, func([]byte) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || report.RejectedAudioFrames != 2 || report.AudioFrames != 0 || report.AudioSamples != 0 || report.NonVideoFrames != 2 {
		t.Fatalf("malformed audio was not rejected: calls=%d report=%+v", calls, report)
	}
}

func TestAVFramesAudioCallbackErrorAndNilCallback(t *testing.T) {
	data := append(envelope(11, []byte{0x30}), envelope(3, jpegFixture(t, 8, 8))...)
	wantErr := errors.New("audio consumer failed")
	report, err := AVFrames(context.Background(), bytes.NewReader(data), func([]byte) error { return nil }, func([]byte) error { return wantErr })
	if !errors.Is(err, wantErr) || report.AudioFrames != 0 {
		t.Fatalf("callback error not propagated: report=%+v err=%v", report, err)
	}
	report, err = AVFrames(context.Background(), bytes.NewReader(data), func([]byte) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.NonVideoFrames != 1 || report.AudioFrames != 0 || report.AudioSamples != 0 || report.RejectedAudioFrames != 0 {
		t.Fatalf("audio should be ignored without callback: %+v", report)
	}
}
