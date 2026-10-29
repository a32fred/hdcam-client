package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// WAVWriter writes PCM16 mono at 8 kHz and finalizes its header on Close.
// It does not close the underlying writer.
type WAVWriter struct {
	w       io.WriteSeeker
	written uint64
	failed  error
	closed  bool
}

func streamHeader(size uint32) []byte {
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], size+36)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 1)
	binary.LittleEndian.PutUint32(h[24:], 8000)
	binary.LittleEndian.PutUint32(h[28:], 16000)
	binary.LittleEndian.PutUint16(h[32:], 2)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], size)
	return h
}

// NewWAV starts a WAV file at offset zero of w.
func NewWAV(w io.WriteSeeker) (*WAVWriter, error) {
	if w == nil {
		return nil, errors.New("nil WAV writer")
	}
	if _, err := w.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek WAV start: %w", err)
	}
	h := streamHeader(0)
	n, err := w.Write(h)
	if err != nil {
		return nil, fmt.Errorf("write WAV header: %w", err)
	}
	if n != len(h) {
		return nil, io.ErrShortWrite
	}
	return &WAVWriter{w: w}, nil
}

func (w *WAVWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("WAV writer is closed")
	}
	if w.failed != nil {
		return 0, w.failed
	}
	if len(p)%2 != 0 {
		w.failed = errors.New("PCM data ends with an incomplete sample")
		return 0, w.failed
	}
	if uint64(len(p)) > uint64(^uint32(0))-36-w.written {
		w.failed = errors.New("PCM data exceeds RIFF/WAVE size limit")
		return 0, w.failed
	}
	n, err := w.w.Write(p)
	if n < 0 || n > len(p) {
		w.failed = errors.New("invalid WAV write count")
		return 0, w.failed
	}
	w.written += uint64(n)
	if err != nil {
		w.failed = err
	} else if n != len(p) {
		w.failed = io.ErrShortWrite
	}
	return n, w.failed
}

func (w *WAVWriter) Close() error {
	if w.closed {
		return w.failed
	}
	w.closed = true
	if w.failed != nil {
		return w.failed
	}
	if _, err := w.w.Seek(0, io.SeekStart); err != nil {
		w.failed = err
		return err
	}
	h := streamHeader(uint32(w.written))
	n, err := w.w.Write(h)
	if err != nil {
		w.failed = err
		return err
	}
	if n != len(h) {
		w.failed = io.ErrShortWrite
		return w.failed
	}
	_, w.failed = w.w.Seek(0, io.SeekEnd)
	return w.failed
}
