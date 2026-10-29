package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestWriteWAVHeaderAndPCM(t *testing.T) {
	pcm := []byte{0x34, 0x12, 0xcc, 0xed}
	var out bytes.Buffer
	if err := WriteWAV(&out, pcm, PCM16Mono8kHz); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	if len(b) != 44+len(pcm) {
		t.Fatalf("WAV length = %d, want %d", len(b), 44+len(pcm))
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[12:16]) != "fmt " || string(b[36:40]) != "data" {
		t.Fatalf("invalid WAV chunk markers: %q %q %q %q", b[:4], b[8:12], b[12:16], b[36:40])
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != uint32(len(pcm)+36) {
		t.Errorf("RIFF size = %d, want %d", got, len(pcm)+36)
	}
	if got := binary.LittleEndian.Uint16(b[20:22]); got != 1 {
		t.Errorf("format tag = %d, want PCM (1)", got)
	}
	if got := binary.LittleEndian.Uint32(b[24:28]); got != 8000 {
		t.Errorf("sample rate = %d, want 8000", got)
	}
	if got := binary.LittleEndian.Uint32(b[28:32]); got != 16000 {
		t.Errorf("byte rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint16(b[32:34]); got != 2 {
		t.Errorf("block align = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint16(b[34:36]); got != 16 {
		t.Errorf("bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(b[40:44]); got != uint32(len(pcm)) {
		t.Errorf("data size = %d, want %d", got, len(pcm))
	}
	if !bytes.Equal(b[44:], pcm) {
		t.Errorf("PCM payload = %x, want %x", b[44:], pcm)
	}
}

func TestWriteWAVRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		pcm    []byte
		format Format
	}{
		{"unsupported bit depth", []byte{0, 0}, Format{SampleRate: 8000, Channels: 1, BitsPerSample: 8}},
		{"zero sample rate", []byte{0, 0}, Format{Channels: 1, BitsPerSample: 16}},
		{"incomplete sample frame", []byte{0}, PCM16Mono8kHz},
		{"incomplete multichannel frame", []byte{0, 0, 0, 0, 0, 0}, Format{SampleRate: 48000, Channels: 2, BitsPerSample: 16}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteWAV(&out, tt.pcm, tt.format); err == nil {
				t.Fatal("WriteWAV succeeded, want validation error")
			}
		})
	}
}

type failingWriter struct{ calls int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return 0, nil
	}
	return 0, io.ErrClosedPipe
}

func TestWriteWAVPropagatesWriterFailure(t *testing.T) {
	err := WriteWAV(&failingWriter{}, []byte{0, 0}, PCM16Mono8kHz)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}
