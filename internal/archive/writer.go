package archive

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"example.com/hdcam-client/internal/audio"
)

type Options struct {
	MaxFrames int
	MaxBytes  int64
	Keep      int
	Audio     bool
}
type Segment struct {
	ID           string    `json:"id"`
	Started      time.Time `json:"started"`
	Ended        time.Time `json:"ended"`
	Frames       uint64    `json:"frames"`
	AudioSamples uint64    `json:"audio_samples"`
	Bytes        int64     `json:"bytes"`
	VideoMS      []int64   `json:"video_ms"`
	VideoSizes   []int     `json:"video_sizes"`
	AudioStartMS int64     `json:"audio_start_ms,omitempty"`
	Audio        bool      `json:"audio"`
}
type writerEntry struct {
	path string
	meta Segment
	size int64
}
type Writer struct {
	directory       string
	opts            Options
	lock, video, af *os.File
	wav             *audio.WAVWriter
	part            string
	current         Segment
	start           time.Time
	payload         int64
	entries         []writerEntry
	leftover        int64
	failed          error
	droppedAudio    uint64
	closed          bool
}

func Open(directory string, opts Options) (_ *Writer, err error) {
	if !archiveSupported {
		return nil, ErrUnsupported
	}
	if opts.MaxFrames <= 0 || opts.MaxFrames > 100000 || opts.MaxBytes <= 0 || opts.Keep <= 0 {
		return nil, errors.New("archive limits must be positive")
	}
	if err = os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	st, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("archive root must be a private directory (0700)")
	}
	w := &Writer{directory: directory, opts: opts}
	defer func() {
		if err != nil {
			_ = w.release()
		}
	}()
	lockpath := filepath.Join(directory, ".lock")
	if s, e := os.Lstat(lockpath); e == nil && !s.Mode().IsRegular() {
		return nil, errors.New("unsafe archive lock")
	}
	w.lock, err = archiveOpenLock(lockpath)
	if err != nil {
		return nil, err
	}
	marker := filepath.Join(directory, ".hdcam-archive")
	st, err = os.Lstat(marker)
	if os.IsNotExist(err) {
		var entries []os.DirEntry
		entries, err = os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Name() != ".lock" {
				return nil, errors.New("refusing nonempty unmarked archive")
			}
		}
		var f *os.File
		f, err = os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = f.WriteString("hdcam-archive-v1\n")
		err = errors.Join(err, f.Sync(), f.Close())
		if err != nil {
			return nil, err
		}
	} else {
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, errors.New("unsafe archive marker")
		}
		var b []byte
		b, err = os.ReadFile(marker)
		if err != nil {
			return nil, err
		}
		if string(b) != "hdcam-archive-v1\n" {
			return nil, errors.New("invalid archive marker")
		}
	}
	list, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		if (strings.HasPrefix(e.Name(), ".segment-") && strings.HasSuffix(e.Name(), ".part")) || (e.IsDir() && ValidID(e.Name()) && !writerValid(directory, e.Name())) {
			err = filepath.WalkDir(filepath.Join(directory, e.Name()), func(p string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				s, e := os.Lstat(p)
				if e != nil {
					return e
				}
				if s.Mode().IsRegular() {
					if s.Size() > math.MaxInt64-w.leftover {
						return errors.New("archive size overflow")
					}
					w.leftover += s.Size()
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		if ValidID(e.Name()) {
			if ent, ok := writerInspect(directory, e.Name()); ok {
				w.entries = append(w.entries, ent)
			}
		}
	}
	sort.Slice(w.entries, func(i, j int) bool { return w.entries[i].meta.Started.Before(w.entries[j].meta.Started) })
	if err = w.fit(0); err != nil {
		return nil, err
	}
	if err = w.keep(); err != nil {
		return nil, err
	}
	return w, nil
}

func writerValid(root, name string) bool { _, ok := writerInspect(root, name); return ok }

func writerInspect(root, name string) (writerEntry, bool) {
	if !ValidID(name) {
		return writerEntry{}, false
	}
	p := filepath.Join(root, name)
	st, err := os.Lstat(p)
	if err != nil || !st.IsDir() {
		return writerEntry{}, false
	}
	dir, err := os.OpenRoot(p)
	if err != nil {
		return writerEntry{}, false
	}
	m, err := readerMetadata(dir, name)
	err = errors.Join(err, dir.Close())
	if err != nil {
		return writerEntry{}, false
	}
	return writerEntry{p, m, m.Bytes}, true
}

func writerSyncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	return errors.Join(f.Sync(), f.Close())
}
func writerManifest(m Segment, payload int64) ([]byte, error) {
	m.Bytes = payload
	for i := 0; i < 8; i++ {
		b, e := json.Marshal(m)
		if e != nil {
			return nil, e
		}
		if payload < 0 || payload > math.MaxInt64-int64(len(b)) {
			return nil, errors.New("archive size overflow")
		}
		n := payload + int64(len(b))
		if n == m.Bytes {
			return b, nil
		}
		m.Bytes = n
	}
	return nil, errors.New("manifest size did not converge")
}
func (w *Writer) latch(e error) error {
	if e != nil && w.failed == nil {
		w.failed = e
	}
	return e
}
func (w *Writer) ready() error {
	if w.closed {
		return errors.New("archive closed")
	}
	return w.failed
}
func (w *Writer) projected(m Segment, payload int64) int64 {
	m.Ended = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	m.Started = m.Ended
	b, e := writerManifest(m, payload)
	if e != nil {
		return math.MaxInt64
	}
	return payload + int64(len(b))
}
func (w *Writer) fit(need int64) error {
	if need < 0 || w.leftover > w.opts.MaxBytes || need > w.opts.MaxBytes-w.leftover {
		return errors.New("archive byte budget exceeded by current segment and preserved partials")
	}
	total := need + w.leftover
	for _, e := range w.entries {
		if e.size > math.MaxInt64-total {
			return errors.New("archive size overflow")
		}
		total += e.size
	}
	for total > w.opts.MaxBytes && len(w.entries) > 0 {
		e := w.entries[0]
		if _, ok := writerInspect(w.directory, e.meta.ID); !ok {
			return errors.New("retention segment changed")
		}
		if err := os.RemoveAll(e.path); err != nil {
			return err
		}
		if err := writerSyncDir(w.directory); err != nil {
			return err
		}
		total -= e.size
		w.entries = w.entries[1:]
	}
	if total > w.opts.MaxBytes {
		return errors.New("archive byte budget exceeded")
	}
	return nil
}
func (w *Writer) begin() error {
	var r [4]byte
	if _, e := rand.Read(r[:]); e != nil {
		return e
	}
	w.start = time.Now()
	id := fmt.Sprintf("segment-%d-%s", w.start.UTC().UnixNano(), hex.EncodeToString(r[:]))
	w.part = filepath.Join(w.directory, "."+id+".part")
	if e := os.Mkdir(w.part, 0700); e != nil {
		return e
	}
	w.current = Segment{ID: id, Started: w.start.UTC(), Audio: w.opts.Audio}
	w.payload = 0
	var e error
	w.video, e = os.OpenFile(filepath.Join(w.part, "video.mjpg"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if w.opts.Audio {
		w.af, e = os.OpenFile(filepath.Join(w.part, "audio.wav"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		w.wav, e = audio.NewWAV(w.af)
		if e != nil {
			return e
		}
		w.payload = 44
	}
	return nil
}
func (w *Writer) WriteVideo(jpeg []byte) error {
	if e := w.ready(); e != nil {
		return e
	}
	if len(jpeg) == 0 || len(jpeg) > 4<<20 {
		return w.latch(errors.New("invalid JPEG size"))
	}
	minimum := Segment{ID: "segment-0000000000000000000-00000000", Started: time.Now().UTC(), Frames: 1, VideoMS: []int64{0}, VideoSizes: []int{len(jpeg)}, Audio: w.opts.Audio}
	minimumBytes := int64(len(jpeg))
	if w.opts.Audio {
		minimumBytes += 44
	}
	if w.leftover > w.opts.MaxBytes || w.projected(minimum, minimumBytes) > w.opts.MaxBytes-w.leftover {
		return w.latch(errors.New("incoming frame exceeds archive byte budget"))
	}
	if w.current.Frames >= uint64(w.opts.MaxFrames) {
		if e := w.commit(); e != nil {
			return w.latch(e)
		}
	}
	m := w.current
	if w.video == nil {
		m = Segment{ID: "segment-0000000000000000000-00000000", Started: time.Now().UTC(), Audio: w.opts.Audio}
	}
	m.Frames++
	m.VideoMS = append(append([]int64{}, m.VideoMS...), int64(9223372036854775807))
	m.VideoSizes = append(append([]int{}, m.VideoSizes...), len(jpeg))
	if w.payload > math.MaxInt64-int64(len(jpeg))-44 {
		return w.latch(errors.New("archive size overflow"))
	}
	payload := w.payload + int64(len(jpeg))
	if w.video == nil && w.opts.Audio {
		payload += 44
	}
	if (w.leftover > w.opts.MaxBytes || w.projected(m, payload) > w.opts.MaxBytes-w.leftover) && w.current.Frames > 0 {
		if e := w.commit(); e != nil {
			return w.latch(e)
		}
		return w.WriteVideo(jpeg)
	}
	if e := w.fit(w.projected(m, payload)); e != nil {
		return w.latch(e)
	}
	if w.video == nil {
		if e := w.begin(); e != nil {
			return w.latch(e)
		}
	}
	n, e := w.video.Write(jpeg)
	if e == nil && n != len(jpeg) {
		e = io.ErrShortWrite
	}
	if e != nil {
		return w.latch(e)
	}
	w.payload += int64(n)
	w.current.Frames++
	w.current.VideoMS = append(w.current.VideoMS, time.Since(w.start).Milliseconds())
	w.current.VideoSizes = append(w.current.VideoSizes, len(jpeg))
	return nil
}
func (w *Writer) WriteAudio(pcm []byte) error {
	if e := w.ready(); e != nil {
		return e
	}
	if len(pcm)%2 != 0 || len(pcm) > 8192 {
		return w.latch(errors.New("invalid PCM block"))
	}
	if w.wav == nil {
		if w.opts.Audio {
			w.droppedAudio += uint64(len(pcm) / 2)
		}
		return nil
	}
	if w.payload > math.MaxInt64-int64(len(pcm)) {
		return w.latch(errors.New("archive size overflow"))
	}
	m := w.current
	m.AudioSamples += uint64(len(pcm) / 2)
	if w.current.AudioSamples == 0 && len(pcm) > 0 {
		m.AudioStartMS = time.Since(w.start).Milliseconds()
	}
	if w.leftover > w.opts.MaxBytes || w.projected(m, w.payload+int64(len(pcm))) > w.opts.MaxBytes-w.leftover {
		if e := w.commit(); e != nil {
			return w.latch(e)
		}
		w.droppedAudio += uint64(len(pcm) / 2)
		return nil
	}
	if e := w.fit(w.projected(m, w.payload+int64(len(pcm)))); e != nil {
		return w.latch(fmt.Errorf("audio budget: %w", e))
	}
	n, e := w.wav.Write(pcm)
	if e != nil {
		return w.latch(e)
	}
	w.payload += int64(n)
	w.current.AudioSamples = m.AudioSamples
	w.current.AudioStartMS = m.AudioStartMS
	return nil
}
func (w *Writer) closeStreams() error {
	var e error
	if w.wav != nil {
		e = errors.Join(e, w.wav.Close())
		w.wav = nil
	}
	if w.af != nil {
		e = errors.Join(e, w.af.Sync(), w.af.Close())
		w.af = nil
	}
	if w.video != nil {
		e = errors.Join(e, w.video.Sync(), w.video.Close())
		w.video = nil
	}
	return e
}
func (w *Writer) commit() error {
	if w.current.Frames == 0 {
		return nil
	}
	if e := w.closeStreams(); e != nil {
		return e
	}
	w.current.Ended = time.Now().UTC()
	b, e := writerManifest(w.current, w.payload)
	if e != nil {
		return e
	}
	if e = w.fit(w.payload + int64(len(b))); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(w.part, "index.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	n, e := f.Write(b)
	if e == nil && n != len(b) {
		e = io.ErrShortWrite
	}
	e = errors.Join(e, f.Sync(), f.Close())
	if e != nil {
		return e
	}
	if e = writerSyncDir(w.part); e != nil {
		return e
	}
	dst := filepath.Join(w.directory, w.current.ID)
	if e = os.Rename(w.part, dst); e != nil {
		return e
	}
	if e := writerSyncDir(w.directory); e != nil {
		return e
	}
	var m Segment
	_ = json.Unmarshal(b, &m)
	w.entries = append(w.entries, writerEntry{dst, m, m.Bytes})
	w.current = Segment{}
	w.payload = 0
	w.part = ""
	return w.keep()
}

func (w *Writer) DroppedAudioSamples() uint64 { return w.droppedAudio }

func (w *Writer) keep() error {
	for len(w.entries) > w.opts.Keep {
		ent := w.entries[0]
		if _, ok := writerInspect(w.directory, ent.meta.ID); !ok {
			return errors.New("retention segment changed")
		}
		if e := os.RemoveAll(ent.path); e != nil {
			return e
		}
		if e := writerSyncDir(w.directory); e != nil {
			return e
		}
		w.entries = w.entries[1:]
	}
	return nil
}
func (w *Writer) release() error {
	if w.lock == nil {
		return nil
	}
	e := archiveUnlock(w.lock)
	e = errors.Join(e, w.lock.Close())
	w.lock = nil
	return e
}

// Flush publishes the current segment and leaves the archive open for a new session.
func (w *Writer) Flush() error {
	if e := w.ready(); e != nil {
		return e
	}
	return w.latch(w.commit())
}
func (w *Writer) Close() error {
	if w.closed {
		return w.failed
	}
	if w.failed == nil {
		w.latch(w.commit())
	}
	w.latch(w.closeStreams())
	w.latch(w.release())
	w.closed = true
	return w.failed
}
