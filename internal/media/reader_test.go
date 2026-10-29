package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func fixture(kind byte, seq uint32, payload []byte) []byte {
	h := make([]byte, 32)
	binary.LittleEndian.PutUint32(h, 0xa815aa55)
	h[4], h[5] = kind, 3
	binary.LittleEndian.PutUint32(h[8:], 123)
	binary.LittleEndian.PutUint32(h[12:], seq)
	binary.LittleEndian.PutUint32(h[16:], uint32(len(payload)))
	return append(h, payload...)
}

func TestReaderFramingAndCounters(t *testing.T) {
	payload := []byte("\xff\xd8\xffinside\xff\xd9still-payload")
	data := fixture(3, ^uint32(0), payload)
	data = append(data, fixture(11, 99, []byte{1, 2})...)
	data = append(data, fixture(3, 0, []byte{3})...)
	data = append(data, fixture(3, 3, []byte{4})...)
	r := NewReader(bytes.NewReader(data), 0)
	f, err := r.Next()
	if err != nil || !bytes.Equal(f.Payload, payload) || f.Type != 3 || f.StreamID != 3 || f.Timestamp != 123 {
		t.Fatalf("frame mismatch: %+v %v", f, err)
	}
	for range 3 {
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF: %v", err)
	}
	s := r.Statistics()
	if s.Frames != 4 || s.Bytes != uint64(len(data)) || s.VideoSequenceDiscontinuities != 1 || s.VideoCounterGapIDs != 2 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestReaderTruncationAndTerminalError(t *testing.T) {
	for _, data := range [][]byte{fixture(3, 1, []byte{1})[:12], fixture(3, 1, []byte{1, 2})[:33]} {
		r := NewReader(bytes.NewReader(data), 0)
		if _, err := r.Next(); !errors.Is(err, ErrTruncated) {
			t.Fatalf("want truncated: %v", err)
		}
		if _, err := r.Next(); !errors.Is(err, ErrTruncated) {
			t.Fatalf("terminal error lost: %v", err)
		}
		if r.Statistics().TrailingBytes != uint64(len(data)) {
			t.Fatalf("tail: %+v", r.Statistics())
		}
		if r.Statistics().Bytes != uint64(len(data)) {
			t.Fatalf("consumed bytes lost: %+v", r.Statistics())
		}
	}
}

func TestReaderAcceptsFragmentedReads(t *testing.T) {
	data := append(fixture(3, 1, []byte{1, 2, 3}), fixture(3, 2, []byte{4})...)
	r := NewReader(iotest.OneByteReader(bytes.NewReader(data)), 0)
	for range 2 {
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if s := r.Statistics(); s.Frames != 2 || s.Bytes != uint64(len(data)) || s.TrailingBytes != 0 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestReaderPreservesSourceError(t *testing.T) {
	wanted := errors.New("read failed")
	r := NewReader(iotest.ErrReader(wanted), 0)
	for range 2 {
		if _, err := r.Next(); !errors.Is(err, wanted) || errors.Is(err, ErrTruncated) {
			t.Fatalf("source error masked: %v", err)
		}
	}
}

func TestReaderRejectsInvalidEnvelope(t *testing.T) {
	bad := fixture(3, 1, nil)
	bad[0] ^= 1
	large := fixture(3, 1, nil)
	binary.LittleEndian.PutUint32(large[16:], ^uint32(0))
	for _, data := range [][]byte{bad, large} {
		r := NewReader(bytes.NewReader(data), 0)
		if _, err := r.Next(); err == nil || errors.Is(err, ErrTruncated) {
			t.Fatalf("want format error: %v", err)
		}
		if r.Statistics().TrailingBytes != 32 {
			t.Fatalf("unaccepted header not reported: %+v", r.Statistics())
		}
	}
	r := NewReader(bytes.NewReader(fixture(3, 1, []byte{1, 2, 3})), 2)
	if _, err := r.Next(); err == nil {
		t.Fatal("custom payload limit ignored")
	}
}

type boundaryErrorReader struct {
	data               []byte
	boundary, consumed int
	err                error
}

func (r *boundaryErrorReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	r.consumed += n
	if r.consumed == r.boundary {
		return n, r.err
	}
	return n, nil
}

func TestReaderPreservesErrorAtExactBoundary(t *testing.T) {
	data := fixture(3, 1, []byte{1, 2, 3})
	wanted := errors.New("source failed at boundary")
	for _, boundary := range []int{32, len(data)} {
		r := NewReader(&boundaryErrorReader{data: data, boundary: boundary, err: wanted}, 0)
		_, err := r.Next()
		if err == nil {
			_, err = r.Next()
		}
		if !errors.Is(err, wanted) {
			t.Fatalf("boundary %d lost source error: %v", boundary, err)
		}
	}
}

func TestFullFinalHeaderWithEOF(t *testing.T) {
	empty := fixture(3, 1, nil)
	r := NewReader(&boundaryErrorReader{data: empty, boundary: 32, err: io.EOF}, 0)
	if _, err := r.Next(); err != nil {
		t.Fatalf("complete empty envelope rejected: %v", err)
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	bad := fixture(3, 1, nil)
	bad[0] ^= 1
	r = NewReader(&boundaryErrorReader{data: bad, boundary: 32, err: io.EOF}, 0)
	if _, err := r.Next(); err == nil || errors.Is(err, ErrTruncated) || errors.Is(err, io.EOF) {
		t.Fatalf("invalid header became recoverable: %v", err)
	}
}

func TestReaderDuplicateAndBacktrack(t *testing.T) {
	data := append(fixture(3, 10, nil), fixture(3, 10, nil)...)
	data = append(data, fixture(3, 9, nil)...)
	r := NewReader(bytes.NewReader(data), 0)
	for range 3 {
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
	}
	s := r.Statistics()
	if s.VideoSequenceDiscontinuities != 2 || s.VideoCounterGapIDs != 0 {
		t.Fatalf("stats: %+v", s)
	}
}
