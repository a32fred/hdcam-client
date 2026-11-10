package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	mjpeg "github.com/a32fred/hdcam-client/internal/export"
	"github.com/a32fred/hdcam-client/internal/live"
	"github.com/a32fred/hdcam-client/internal/media"
)

func TestRetryCodeRedactsDetails(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{{nil, "session_completed"}, {context.Canceled, "stalled"}, {context.DeadlineExceeded, "timeout"}, {mjpeg.ErrNoFrames, "no_video"}, {fmt.Errorf("wrapped:%w", media.ErrFormat), "invalid_media"}, {media.ErrTruncated, "invalid_media"}, {&os.PathError{Op: "exec", Path: "private-runtime", Err: os.ErrNotExist}, "runtime_missing"}, {exec.ErrNotFound, "runtime_missing"}, {errors.New("secret-field-do-not-echo"), "capture_error"}}
	for _, c := range cases {
		if got := RetryCode(c.err); got != c.code {
			t.Fatalf("code=%q want%q", got, c.code)
		}
	}
}

func TestRetryCodeWatchdogCancellation(t *testing.T) {
	parent := context.Background()
	err := live.Watch(parent, time.Millisecond, time.Millisecond, func(child context.Context, _ func()) error {
		<-child.Done()
		return fmt.Errorf("capture attempt: %w", child.Err())
	})
	if parent.Err() != nil || RetryCode(err) != "stalled" {
		t.Fatalf("unexpected watchdog classification:%s", RetryCode(err))
	}
}
func TestRetryCodeUsesExitStage(t *testing.T) {
	if os.Getenv("HDCAM_FAILURE_HELPER") != "" {
		if os.Getenv("HDCAM_FAILURE_HELPER") == "startup" {
			os.Exit(3)
		}
		os.Exit(4)
	}
	for _, s := range []struct{ stage, code string }{{"startup", "startup_failed"}, {"stream", "stream_failed"}} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRetryCodeUsesExitStage$")
		cmd.Env = append(os.Environ(), "HDCAM_FAILURE_HELPER="+s.stage)
		err := cmd.Run()
		for _, wrapped := range []error{err, fmt.Errorf("capture: %w", errors.Join(mjpeg.ErrNoFrames, err))} {
			if got := RetryCode(wrapped); got != s.code {
				t.Fatalf("%s=%s", s.stage, got)
			}
		}
	}
}
