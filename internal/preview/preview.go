package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Run renders an explicitly assumed constant frame rate, not camera timing.
func Run(ctx context.Context, ffmpeg, input, output string, fps int) error {
	if fps < 1 || fps > 60 {
		return errors.New("fps must be between 1 and 60")
	}
	source, err := os.Stat(input)
	if err != nil {
		return err
	}
	if !source.Mode().IsRegular() {
		return errors.New("input must be a regular file")
	}
	if _, err := os.Lstat(output); err == nil {
		return errors.New("output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), ".preview-*.mp4")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Close(); err != nil {
		return err
	}
	absolute, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-f", "mjpeg", "-framerate", strconv.Itoa(fps), "-i", absolute, "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-f", "mp4", path)
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("ffmpeg produced an empty file")
	}
	// Hard-link publication is atomic and refuses to replace any existing path.
	if err := os.Link(path, output); err != nil {
		return fmt.Errorf("publish MP4: %w", err)
	}
	return nil
}
