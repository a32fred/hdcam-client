package record

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRotationAndRetention(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "recordings")
	r, err := New(dir, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	frames := [][]byte{[]byte("one"), []byte("two"), []byte("three"), []byte("four"), []byte("five")}
	for _, frame := range frames {
		if err := r.WriteFrame(frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{
		"segment-000002.mjpg": []byte("threefour"),
		"segment-000003.mjpg": []byte("five"),
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "segment-000001.mjpg")); !os.IsNotExist(err) {
		t.Fatalf("old segment still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "segment-000003.mjpg.part")); !os.IsNotExist(err) {
		t.Fatalf("part file remains after successful close: %v", err)
	}
}

func TestNewRejectsExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(dir, 1, 1); !errors.Is(err, os.ErrExist) {
		t.Fatalf("New existing directory error = %v", err)
	}
}

func TestNewRejectsInvalidLimits(t *testing.T) {
	for _, limits := range [][2]int{{0, 1}, {-1, 1}, {1, 0}, {1, -1}} {
		if _, err := New(filepath.Join(t.TempDir(), "recordings"), limits[0], limits[1]); err == nil {
			t.Fatalf("New(%d, %d) succeeded", limits[0], limits[1])
		}
	}
}

func TestCloseWithoutFramesCreatesNoFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "recordings")
	r, err := New(dir, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty recorder created files: %v", entries)
	}
}

func TestClosePreservesPartAfterWriteError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "recordings")
	r, err := New(dir, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFrame([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := r.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFrame([]byte("second")); err == nil {
		t.Fatal("WriteFrame succeeded after its segment file was closed")
	}
	if err := r.Close(); err == nil {
		t.Fatal("Close did not retain the earlier write error")
	}
	if _, err := os.Stat(filepath.Join(dir, "segment-000001.mjpg.part")); err != nil {
		t.Fatalf("failed segment part was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "segment-000001.mjpg")); !os.IsNotExist(err) {
		t.Fatalf("failed segment was published: %v", err)
	}
}
