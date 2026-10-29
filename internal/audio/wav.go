// Package audio decodes the observed camera ADPCM and writes PCM WAV streams.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var PCM16Mono8kHz = Format{SampleRate: 8000, Channels: 1, BitsPerSample: 16}

const wavHeaderSize = 44

// Format describes integer PCM stored in little-endian byte order.
type Format struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
}

// WriteWAV writes PCM data with a 44-byte RIFF/WAVE header. The payload must
// contain complete sample frames. Format currently supports signed 16-bit PCM.
func WriteWAV(w io.Writer, pcm []byte, format Format) error {
	if format.SampleRate == 0 || format.Channels == 0 || format.BitsPerSample != 16 {
		return fmt.Errorf("unsupported PCM format: %d Hz, %d channels, %d bits", format.SampleRate, format.Channels, format.BitsPerSample)
	}
	blockAlign := uint32(format.Channels) * uint32(format.BitsPerSample/8)
	if blockAlign > uint32(^uint16(0)) {
		return errors.New("PCM block alignment exceeds WAV format limit")
	}
	if uint64(len(pcm))%uint64(blockAlign) != 0 {
		return errors.New("PCM data ends with an incomplete sample frame")
	}
	if uint64(len(pcm)) > uint64(^uint32(0))-(wavHeaderSize-8) {
		return errors.New("PCM data exceeds RIFF/WAVE size limit")
	}
	byteRate := uint64(format.SampleRate) * uint64(blockAlign)
	if byteRate > uint64(^uint32(0)) {
		return errors.New("PCM byte rate exceeds WAV format limit")
	}

	var header [wavHeaderSize]byte
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(pcm))+wavHeaderSize-8)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // WAVE_FORMAT_PCM
	binary.LittleEndian.PutUint16(header[22:24], format.Channels)
	binary.LittleEndian.PutUint32(header[24:28], format.SampleRate)
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], format.BitsPerSample)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(pcm)))
	if err := writeFull(w, header[:]); err != nil {
		return fmt.Errorf("write WAV header: %w", err)
	}
	if err := writeFull(w, pcm); err != nil {
		return fmt.Errorf("write WAV PCM data: %w", err)
	}
	return nil
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return errors.New("invalid write count")
		}
		p = p[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
