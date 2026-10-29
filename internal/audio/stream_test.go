package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
)

func TestStreamingWAVHeaderAndLifecycle(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "audio")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := NewWAV(f)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) != 44 || binary.LittleEndian.Uint32(initial[4:]) != 36 || binary.LittleEndian.Uint32(initial[40:]) != 0 {
		t.Fatalf("initial header %x", initial)
	}
	payload := []byte{1, 2, 3, 4}
	if n, err := w.Write(payload); err != nil || n != 4 {
		t.Fatalf("write %d %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(final) != 48 || !bytes.Equal(final[44:], payload) || binary.LittleEndian.Uint32(final[4:]) != 40 || binary.LittleEndian.Uint32(final[40:]) != 4 {
		t.Fatalf("final %x", final)
	}
	var reference bytes.Buffer
	if err := WriteWAV(&reference, payload, PCM16Mono8kHz); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(final, reference.Bytes()) {
		t.Fatal("header format mismatch")
	}
	if pos, err := f.Seek(0, io.SeekCurrent); err != nil || pos != 48 {
		t.Fatalf("position %d %v", pos, err)
	}
	if _, err := w.Write(payload); err == nil {
		t.Fatal("accepted after close")
	}
	if _, err := f.Write(nil); err != nil {
		t.Fatalf("underlying closed: %v", err)
	}
}

type wavFault struct {
	data     []byte
	writes   int
	seeks    int
	short    bool
	writeErr error
	seekErr  error
}

func (f *wavFault) Write(p []byte) (int, error) {
	f.writes++
	if f.short {
		return len(p) / 2, nil
	}
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.data = append(f.data, p...)
	return len(p), nil
}
func (f *wavFault) Seek(int64, int) (int64, error) { f.seeks++; return 0, f.seekErr }

func TestStreamingWAVLatchesWriteFailure(t *testing.T) {
	for _, short := range []bool{true, false} {
		f := &wavFault{}
		w, err := NewWAV(f)
		if err != nil {
			t.Fatal(err)
		}
		f.short = short
		if !short {
			f.writeErr = errors.New("disk failure")
		}
		_, failure := w.Write([]byte{1, 2})
		if failure == nil {
			t.Fatal("missing failure")
		}
		calls := f.writes
		seeks := f.seeks
		if err := w.Close(); !errors.Is(err, failure) {
			t.Fatalf("close error %v", err)
		}
		if err := w.Close(); !errors.Is(err, failure) {
			t.Fatalf("repeat close error %v", err)
		}
		if f.writes != calls || f.seeks != seeks {
			t.Fatal("finalized failed stream")
		}
	}
}

func TestStreamingWAVRejectsSamplesAndOverflowBeforeWriting(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		f := &wavFault{}
		w, err := NewWAV(f)
		if err != nil {
			t.Fatal(err)
		}
		p := []byte{1}
		if overflow {
			w.written = uint64(^uint32(0)) - 37
			p = []byte{1, 2}
		}
		calls := f.writes
		if _, err := w.Write(p); err == nil {
			t.Fatal("accepted invalid payload")
		}
		if f.writes != calls {
			t.Fatal("wrote invalid payload")
		}
		if err := w.Close(); err == nil {
			t.Fatal("finalized invalid stream")
		}
	}
}

func TestStreamingWAVSeekAndHeaderFailures(t *testing.T) {
	failure := errors.New("seek failure")
	if _, err := NewWAV(&wavFault{seekErr: failure}); !errors.Is(err, failure) {
		t.Fatalf("new seek %v", err)
	}
	if _, err := NewWAV(&wavFault{short: true}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("new short %v", err)
	}
	f := &wavFault{}
	w, err := NewWAV(f)
	if err != nil {
		t.Fatal(err)
	}
	f.seekErr = failure
	calls := f.writes
	if err := w.Close(); !errors.Is(err, failure) {
		t.Fatalf("close seek %v", err)
	}
	if f.writes != calls {
		t.Fatal("wrote header after failed seek")
	}
}
