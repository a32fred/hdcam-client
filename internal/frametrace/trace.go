package frametrace

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
)

const maxEntries = 1000000

type Entry struct {
	MonotonicNS  int64  `json:"monotonic_ns"`
	Type         uint8  `json:"type"`
	StreamID     uint8  `json:"stream_id"`
	Sequence     uint32 `json:"sequence"`
	RawTime      uint32 `json:"raw_time_u32"`
	PayloadBytes uint32 `json:"payload_bytes"`
}
type Summary struct {
	Entries                 uint64  `json:"entries"`
	VideoFrames             uint64  `json:"video_frames"`
	FirstVideoNS            int64   `json:"first_video_ns"`
	LastVideoNS             int64   `json:"last_video_ns"`
	ReceivedFPS             float64 `json:"received_fps"`
	SequenceDiscontinuities uint64  `json:"sequence_discontinuities"`
	MissingSequenceIDs      uint64  `json:"missing_sequence_ids"`
	Duplicates              uint64  `json:"duplicates"`
	Backtracks              uint64  `json:"backtracks"`
	RawTimeConstant         bool    `json:"raw_time_constant"`
	GapMedianMS             float64 `json:"gap_median_ms"`
	GapP95MS                float64 `json:"gap_p95_ms"`
	GapMaxMS                float64 `json:"gap_max_ms"`
}

// Writer is serial and does not own or flush its destination.
type Writer struct {
	dst         io.Writer
	failed      error
	summary     Summary
	lastArrival int64
	sequence    uint32
	rawTime     uint32
	gaps        []int64
}

func New(dst io.Writer) *Writer {
	w := &Writer{dst: dst}
	if dst == nil {
		w.failed = errors.New("trace destination required")
	}
	return w
}
func (w *Writer) Record(e Entry) error {
	if w.failed != nil {
		return w.failed
	}
	if e.MonotonicNS < 0 || (w.summary.Entries > 0 && e.MonotonicNS < w.lastArrival) {
		w.failed = errors.New("invalid monotonic trace order")
		return w.failed
	}
	if w.summary.Entries >= maxEntries {
		w.failed = errors.New("trace record limit reached")
		return w.failed
	}
	data, err := json.Marshal(e)
	if err != nil {
		w.failed = errors.New("trace encoding failed")
		return w.failed
	}
	data = append(data, '\n')
	n, err := w.dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.failed = err
		return err
	}
	w.summary.Entries++
	w.lastArrival = e.MonotonicNS
	if e.Type != 3 {
		return nil
	}
	if w.summary.VideoFrames == 0 {
		w.summary.FirstVideoNS = e.MonotonicNS
		w.summary.RawTimeConstant = true
		w.rawTime = e.RawTime
	} else {
		w.gaps = append(w.gaps, e.MonotonicNS-w.summary.LastVideoNS)
		delta := e.Sequence - w.sequence
		if delta != 1 {
			w.summary.SequenceDiscontinuities++
			switch {
			case delta == 0:
				w.summary.Duplicates++
			case delta >= 0x80000000:
				w.summary.Backtracks++
			default:
				w.summary.MissingSequenceIDs += uint64(delta - 1)
			}
		}
		if e.RawTime != w.rawTime {
			w.summary.RawTimeConstant = false
		}
	}
	w.summary.VideoFrames++
	w.summary.LastVideoNS = e.MonotonicNS
	w.sequence = e.Sequence
	return nil
}
func (w *Writer) Summary() Summary {
	s := w.summary
	if s.VideoFrames > 1 && s.LastVideoNS > s.FirstVideoNS {
		s.ReceivedFPS = float64(s.VideoFrames-1) * 1e9 / float64(s.LastVideoNS-s.FirstVideoNS)
	}
	if len(w.gaps) == 0 {
		return s
	}
	sorted := append([]int64(nil), w.gaps...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	middle := len(sorted) / 2
	s.GapMedianMS = float64(sorted[middle]) / 1e6
	if len(sorted)%2 == 0 {
		s.GapMedianMS = (float64(sorted[middle-1]) + float64(sorted[middle])) / 2e6
	}
	rank := int(math.Ceil(float64(len(sorted))*.95)) - 1
	s.GapP95MS = float64(sorted[rank]) / 1e6
	s.GapMaxMS = float64(sorted[len(sorted)-1]) / 1e6
	return s
}
