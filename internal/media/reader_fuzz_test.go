package media

import (
	"bytes"
	"testing"
)

func FuzzReader(f *testing.F) {
	f.Add([]byte{})
	f.Add(fixture(3, 1, []byte{1, 2, 3}))
	f.Add(fixture(11, 0, nil)[:12])
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		r := NewReader(bytes.NewReader(data), 1<<16)
		for attempts := 0; attempts <= len(data)/32+1; attempts++ {
			frame, err := r.Next()
			if r.Statistics().Bytes > uint64(len(data)) {
				t.Fatal("counted unread input")
			}
			if err != nil {
				return
			}
			if len(frame.Payload) > 1<<16 {
				t.Fatal("payload limit exceeded")
			}
		}
		t.Fatal("reader did not terminate for finite input")
	})
}
