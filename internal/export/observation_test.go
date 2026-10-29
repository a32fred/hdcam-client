package export

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"
)

func setEnvelopeMetadata(data []byte, streamID byte, sequence, rawTime uint32) {
	data[5] = streamID
	binary.LittleEndian.PutUint32(data[12:16], sequence)
	binary.LittleEndian.PutUint32(data[8:12], rawTime)
}

func TestObserveAVFramesReportsCompleteJPEGBeforeConsume(t *testing.T) {
	jpeg := jpegFixture(t, 8, 8)
	data := envelope(3, jpeg)
	setEnvelopeMetadata(data, 7, 41, 1234)
	var events []string
	var observations []FrameObservation
	report, err := ObserveAVFrames(context.Background(), bytes.NewReader(data), func(got []byte) error {
		events = append(events, "consume")
		if !bytes.Equal(got, jpeg) {
			t.Fatal("consumer received a different JPEG")
		}
		return nil
	}, nil, func(observation FrameObservation) error {
		events = append(events, "observe")
		observations = append(observations, observation)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []FrameObservation{{Type: 3, StreamID: 7, Sequence: 41, RawTime: 1234, PayloadBytes: uint32(len(jpeg))}}
	if !reflect.DeepEqual(observations, want) || !reflect.DeepEqual(events, []string{"observe", "consume"}) {
		t.Fatalf("observations=%+v events=%v", observations, events)
	}
	if report.Frames != 1 || report.RejectedFrames != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestObserveAVFramesIncludesMalformedJPEG(t *testing.T) {
	data := envelope(3, []byte{0xff, 0xd8, 0xff, 0x00})
	var observed int
	var consumed int
	report, err := ObserveAVFrames(context.Background(), bytes.NewReader(data), func([]byte) error {
		consumed++
		return nil
	}, nil, func(FrameObservation) error {
		observed++
		return nil
	})
	if !errors.Is(err, ErrNoFrames) || observed != 1 || consumed != 0 || report.RejectedFrames != 1 {
		t.Fatalf("observed=%d consumed=%d report=%+v err=%v", observed, consumed, report, err)
	}
}

func TestObserveAVFramesIncludesAudio(t *testing.T) {
	data := envelope(11, []byte{0x30})
	var observations []FrameObservation
	var events []string
	report, err := ObserveAVFrames(context.Background(), bytes.NewReader(data), nil, func([]byte) error {
		events = append(events, "consume")
		return nil
	}, func(observation FrameObservation) error {
		observations = append(observations, observation)
		events = append(events, "observe")
		return nil
	})
	if !errors.Is(err, ErrNoFrames) {
		t.Fatalf("want ErrNoFrames for audio-only input, got %v", err)
	}
	want := []FrameObservation{{Type: 11, StreamID: 0, Sequence: 23, RawTime: 17, PayloadBytes: 1}}
	if !reflect.DeepEqual(observations, want) || !reflect.DeepEqual(events, []string{"observe", "consume"}) || report.NonVideoFrames != 1 || report.AudioFrames != 1 {
		t.Fatalf("observations=%+v events=%v report=%+v", observations, events, report)
	}
}

func TestObserveAVFramesObserverErrorStopsBeforeConsume(t *testing.T) {
	wantErr := errors.New("observer failed")
	var consumed bytes.Buffer
	report, err := ObserveAVFrames(context.Background(), bytes.NewReader(envelope(3, jpegFixture(t, 8, 8))), func(frame []byte) error {
		_, _ = consumed.Write(frame)
		return nil
	}, nil, func(FrameObservation) error { return wantErr })
	if !errors.Is(err, wantErr) || err.Error() != "observe media: observer failed" || consumed.Len() != 0 || report.Frames != 0 {
		t.Fatalf("report=%+v output=%d err=%v", report, consumed.Len(), err)
	}
}

func TestObserveAVFramesDoesNotReportTruncatedEnvelope(t *testing.T) {
	complete := envelope(3, jpegFixture(t, 8, 8))
	for name, data := range map[string][]byte{
		"header":  complete[:16],
		"payload": complete[:len(complete)-1],
	} {
		t.Run(name, func(t *testing.T) {
			observed := 0
			_, err := ObserveAVFrames(context.Background(), bytes.NewReader(data), func([]byte) error { return nil }, nil, func(FrameObservation) error {
				observed++
				return nil
			})
			if observed != 0 || !errors.Is(err, ErrNoFrames) {
				t.Fatalf("observed=%d err=%v", observed, err)
			}
		})
	}
}

type cancelWhenReadComplete struct {
	data   []byte
	cancel context.CancelFunc
	read   int
}

func (r *cancelWhenReadComplete) Read(p []byte) (int, error) {
	if r.read == len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.read:])
	r.read += n
	if r.read == len(r.data) {
		r.cancel()
	}
	return n, nil
}

func TestObserveAVFramesObservesCompleteReadBeforeCancellationCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jpeg := jpegFixture(t, 8, 8)
	source := &cancelWhenReadComplete{data: envelope(3, jpeg), cancel: cancel}
	var observed int
	var consumed int
	report, err := ObserveAVFrames(ctx, source, func([]byte) error {
		consumed++
		return nil
	}, nil, func(observation FrameObservation) error {
		observed++
		if observation.PayloadBytes != uint32(len(jpeg)) {
			t.Fatalf("unexpected observation: %+v", observation)
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || observed != 1 || consumed != 0 || report.Frames != 0 {
		t.Fatalf("observed=%d consumed=%d report=%+v err=%v", observed, consumed, report, err)
	}
}
