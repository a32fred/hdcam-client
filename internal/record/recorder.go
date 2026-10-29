package record

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Recorder struct {
	directory string
	maxFrames int
	keep      int
	segment   int
	frames    int
	file      *os.File
	partPath  string
	completed []string
	closed    bool
	err       error
}

func New(directory string, maxFrames int, keep int) (*Recorder, error) {
	if maxFrames <= 0 || keep <= 0 {
		return nil, errors.New("maxFrames and keep must be greater than zero")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, fmt.Errorf("create recording directory: %w", err)
	}
	return &Recorder{directory: directory, maxFrames: maxFrames, keep: keep}, nil
}

func (r *Recorder) WriteFrame(frame []byte) error {
	if r.closed {
		return os.ErrClosed
	}
	if r.err != nil {
		return r.err
	}
	if r.file == nil {
		if err := r.openSegment(); err != nil {
			r.err = err
			return err
		}
	}
	for data := frame; len(data) > 0; {
		n, err := r.file.Write(data)
		data = data[n:]
		if err != nil {
			r.err = fmt.Errorf("write frame: %w", err)
			return r.err
		}
		if n == 0 {
			r.err = io.ErrShortWrite
			return r.err
		}
	}
	r.frames++
	if r.frames == r.maxFrames {
		if err := r.commitSegment(); err != nil {
			r.err = err
			return err
		}
	}
	return nil
}

func (r *Recorder) openSegment() error {
	r.segment++
	name := fmt.Sprintf("segment-%06d.mjpg", r.segment)
	r.partPath = filepath.Join(r.directory, name+".part")
	f, err := os.OpenFile(r.partPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create segment: %w", err)
	}
	r.file = f
	r.frames = 0
	return nil
}

func (r *Recorder) commitSegment() error {
	if r.file == nil || r.frames == 0 {
		return nil
	}
	if err := r.file.Close(); err != nil {
		r.file = nil
		return fmt.Errorf("close segment: %w", err)
	}
	r.file = nil
	finalPath := r.partPath[:len(r.partPath)-len(".part")]
	if err := os.Rename(r.partPath, finalPath); err != nil {
		return fmt.Errorf("publish segment: %w", err)
	}
	r.completed = append(r.completed, finalPath)
	r.partPath = ""
	r.frames = 0
	for len(r.completed) > r.keep {
		oldest := r.completed[0]
		if err := os.Remove(oldest); err != nil {
			return fmt.Errorf("remove expired segment: %w", err)
		}
		r.completed = r.completed[1:]
	}
	return nil
}

func (r *Recorder) Close() error {
	if r.closed {
		return r.err
	}
	r.closed = true
	if r.err != nil {
		if r.file != nil {
			_ = r.file.Close()
			r.file = nil
		}
		return r.err
	}
	r.err = r.commitSegment()
	return r.err
}
