package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"io"

	"example.com/hdcam-client/internal/audio"
	"example.com/hdcam-client/internal/media"
)

var ErrNoFrames = errors.New("no valid video frames")

type Report struct {
	Frames              uint64      `json:"frames"`
	NonVideoFrames      uint64      `json:"non_video_frames"`
	RejectedFrames      uint64      `json:"rejected_frames"`
	AudioFrames         uint64      `json:"audio_frames,omitempty"`
	AudioSamples        uint64      `json:"audio_samples,omitempty"`
	RejectedAudioFrames uint64      `json:"rejected_audio_frames,omitempty"`
	Media               media.Stats `json:"media"`
}

// FrameObservation describes a complete envelope before image decoding.
// RawTime is an uninterpreted source field, not a per-frame clock.
type FrameObservation struct {
	Type, StreamID                  uint8
	Sequence, RawTime, PayloadBytes uint32
}

type exactJPEGReader struct{ *bytes.Reader }

func (r exactJPEGReader) Read(p []byte) (int, error) {
	// Prevent decoder read-ahead from hiding another image or trailing data.
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

// WriteMJPEG preserves complete JPEG payloads; a truncated final envelope is
// reported but recoverable. Cancellation cannot interrupt a blocked source.Read.
func WriteMJPEG(ctx context.Context, source io.Reader, destination io.Writer) (report Report, err error) {
	return Frames(ctx, source, func(frame []byte) error {
		n, err := destination.Write(frame)
		if err == nil && n != len(frame) {
			return io.ErrShortWrite
		}
		return err
	})
}

// Frames validates JPEGs before handing each complete frame to a synchronous consumer.
func Frames(ctx context.Context, source io.Reader, consume func([]byte) error) (report Report, err error) {
	return AVFrames(ctx, source, consume, nil)
}

// AVFrames routes independently decoded PCM packets alongside validated JPEGs.
// Callbacks run synchronously; no audio/video timestamp synchronization is inferred.
func AVFrames(ctx context.Context, source io.Reader, consume, consumeAudio func([]byte) error) (report Report, err error) {
	return ObserveAVFrames(ctx, source, consume, consumeAudio, nil)
}

func ObserveAVFrames(ctx context.Context, source io.Reader, consume, consumeAudio func([]byte) error, observe func(FrameObservation) error) (report Report, err error) {
	reader := media.NewReader(source, 0)
	defer func() { report.Media = reader.Statistics() }()
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		frame, err := reader.Next()
		if err == nil && observe != nil {
			if err := observe(FrameObservation{Type: frame.Type, StreamID: frame.StreamID, Sequence: frame.Sequence, RawTime: frame.Timestamp, PayloadBytes: uint32(len(frame.Payload))}); err != nil {
				return report, fmt.Errorf("observe media: %w", err)
			}
		}
		if canceled := ctx.Err(); canceled != nil {
			return report, canceled
		}
		if errors.Is(err, io.EOF) || errors.Is(err, media.ErrTruncated) {
			break
		}
		if err != nil {
			return report, fmt.Errorf("read media: %w", err)
		}
		if frame.Type != 3 {
			report.NonVideoFrames++
			if frame.Type == 11 && consumeAudio != nil {
				pcm, err := audio.DecodeADPCM(frame.Payload)
				if err != nil {
					report.RejectedAudioFrames++
					continue
				}
				if err := consumeAudio(pcm); err != nil {
					return report, fmt.Errorf("consume PCM: %w", err)
				}
				report.AudioFrames++
				report.AudioSamples += uint64(len(pcm) / 2)
			}
			continue
		}
		canonical, err := CanonicalJPEG(frame.Payload)
		if err != nil {
			report.RejectedFrames++
			continue
		}
		frame.Payload = canonical
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if err := consume(frame.Payload); err != nil {
			return report, fmt.Errorf("consume JPEG: %w", err)
		}
		report.Frames++
	}
	if report.Frames == 0 {
		return report, ErrNoFrames
	}
	return report, nil
}

// CanonicalJPEG validates one complete image and removes at most three alignment zeros.
func CanonicalJPEG(data []byte) ([]byte, error) {
	if len(data) > 4<<20 {
		return nil, errors.New("JPEG exceeds payload limit")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 8847360 {
		return nil, errors.New("invalid JPEG dimensions")
	}
	payload := exactJPEGReader{bytes.NewReader(data)}
	if _, err := jpeg.Decode(payload); err != nil {
		return nil, errors.New("invalid JPEG image")
	}
	end := len(data) - payload.Len()
	trailing := data[end:]
	if len(trailing) > 3 {
		return nil, errors.New("invalid JPEG trailing bytes")
	}
	for _, value := range trailing {
		if value != 0 {
			return nil, errors.New("invalid JPEG trailing bytes")
		}
	}
	return data[:end], nil
}
