package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	headerSize               = 32
	defaultMaxPayload        = 4 << 20
	frameMagic        uint32 = 0xa815aa55
)

var ErrTruncated = errors.New("truncated frame envelope")
var ErrFormat = errors.New("invalid media envelope")

type Frame struct {
	Type      uint8
	StreamID  uint8
	Timestamp uint32
	Sequence  uint32
	Payload   []byte
}

type Stats struct {
	Frames                       uint64 `json:"frames"`
	Bytes                        uint64 `json:"bytes"`
	TrailingBytes                uint64 `json:"trailing_bytes"`
	VideoSequenceDiscontinuities uint64 `json:"video_sequence_discontinuities"`
	VideoCounterGapIDs           uint64 `json:"video_counter_gap_ids"`
}

// Reader consumes sequential 32-byte frame envelopes. It is not concurrent-safe.
type Reader struct {
	r          *deferredErrorReader
	maxPayload uint32
	stats      Stats
	prevSeq    uint32
	hasPrevSeq bool
	terminal   error
}

// deferredErrorReader preserves an error returned alongside data. ReadFull
// otherwise loses that error when the data fills its buffer exactly.
type deferredErrorReader struct {
	r   io.Reader
	err error
}

func (r *deferredErrorReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.r.Read(p)
	if n > 0 && err != nil {
		r.err = err
		return n, nil
	}
	return n, err
}

func (r *deferredErrorReader) pendingError() error { return r.err }

func NewReader(r io.Reader, maxPayload uint32) *Reader {
	if maxPayload == 0 {
		maxPayload = defaultMaxPayload
	}
	return &Reader{r: &deferredErrorReader{r: r}, maxPayload: maxPayload}
}

func (r *Reader) Statistics() Stats { return r.stats }

func (r *Reader) Next() (Frame, error) {
	if r.terminal != nil {
		return Frame{}, r.terminal
	}

	var header [headerSize]byte
	n, err := io.ReadFull(r.r, header[:])
	r.stats.Bytes += uint64(n)
	if err != nil {
		if err == io.EOF && n == 0 {
			r.terminal = io.EOF
			return Frame{}, io.EOF
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, r.truncated(uint64(n))
		}
		r.stats.TrailingBytes = uint64(n)
		return Frame{}, r.fail(fmt.Errorf("read frame header: %w", err))
	}
	if magic := binary.LittleEndian.Uint32(header[:4]); magic != frameMagic {
		r.stats.TrailingBytes = headerSize
		return Frame{}, r.fail(fmt.Errorf("%w: invalid frame magic: %#08x", ErrFormat, magic))
	}
	payloadLen := binary.LittleEndian.Uint32(header[16:20])
	if payloadLen > r.maxPayload {
		r.stats.TrailingBytes = headerSize
		return Frame{}, r.fail(fmt.Errorf("%w: payload length %d exceeds maximum %d", ErrFormat, payloadLen, r.maxPayload))
	}
	if err := r.r.pendingError(); err != nil && !errors.Is(err, io.EOF) {
		r.stats.TrailingBytes = headerSize
		return Frame{}, r.fail(fmt.Errorf("read frame header: %w", err))
	}

	payload := make([]byte, int(payloadLen))
	n, err = io.ReadFull(r.r, payload)
	r.stats.Bytes += uint64(n)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, r.truncated(headerSize + uint64(n))
		}
		r.stats.TrailingBytes = headerSize + uint64(n)
		return Frame{}, r.fail(fmt.Errorf("read frame payload: %w", err))
	}

	r.stats.TrailingBytes = 0
	f := Frame{
		Type:      header[4],
		StreamID:  header[5],
		Timestamp: binary.LittleEndian.Uint32(header[8:12]),
		Sequence:  binary.LittleEndian.Uint32(header[12:16]),
		Payload:   payload,
	}
	r.stats.Frames++
	if f.Type == 3 {
		if r.hasPrevSeq {
			delta := f.Sequence - r.prevSeq
			if delta != 1 {
				r.stats.VideoSequenceDiscontinuities++
				if delta > 1 && delta < 0x80000000 {
					r.stats.VideoCounterGapIDs += uint64(delta - 1)
				}
			}
		}
		r.prevSeq, r.hasPrevSeq = f.Sequence, true
	}
	return f, nil
}

func (r *Reader) truncated(n uint64) error {
	r.stats.TrailingBytes = n
	return r.fail(ErrTruncated)
}

func (r *Reader) fail(err error) error {
	r.terminal = err
	return err
}
