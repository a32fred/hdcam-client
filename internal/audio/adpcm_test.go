package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestDecodeADPCMHandVectors(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []int16
	}{
		{"zero and index floor", []byte{0, 0}, []int16{0, 0, 0, 0}},
		{"high nibble first and multiplication rounding", []byte{0x30}, []int16{5, 5}},
		{"negative", []byte{0xb0}, []int16{-5, -5}},
		{"index increases", []byte{0x77}, []int16{12, 42}},
		{"opposite signs", []byte{0x7f}, []int16{12, -18}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeADPCM(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want)*2 {
				t.Fatalf("length %d", len(got))
			}
			for i, want := range tt.want {
				if v := int16(binary.LittleEndian.Uint16(got[i*2:])); v != want {
					t.Fatalf("sample %d = %d, want %d", i, v, want)
				}
			}
		})
	}
}

func TestDecodeADPCMClampAndIndexCeiling(t *testing.T) {
	for _, tt := range []struct {
		packed byte
		want   int16
	}{{0x77, 32767}, {0xff, -32768}} {
		pcm, err := DecodeADPCM(bytes.Repeat([]byte{tt.packed}, 2048))
		if err != nil {
			t.Fatal(err)
		}
		if len(pcm) != 8192 {
			t.Fatalf("length %d", len(pcm))
		}
		if got := int16(binary.LittleEndian.Uint16(pcm[len(pcm)-2:])); got != tt.want {
			t.Fatalf("tail %d, want %d", got, tt.want)
		}
	}
}

func TestDecodeADPCMResetsEachPacket(t *testing.T) {
	first, err := DecodeADPCM([]byte{0x77})
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeADPCM(bytes.Repeat([]byte{0xff}, 32))
	if err != nil {
		t.Fatal(err)
	}
	second, err := DecodeADPCM([]byte{0x77})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("packet state leaked")
	}
}

func TestDecodeADPCMRejectsInvalidLength(t *testing.T) {
	for _, p := range [][]byte{nil, {}, make([]byte, 2049)} {
		got, err := DecodeADPCM(p)
		if err == nil || got != nil {
			t.Fatalf("length %d: got %v, error %v", len(p), got, err)
		}
	}
}
