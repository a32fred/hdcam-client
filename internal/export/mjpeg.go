package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"io"

	"example.com/hdcam-client/internal/media"
)

var ErrNoFrames = errors.New("no valid video frames")

type Report struct {
	Frames         uint64      `json:"frames"`
	NonVideoFrames uint64      `json:"non_video_frames"`
	RejectedFrames uint64      `json:"rejected_frames"`
	Media          media.Stats `json:"media"`
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
	reader := media.NewReader(source, 0)
	defer func() { report.Media = reader.Statistics() }()
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		frame, err := reader.Next()
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
			continue
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(frame.Payload))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 8847360 {
			report.RejectedFrames++
			continue
		}
		payload := exactJPEGReader{bytes.NewReader(frame.Payload)}
		if _, err := jpeg.Decode(payload); err != nil {
			report.RejectedFrames++
			continue
		}
		end := len(frame.Payload) - payload.Len()
		trailing := frame.Payload[end:]
		validPadding := len(trailing) <= 3
		for _, value := range trailing {
			if value != 0 {
				validPadding = false
				break
			}
		}
		if !validPadding {
			report.RejectedFrames++
			continue
		}
		frame.Payload = frame.Payload[:end]
		if err := ctx.Err(); err != nil {
			return report, err
		}
		written, err := destination.Write(frame.Payload)
		if err != nil {
			return report, fmt.Errorf("write MJPEG: %w", err)
		}
		if written != len(frame.Payload) {
			return report, io.ErrShortWrite
		}
		report.Frames++
	}
	if report.Frames == 0 {
		return report, ErrNoFrames
	}
	return report, nil
}
