package archive

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRoot(t *testing.T) string { t.Helper(); p := filepath.Join(t.TempDir(), "archive"); return p }
func TestWriterAVRolloverAndResume(t *testing.T) {
	p := testRoot(t)
	opts := Options{MaxFrames: 2, MaxBytes: 100000, Keep: 2, Audio: true}
	w, e := Open(p, opts)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.WriteAudio([]byte{0, 0}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e = w.WriteVideo([]byte{0xff, 0xd8, byte(i), 0xff, 0xd9}); e != nil {
			t.Fatal(e)
		}
		if e = w.WriteAudio([]byte{1, 0, 2, 0}); e != nil {
			t.Fatal(e)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	w, e = Open(p, opts)
	if e != nil {
		t.Fatal(e)
	}
	if len(w.entries) != 2 {
		t.Fatalf("segments %d", len(w.entries))
	}
	m := w.entries[0].meta
	if m.Frames != 2 || m.AudioSamples != 4 || m.VideoMS[1] < 1 {
		t.Fatalf("metadata %+v", m)
	}
	b, e := os.ReadFile(filepath.Join(w.entries[0].path, "audio.wav"))
	if e != nil {
		t.Fatal(e)
	}
	if string(b[:4]) != "RIFF" || binary.LittleEndian.Uint32(b[40:44]) != 8 {
		t.Fatal("invalid WAV")
	}
	if e = w.WriteVideo([]byte{1}); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	w, e = Open(p, opts)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if len(w.entries) != 2 {
		t.Fatal("restart retention failed")
	}
}
func TestLockAndUnsafeRoots(t *testing.T) {
	p := testRoot(t)
	w, e := Open(p, Options{1, 10000, 1, false})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Open(p, Options{1, 10000, 1, false}); e == nil {
		t.Fatal("second writer acquired lock")
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0755); e != nil {
		t.Fatal(e)
	}
	if _, e = Open(p, Options{1, 10000, 1, false}); e == nil {
		t.Fatal("unsafe permissions accepted")
	}
}
func TestBudgetFailurePreservesPartial(t *testing.T) {
	p := testRoot(t)
	w, e := Open(p, Options{10, 600, 2, true})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.WriteVideo([]byte{1}); e != nil {
		t.Fatal(e)
	}
	part := w.part
	if e = w.WriteVideo(make([]byte, 601)); e == nil {
		t.Fatal("oversize accepted")
	}
	if e = w.Close(); e == nil {
		t.Fatal("failure not returned")
	}
	if _, e = os.Stat(part); e != nil {
		t.Fatal(e)
	}
	if len(w.entries) != 0 {
		t.Fatal("failed segment promoted")
	}
	if _, ok := writerInspect(p, filepath.Base(part)); ok {
		t.Fatal("partial valid")
	}
}
func TestByteCapIncludesIndexAndRetentionLeavesUnknown(t *testing.T) {
	p := testRoot(t)
	w, e := Open(p, Options{1, 700, 1, false})
	if e != nil {
		t.Fatal(e)
	}
	unknown := filepath.Join(p, "segment-unrelated")
	if e = os.Mkdir(unknown, 0700); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	if e = os.Symlink(outside, filepath.Join(p, "segment-link")); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		if e = w.WriteVideo(make([]byte, 150)); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	var total int64
	for _, ent := range w.entries {
		total += ent.size
		if ent.meta.Bytes <= 150 {
			t.Fatal("index excluded from byte count")
		}
	}
	if total > 700 || len(w.entries) > 1 {
		t.Fatalf("quota total=%d segments=%d", total, len(w.entries))
	}
	if _, e = os.Lstat(unknown); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(filepath.Join(p, "segment-link")); e != nil {
		t.Fatal(e)
	}
}
func TestPreservedPartialConsumesBudget(t *testing.T) {
	p := testRoot(t)
	opts := Options{10, 700, 2, false}
	w, e := Open(p, opts)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.WriteVideo(make([]byte, 200)); e != nil {
		t.Fatal(e)
	}
	_ = w.WriteAudio([]byte{1})
	_ = w.Close()
	w, e = Open(p, opts)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if w.leftover != 200 {
		t.Fatalf("partial bytes %d", w.leftover)
	}
	if e = w.WriteVideo(make([]byte, 400)); e == nil {
		t.Fatal("partial bytes excluded from budget")
	}
}

func TestFlushContinuesInNewSegment(t *testing.T) {
	p := testRoot(t)
	w, e := Open(p, Options{100, 10000, 3, false})
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	if e = w.WriteVideo([]byte{1, 2}); e != nil {
		t.Fatal(e)
	}
	if e = w.Flush(); e != nil {
		t.Fatal(e)
	}
	if len(w.entries) != 1 || w.current.Frames != 0 {
		t.Fatal("flush did not publish")
	}
	if e = w.WriteVideo([]byte{3, 4}); e != nil {
		t.Fatal(e)
	}
	if e = w.Flush(); e != nil {
		t.Fatal(e)
	}
	if len(w.entries) != 2 {
		t.Fatal("next segment missing")
	}
}

func TestWriteFailureNeverPublishes(t *testing.T) {
	p := testRoot(t)
	w, e := Open(p, Options{100, 10000, 3, false})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.WriteVideo([]byte{1}); e != nil {
		t.Fatal(e)
	}
	part := w.part
	if e = w.video.Close(); e != nil {
		t.Fatal(e)
	}
	if e = w.WriteVideo([]byte{2}); e == nil {
		t.Fatal("closed file accepted write")
	}
	if e = w.Close(); e == nil {
		t.Fatal("I/O failure lost")
	}
	if _, e = os.Stat(part); e != nil {
		t.Fatal(e)
	}
	list, e := os.ReadDir(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range list {
		if len(entry.Name()) >= 8 && entry.Name()[:8] == "segment-" {
			t.Fatal("failed segment published")
		}
	}
}

func TestLeftoverForcesVideoRollover(t *testing.T) {
	p := testRoot(t)
	opts := Options{10, 700, 3, false}
	w, err := Open(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(p, ".segment-old.part")
	if err = os.Mkdir(part, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(part, "data"), make([]byte, 200), 0600); err != nil {
		t.Fatal(err)
	}
	w, err = Open(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.WriteVideo(make([]byte, 200)); err != nil {
		t.Fatal(err)
	}
	if err = w.WriteVideo(make([]byte, 200)); err != nil {
		t.Fatal(err)
	}
	if w.current.Frames != 1 {
		t.Fatalf("rollover failed: %d", w.current.Frames)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAudioQuotaPublishesAndCountsDrops(t *testing.T) {
	p := testRoot(t)
	w, err := Open(p, Options{10, 700, 3, true})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.WriteVideo(make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err = w.WriteAudio(make([]byte, 500)); err != nil {
		t.Fatal(err)
	}
	if err = w.WriteAudio(make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if w.DroppedAudioSamples() != 252 || len(w.entries) != 1 {
		t.Fatalf("dropped=%d entries=%d", w.DroppedAudioSamples(), len(w.entries))
	}
	r, err := NewReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.Segment(w.entries[0].meta.ID); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenEnforcesLimitsAndPreservesCorrupt(t *testing.T) {
	p := testRoot(t)
	w, err := Open(p, Options{1, 10000, 5, false})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = w.WriteVideo(make([]byte, 100)); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(p, "segment-1-abcdef12")
	if err = os.Mkdir(corrupt, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(corrupt, "data"), make([]byte, 100), 0600); err != nil {
		t.Fatal(err)
	}
	w, err = Open(p, Options{1, 700, 1, false})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if len(w.entries) != 1 || w.leftover != 100 || w.entries[0].size+w.leftover > 700 {
		t.Fatalf("entries=%d leftover=%d", len(w.entries), w.leftover)
	}
	if _, err = os.Stat(filepath.Join(corrupt, "data")); err != nil {
		t.Fatal(err)
	}
}
