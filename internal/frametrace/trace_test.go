package frametrace

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

func TestRecordJSONLNumericRoundTrip(t *testing.T) {
	want := Entry{MonotonicNS: 123456789, Type: 3, StreamID: 7, Sequence: 4294967295, RawTime: 4000000000, PayloadBytes: 1234}
	var output bytes.Buffer
	w := New(&output)
	if err := w.Record(want); err != nil {
		t.Fatal(err)
	}
	encoded := output.String()
	scanner := bufio.NewScanner(strings.NewReader(encoded))
	if !scanner.Scan() {
		t.Fatal("missing JSONL row")
	}
	var got Entry
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want || scanner.Scan() || scanner.Err() != nil {
		t.Fatalf("decoded entry=%+v want=%+v; output=%q", got, want, output.String())
	}
	if !strings.HasSuffix(encoded, "\n") {
		t.Fatalf("JSONL row lacks newline: %q", encoded)
	}
	if !strings.Contains(encoded, `"sequence":4294967295`) || strings.Contains(encoded, `"sequence":"`) {
		t.Fatalf("sequence was not encoded as a JSON number: %s", encoded)
	}
}

func TestSummarySteadyTenFPSStartsAtZero(t *testing.T) {
	w := New(io.Discard)
	for i := 0; i < 11; i++ {
		if err := w.Record(Entry{MonotonicNS: int64(i) * 100_000_000, Type: 3, Sequence: uint32(i)}); err != nil {
			t.Fatal(err)
		}
	}
	s := w.Summary()
	if s.FirstVideoNS != 0 || s.LastVideoNS != 1_000_000_000 || math.Abs(s.ReceivedFPS-10) > 1e-9 {
		t.Fatalf("unexpected 10fps summary: %+v", s)
	}
}

func TestAudioSequenceDoesNotAffectVideoSequenceStats(t *testing.T) {
	w := New(io.Discard)
	entries := []Entry{
		{MonotonicNS: 0, Type: 3, Sequence: 10},
		{MonotonicNS: 5, Type: 11, Sequence: 9000},
		{MonotonicNS: 10, Type: 3, Sequence: 11},
	}
	for _, entry := range entries {
		if err := w.Record(entry); err != nil {
			t.Fatal(err)
		}
	}
	s := w.Summary()
	if s.Entries != 3 || s.VideoFrames != 2 || s.SequenceDiscontinuities != 0 || s.MissingSequenceIDs != 0 {
		t.Fatalf("audio contaminated video stats: %+v", s)
	}
}

func TestSequenceWrapFromMaxToZeroIsContinuous(t *testing.T) {
	w := New(io.Discard)
	for _, entry := range []Entry{
		{MonotonicNS: 0, Type: 3, Sequence: math.MaxUint32},
		{MonotonicNS: 1, Type: 3, Sequence: 0},
	} {
		if err := w.Record(entry); err != nil {
			t.Fatal(err)
		}
	}
	s := w.Summary()
	if s.VideoFrames != 2 || s.SequenceDiscontinuities != 0 || s.MissingSequenceIDs != 0 || s.Duplicates != 0 || s.Backtracks != 0 {
		t.Fatalf("sequence wrap was not continuous: %+v", s)
	}
}

func TestSequenceAnomaliesAreLabeledWithoutChangingFrameCount(t *testing.T) {
	w := New(io.Discard)
	for i, sequence := range []uint32{10, 11, 11, 9, 12} {
		if err := w.Record(Entry{MonotonicNS: int64(i), Type: 3, Sequence: sequence}); err != nil {
			t.Fatal(err)
		}
	}
	s := w.Summary()
	if s.VideoFrames != 5 || s.Entries != 5 || s.SequenceDiscontinuities != 3 || s.Duplicates != 1 || s.Backtracks != 1 || s.MissingSequenceIDs != 2 {
		t.Fatalf("unexpected sequence anomaly summary: %+v", s)
	}
}

func TestRawTimeChangeOnSecondVideoFrameIsNotConstant(t *testing.T) {
	w := New(io.Discard)
	for i, rawTime := range []uint32{100, 101} {
		if err := w.Record(Entry{MonotonicNS: int64(i), Type: 3, Sequence: uint32(i), RawTime: rawTime}); err != nil {
			t.Fatal(err)
		}
	}
	if s := w.Summary(); s.RawTimeConstant {
		t.Fatalf("raw time changed but summary says constant: %+v", s)
	}
}

