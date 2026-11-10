package capture

import (
	"context"
	"errors"
	"os"
	"os/exec"

	mjpeg "github.com/a32fred/hdcam-client/internal/export"
	"github.com/a32fred/hdcam-client/internal/media"
)

// RetryCode categorizes an attempt while its supervisor remains active.
// A canceled child here is the frame watchdog; intentional parent shutdown is filtered by the caller.
func RetryCode(err error) string {
	if err == nil {
		return "session_completed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "stalled"
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) || errors.Is(err, exec.ErrDot) {
		return "runtime_missing"
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		switch exited.ExitCode() {
		case 3:
			return "startup_failed"
		case 4:
			return "stream_failed"
		}
	}
	if errors.Is(err, media.ErrFormat) || errors.Is(err, media.ErrTruncated) {
		return "invalid_media"
	}
	if exited != nil {
		return "capture_error"
	}
	if errors.Is(err, mjpeg.ErrNoFrames) {
		return "no_video"
	}
	return "capture_error"
}
