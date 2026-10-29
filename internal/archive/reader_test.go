package archive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func readerFixture(t *testing.T, opts Options) (string, []Segment) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "archive")
	w, err := Open(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	frames := [][]byte{{0xff, 0xd8, 1, 0xff, 0xd9}, {0xff, 0xd8, 2, 0xff, 0xd9}}
	for _, frame := range frames {
		if err := w.WriteVideo(frame); err != nil {
			t.Fatal(err)
		}
		if opts.Audio {
			if err := w.WriteAudio([]byte{1, 0, 2, 0}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	segments := make([]Segment, len(w.entries))
	for i, entry := range w.entries {
		segments[i] = entry.meta
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return root, segments
}

func rewriteMetadata(t *testing.T, root string, m Segment) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, m.ID, "index.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNewReaderRequiresMarkedPrivateDirectory(t *testing.T) {
	t.Run("unmarked", func(t *testing.T) {
		root := t.TempDir()
		if _, err := NewReader(root); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("NewReader error = %v, want ErrCorrupt", err)
		}
	})
	t.Run("directory permissions", func(t *testing.T) {
		root, _ := readerFixture(t, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 2})
		if err := os.Chmod(root, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := NewReader(root); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("NewReader error = %v, want ErrCorrupt", err)
		}
	})
	t.Run("public marker permissions", func(t *testing.T) {
		root, _ := readerFixture(t, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 2})
		if err := os.Chmod(filepath.Join(root, ".hdcam-archive"), 0644); err != nil {
			t.Fatal(err)
		}
		r, err := NewReader(root)
		if err != nil {
			t.Fatalf("NewReader rejected public format marker: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("symlink marker", func(t *testing.T) {
		root, _ := readerFixture(t, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 2})
		marker := filepath.Join(root, ".hdcam-archive")
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "marker")
		if err := os.WriteFile(outside, []byte("hdcam-archive-v1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, marker); err != nil {
			t.Fatal(err)
		}
		if _, err := NewReader(root); err == nil {
			t.Fatal("NewReader accepted symlink marker")
		}
	})
}

func TestValidIDRejectsTraversalAndMalformedIDs(t *testing.T) {
	for _, id := range []string{"../segment-1-01234567", "segment-1-0123456g", "segment--01234567", "segment-18446744073709551616-01234567", "segment-1-01234567/../../x", "segment-1-01234567-extra"} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
	if ValidID("segment-1-01234567") == false {
		t.Fatal("valid segment ID rejected")
	}
}

func TestReaderListAndFrameIndexes(t *testing.T) {
	root, segments := readerFixture(t, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 3, Audio: true})
	id := segments[0].ID
	if err := os.Mkdir(filepath.Join(root, ".segment-2-01234567.part"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "segment-3-01234567")); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	list, err := r.List()
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("List() = %v, %v", list, err)
	}
	want := [][]byte{{0xff, 0xd8, 1, 0xff, 0xd9}, {0xff, 0xd8, 2, 0xff, 0xd9}}
	for i := range want {
		got, err := r.Frame(id, i)
		if err != nil || string(got) != string(want[i]) {
			t.Errorf("Frame(%d) = %v, %v; want %v", i, got, err, want[i])
		}
	}
	for _, index := range []int{-1, 2} {
		if _, err := r.Frame(id, index); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Frame(%d) error = %v, want os.ErrNotExist", index, err)
		}
	}
}

func TestReaderRejectsCorruptMetadataAndFiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string, Segment)
	}{
		{"filesize mismatch", func(t *testing.T, root string, m Segment) {
			f, err := os.OpenFile(filepath.Join(root, m.ID, "video.mjpg"), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := f.Write([]byte{0})
			if err := errors.Join(writeErr, f.Close()); err != nil {
				t.Fatal(err)
			}
		}},
		{"nonmonotonic timing", func(t *testing.T, root string, m Segment) {
			m.VideoMS[1] = m.VideoMS[0] - 1
			rewriteMetadata(t, root, m)
		}},
		{"oversize frame", func(t *testing.T, root string, m Segment) {
			m.VideoSizes[0] = 4<<20 + 1
			rewriteMetadata(t, root, m)
		}},
		{"video symlink outside", func(t *testing.T, root string, m Segment) {
			p := filepath.Join(root, m.ID, "video.mjpg")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "video.mjpg")
			if err := os.WriteFile(outside, []byte{1}, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, p); err != nil {
				t.Fatal(err)
			}
		}},
		{"invalid audio header", func(t *testing.T, root string, m Segment) {
			p := filepath.Join(root, m.ID, "audio.wav")
			f, err := os.OpenFile(p, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := f.WriteAt([]byte("NOPE"), 0)
			if err := errors.Join(writeErr, f.Close()); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audio := tt.name == "invalid audio header"
			root, segments := readerFixture(t, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 2, Audio: audio})
			tt.mutate(t, root, segments[0])
			r, err := NewReader(root)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err := r.Segment(segments[0].ID); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Segment() error = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestReaderReportsDeletedRetainedSegment(t *testing.T) {
	root, segments := readerFixture(t, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 1})
	w, err := Open(root, Options{MaxFrames: 10, MaxBytes: 10000, Keep: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteVideo([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Segment(segments[0].ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Segment() error = %v, want os.ErrNotExist", err)
	}
}
