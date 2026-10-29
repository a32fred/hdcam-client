package export

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"testing"
)

func envelope(kind byte, payload []byte) []byte {
	h := make([]byte, 32)
	binary.LittleEndian.PutUint32(h[0:4], 0xa815aa55)
	h[4] = kind
	binary.LittleEndian.PutUint32(h[8:12], 17)
	binary.LittleEndian.PutUint32(h[12:16], 23)
	binary.LittleEndian.PutUint32(h[16:20], uint32(len(payload)))
	return append(h, payload...)
}

func jpegFixture(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type cancelAtEOFReader struct {
	r      *bytes.Reader
	cancel context.CancelFunc
}

func (r cancelAtEOFReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err == io.EOF {
		r.cancel()
	}
	return n, err
}

func setJPEGDimensions(t *testing.T, data []byte, width, height uint16) {
	t.Helper()
	for i := 2; i+8 < len(data); i++ {
		if data[i] != 0xff {
			continue
		}
		marker := data[i+1]
		if marker >= 0xc0 && marker <= 0xc3 {
			binary.BigEndian.PutUint16(data[i+5:i+7], height)
			binary.BigEndian.PutUint16(data[i+7:i+9], width)
			return
		}
	}
	t.Fatal("JPEG fixture has no supported start-of-frame marker")
}

func TestWriteMJPEGValidNonVideoAndRejectedFrames(t *testing.T) {
	valid := jpegFixture(t, 32, 24)
	data := append(envelope(3, valid), envelope(2, []byte("audio"))...)
	data = append(data, envelope(3, []byte{0xff, 0xd8, 0xff, 0x00})...)
	data = append(data, envelope(3, valid)...)
	var out bytes.Buffer
	report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
	if err != nil {
		t.Fatalf("WriteMJPEG: %v", err)
	}
	if report.Frames != 2 || report.NonVideoFrames != 1 || report.RejectedFrames != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if want := append(append([]byte(nil), valid...), valid...); !bytes.Equal(out.Bytes(), want) {
		t.Fatal("output is not the exact concatenation of accepted JPEG payloads")
	}
}

func TestWriteMJPEGRejectsJPEGTrailingDataAndContinues(t *testing.T) {
	valid := jpegFixture(t, 20, 18)
	for name, suffix := range map[string][]byte{
		"junk":         []byte("junk after EOI"),
		"second image": valid,
	} {
		t.Run(name, func(t *testing.T) {
			invalid := append(append([]byte(nil), valid...), suffix...)
			data := append(envelope(3, invalid), envelope(3, valid)...)
			var out bytes.Buffer
			report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
			if err != nil {
				t.Fatalf("valid following frame should still be exported: %v", err)
			}
			if report.RejectedFrames != 1 || report.Frames != 1 {
				t.Fatalf("unexpected report: %+v", report)
			}
			if !bytes.Equal(out.Bytes(), valid) {
				t.Fatal("only the subsequent exact JPEG payload should be emitted")
			}
		})
	}
}

func TestWriteMJPEGAcceptsOnlyUpToThreeZeroPaddingBytes(t *testing.T) {
	valid := jpegFixture(t, 20, 18)
	for padding := 1; padding <= 3; padding++ {
		t.Run(string(rune('0'+padding))+" zero bytes", func(t *testing.T) {
			payload := append(append([]byte(nil), valid...), bytes.Repeat([]byte{0}, padding)...)
			var out bytes.Buffer
			report, err := WriteMJPEG(context.Background(), bytes.NewReader(envelope(3, payload)), &out)
			if err != nil {
				t.Fatalf("JPEG with %d alignment zero bytes should be accepted: %v", padding, err)
			}
			if report.Frames != 1 || report.RejectedFrames != 0 {
				t.Fatalf("one padded envelope should count as one accepted frame: %+v", report)
			}
			if !bytes.Equal(out.Bytes(), valid) {
				t.Fatal("output should strip alignment padding and contain the canonical JPEG")
			}
		})
	}
}

func TestWriteMJPEGRejectsFourZeroPaddingBytesAndContinues(t *testing.T) {
	valid := jpegFixture(t, 20, 18)
	payload := append(append([]byte(nil), valid...), bytes.Repeat([]byte{0}, 4)...)
	data := append(envelope(3, payload), envelope(3, valid)...)
	var out bytes.Buffer
	report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
	if err != nil {
		t.Fatalf("valid following frame should still be exported: %v", err)
	}
	if report.RejectedFrames != 1 || report.Frames != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !bytes.Equal(out.Bytes(), valid) {
		t.Fatal("output should contain only the subsequent valid JPEG")
	}
}

func TestWriteMJPEGRecoversFromIncompleteFinalEnvelope(t *testing.T) {
	valid := jpegFixture(t, 16, 16)
	tail := envelope(3, valid)[:32+len(valid)-3]
	data := append(envelope(3, valid), tail...)
	var out bytes.Buffer
	report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
	if err != nil {
		t.Fatalf("incomplete final envelope should be recoverable: %v", err)
	}
	if report.Frames != 1 || report.Media.TrailingBytes != uint64(len(tail)) {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !bytes.Equal(out.Bytes(), valid) {
		t.Fatal("output differs from accepted JPEG")
	}
}

func TestWriteMJPEGBadMagicAfterValidFrameReturnsPartialReport(t *testing.T) {
	valid := jpegFixture(t, 16, 16)
	bad := envelope(3, valid)
	bad[0] ^= 1
	data := append(envelope(3, valid), bad...)
	var out bytes.Buffer
	report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
	if err == nil {
		t.Fatal("expected framing error")
	}
	if report.Frames != 1 || !bytes.Equal(out.Bytes(), valid) {
		t.Fatalf("partial result not preserved: report=%+v output=%d bytes", report, out.Len())
	}
}

func TestWriteMJPEGNoFramesReturnsSentinel(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":              nil,
		"non-video only":     envelope(2, []byte("audio")),
		"invalid video only": envelope(3, []byte{0xff, 0xd8, 0xff, 0x00}),
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
			if !errors.Is(err, ErrNoFrames) {
				t.Fatalf("want ErrNoFrames, got %v", err)
			}
			if report.Frames != 0 || out.Len() != 0 {
				t.Fatalf("unexpected result: report=%+v output=%d bytes", report, out.Len())
			}
		})
	}
}

func TestWriteMJPEGCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	_, err := WriteMJPEG(ctx, bytes.NewReader(envelope(3, jpegFixture(t, 8, 8))), &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %d bytes for canceled context", out.Len())
	}
}

func TestWriteMJPEGCancellationAtEOFReturnsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	valid := jpegFixture(t, 8, 8)
	reader := cancelAtEOFReader{r: bytes.NewReader(envelope(3, valid)), cancel: cancel}
	var out bytes.Buffer
	report, err := WriteMJPEG(ctx, reader, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled after EOF, got %v", err)
	}
	if report.Frames != 1 || !bytes.Equal(out.Bytes(), valid) {
		t.Fatalf("expected prior accepted frame to remain in partial result: report=%+v", report)
	}
}

func TestWriteMJPEGWriterErrorsAndShortWrites(t *testing.T) {
	data := bytes.NewReader(envelope(3, jpegFixture(t, 12, 12)))
	for name, w := range map[string]io.Writer{"error": failingWriter{}, "short write": shortWriter{}} {
		t.Run(name, func(t *testing.T) {
			r, err := WriteMJPEG(context.Background(), data, w)
			if err == nil {
				t.Fatal("expected writer failure")
			}
			if r.Frames != 0 {
				t.Fatalf("incomplete write counted as accepted: %+v", r)
			}
		})
		data.Reset(envelope(3, jpegFixture(t, 12, 12)))
	}
}

func TestWriteMJPEGRejectsHugeJPEGDimensionsAndContinues(t *testing.T) {
	oversized := jpegFixture(t, 5000, 2)
	valid := jpegFixture(t, 8, 8)
	data := append(envelope(3, oversized), envelope(3, valid)...)
	var out bytes.Buffer
	report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
	if err != nil {
		t.Fatalf("valid frame after oversized image should be exported: %v", err)
	}
	if report.RejectedFrames != 1 || report.Frames != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !bytes.Equal(out.Bytes(), valid) {
		t.Fatal("output does not contain only the valid JPEG")
	}
}

func TestWriteMJPEGRejectsPixelCountLimitAndContinues(t *testing.T) {
	oversized := jpegFixture(t, 8, 8)
	setJPEGDimensions(t, oversized, 4096, 2161) // Dimensions fit individually, but their pixel count exceeds the limit.
	valid := jpegFixture(t, 8, 8)
	data := append(envelope(3, oversized), envelope(3, valid)...)
	var out bytes.Buffer
	report, err := WriteMJPEG(context.Background(), bytes.NewReader(data), &out)
	if err != nil {
		t.Fatalf("valid frame after oversized image should be exported: %v", err)
	}
	if report.RejectedFrames != 1 || report.Frames != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !bytes.Equal(out.Bytes(), valid) {
		t.Fatal("output does not contain only the valid JPEG")
	}
}