func TestGapStatisticsUseMedianNearestRankP95AndMaximum(t *testing.T) {
	w := New(io.Discard)
	var arrival int64
	if err := w.Record(Entry{MonotonicNS: 0, Type: 3, Sequence: 0}); err != nil {
		t.Fatal(err)
	}
	for i, gapMS := range []int64{10, 20, 30, 40} {
		arrival += gapMS * 1_000_000
		if err := w.Record(Entry{MonotonicNS: arrival, Type: 3, Sequence: uint32(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	s := w.Summary()
	if s.GapMedianMS != 25 || s.GapP95MS != 40 || s.GapMaxMS != 40 {
		t.Fatalf("unexpected gap stats: %+v", s)
	}
}

func TestSummaryDoesNotMutateGapIntervals(t *testing.T) {
	w := New(io.Discard)
	for i, arrival := range []int64{0, 10_000_000, 30_000_000, 60_000_000, 100_000_000} {
		if err := w.Record(Entry{MonotonicNS: arrival, Type: 3, Sequence: uint32(i)}); err != nil {
			t.Fatal(err)
		}
	}
	before := append([]int64(nil), w.gaps...)
	first, second := w.Summary(), w.Summary()
	if first != second || !equalInt64s(w.gaps, before) {
		t.Fatalf("Summary changed intervals or result: first=%+v second=%+v gaps=%v", first, second, w.gaps)
	}
}

func equalInt64s(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestInvalidArrivalTimesLatchError(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []Entry
	}{
		{name: "negative", entries: []Entry{{MonotonicNS: -1, Type: 3}}},
		{name: "decreasing", entries: []Entry{{MonotonicNS: 10, Type: 3}, {MonotonicNS: 9, Type: 3}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := New(io.Discard)
			var first error
			for i, entry := range test.entries {
				err := w.Record(entry)
				if i == len(test.entries)-1 {
					first = err
				}
			}
			if first == nil {
				t.Fatal("invalid time was accepted")
			}
			if later := w.Record(Entry{MonotonicNS: 100, Type: 3}); later != first {
				t.Fatalf("error was not latched: first=%v later=%v", first, later)
			}
		})
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestWriterErrorsAndShortWriteAreLatched(t *testing.T) {
	wantErr := errors.New("disk failed")
	for name, dst := range map[string]io.Writer{
		"writer error":         errorWriter{err: wantErr},
		"zero nil short write": zeroWriter{},
	} {
		t.Run(name, func(t *testing.T) {
			w := New(dst)
			first := w.Record(Entry{Type: 3})
			if first == nil {
				t.Fatal("expected write failure")
			}
			if name == "writer error" && !errors.Is(first, wantErr) {
				t.Fatalf("want underlying writer error, got %v", first)
			}
			if name == "zero nil short write" && !errors.Is(first, io.ErrShortWrite) {
				t.Fatalf("want io.ErrShortWrite, got %v", first)
			}
			if later := w.Record(Entry{MonotonicNS: 1}); later != first {
				t.Fatalf("error was not latched: first=%v later=%v", first, later)
			}
			if s := w.Summary(); s.Entries != 0 || s.VideoFrames != 0 {
				t.Fatalf("failed write changed summary: %+v", s)
			}
		})
	}
}

func TestRecordLimitRejectsAndLatches(t *testing.T) {
	w := New(io.Discard)
	w.summary.Entries = maxEntries
	first := w.Record(Entry{MonotonicNS: 1})
	if first == nil || !strings.Contains(first.Error(), "limit") {
		t.Fatalf("expected record limit error, got %v", first)
	}
	if later := w.Record(Entry{MonotonicNS: 2}); later != first {
		t.Fatalf("limit error was not latched: first=%v later=%v", first, later)
	}
}

func TestNoVideoHasFalseRawTimeConstant(t *testing.T) {
	w := New(io.Discard)
	if err := w.Record(Entry{MonotonicNS: 0, Type: 11}); err != nil {
		t.Fatal(err)
	}
	if s := w.Summary(); s.VideoFrames != 0 || s.RawTimeConstant {
		t.Fatalf("audio-only trace should report no constant video raw time: %+v", s)
	}
}

func TestNilDestinationReturnsErrorWithoutPanic(t *testing.T) {
	w := New(nil)
	if err := w.Record(Entry{Type: 3}); err == nil {
		t.Fatal("nil destination accepted")
	}
}
