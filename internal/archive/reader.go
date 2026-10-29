package archive

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

var ErrCorrupt = errors.New("invalid archive segment")
var ErrUnsupported = errors.New("archive storage is supported only on Linux")

type Reader struct{ root *os.Root }

func ValidID(id string) bool {
	if len(id) > 64 || !strings.HasPrefix(id, "segment-") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(id, "segment-"), "-")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 20 || len(parts[1]) != 8 {
		return false
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return false
		}
	}
	if _, err := strconv.ParseUint(parts[0], 10, 64); err != nil {
		return false
	}
	for _, r := range parts[1] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func NewReader(directory string) (*Reader, error) {
	if !archiveSupported {
		return nil, ErrUnsupported
	}
	st, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return nil, ErrCorrupt
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	r := &Reader{root: root}
	file, err := readerFile(root, ".hdcam-archive")
	if err != nil {
		root.Close()
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 65))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(data) != "hdcam-archive-v1\n" {
		root.Close()
		return nil, ErrCorrupt
	}
	return r, nil
}
func (r *Reader) Close() error { return r.root.Close() }
func readerFile(root *os.Root, name string) (*os.File, error) {
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, ErrCorrupt
	}
	file, err := archiveOpenRegular(root, name)
	if err != nil {
		return nil, err
	}
	st, err = file.Stat()
	if err != nil || !st.Mode().IsRegular() {
		file.Close()
		return nil, ErrCorrupt
	}
	return file, nil
}
func (r *Reader) segmentRoot(id string) (*os.Root, error) {
	if !ValidID(id) {
		return nil, os.ErrNotExist
	}
	st, err := r.root.Lstat(id)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, ErrCorrupt
	}
	return r.root.OpenRoot(id)
}
func readerMetadata(root *os.Root, id string) (Segment, error) {
	var m Segment
	index, err := readerFile(root, "index.json")
	if err != nil {
		return m, err
	}
	data, err := io.ReadAll(io.LimitReader(index, 8<<20+1))
	closeErr := index.Close()
	if err != nil || closeErr != nil || len(data) > 8<<20 {
		return m, ErrCorrupt
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil {
		return m, ErrCorrupt
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return m, ErrCorrupt
	}
	if m.ID != id || m.Frames == 0 || m.Frames > 100000 || len(m.VideoMS) != int(m.Frames) || len(m.VideoSizes) != int(m.Frames) || m.Started.IsZero() || m.Ended.Before(m.Started) || m.Bytes <= 0 || m.AudioStartMS < 0 {
		return m, ErrCorrupt
	}
	var videoBytes int64
	duration := m.Ended.Sub(m.Started).Milliseconds()
	for i, size := range m.VideoSizes {
		if size < 1 || size > 4<<20 || m.VideoMS[i] < 0 || m.VideoMS[i] > duration+1000 || (i > 0 && m.VideoMS[i] < m.VideoMS[i-1]) {
			return m, ErrCorrupt
		}
		videoBytes += int64(size)
	}
	if m.AudioStartMS > duration+1000 || m.AudioSamples > uint64((1<<32-37)/2) || (!m.Audio && m.AudioSamples != 0) {
		return m, ErrCorrupt
	}
	video, err := readerFile(root, "video.mjpg")
	if err != nil {
		return m, err
	}
	st, err := video.Stat()
	closeErr = video.Close()
	if err != nil || closeErr != nil || st.Size() != videoBytes {
		return m, ErrCorrupt
	}
	total := videoBytes + int64(len(data))
	if m.Audio {
		wav, err := readerFile(root, "audio.wav")
		if err != nil {
			return m, err
		}
		st, err := wav.Stat()
		var h [44]byte
		_, headerErr := io.ReadFull(wav, h[:])
		closeErr = wav.Close()
		expected := 44 + int64(m.AudioSamples)*2
		if err != nil || headerErr != nil || closeErr != nil || st.Size() != expected || string(h[:4]) != "RIFF" || string(h[8:16]) != "WAVEfmt " || string(h[36:40]) != "data" || binary.LittleEndian.Uint32(h[4:]) != uint32(expected-8) || binary.LittleEndian.Uint32(h[16:]) != 16 || binary.LittleEndian.Uint16(h[20:]) != 1 || binary.LittleEndian.Uint16(h[22:]) != 1 || binary.LittleEndian.Uint32(h[24:]) != 8000 || binary.LittleEndian.Uint32(h[28:]) != 16000 || binary.LittleEndian.Uint16(h[32:]) != 2 || binary.LittleEndian.Uint16(h[34:]) != 16 || binary.LittleEndian.Uint32(h[40:]) != uint32(expected-44) {
			return m, ErrCorrupt
		}
		total += expected
	}
	dir, err := root.Open(".")
	if err != nil {
		return m, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return m, err
	}
	expectedFiles := 2
	if m.Audio {
		expectedFiles = 3
	}
	if len(entries) != expectedFiles || m.Bytes != total {
		return m, ErrCorrupt
	}
	return m, nil
}
func (r *Reader) Segment(id string) (Segment, error) {
	root, err := r.segmentRoot(id)
	if err != nil {
		return Segment{}, err
	}
	defer root.Close()
	return readerMetadata(root, id)
}
func (r *Reader) List() ([]Segment, error) {
	dir, err := r.root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return nil, err
	}
	list := make([]Segment, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !ValidID(entry.Name()) {
			continue
		}
		m, err := r.Segment(entry.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Started.Equal(list[j].Started) {
			return list[i].ID > list[j].ID
		}
		return list[i].Started.After(list[j].Started)
	})
	return list, nil
}
func (r *Reader) Frame(id string, index int) ([]byte, error) {
	root, err := r.segmentRoot(id)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	m, err := readerMetadata(root, id)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(m.VideoSizes) {
		return nil, os.ErrNotExist
	}
	var offset int64
	for _, size := range m.VideoSizes[:index] {
		offset += int64(size)
	}
	file, err := readerFile(root, "video.mjpg")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	frame := make([]byte, m.VideoSizes[index])
	_, err = io.ReadFull(io.NewSectionReader(file, offset, int64(len(frame))), frame)
	return frame, err
}
func (r *Reader) OpenAudio(id string) (*os.File, Segment, error) {
	root, err := r.segmentRoot(id)
	if err != nil {
		return nil, Segment{}, err
	}
	defer root.Close()
	m, err := readerMetadata(root, id)
	if err != nil {
		return nil, m, err
	}
	if !m.Audio {
		return nil, m, os.ErrNotExist
	}
	file, err := readerFile(root, "audio.wav")
	return file, m, err
}
