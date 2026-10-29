package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeFFmpegScript = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_LOG"
for last do :; done
case "$FAKE_MODE" in
  fail) exit 7 ;;
  empty) : > "$last" ;;
  race) printf 'generated' > "$last"; printf 'raced-output' > "$FAKE_RACE_OUTPUT" ;;
  active) : > "$FAKE_ACTIVE"; exec sleep 30 ;;
  *) printf 'generated-mp4' > "$last" ;;
esac
`

func fakeFFmpeg(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(path, []byte(fakeFFmpegScript), 0700); err != nil {
		t.Fatal(err)
	}
	return path, dir
}

func setFakeEnv(t *testing.T, dir, mode string) (logPath, activePath, raceOutput string) {
	t.Helper()
	logPath = filepath.Join(dir, "args.log")
	activePath = filepath.Join(dir, "active")
	raceOutput = filepath.Join(dir, "race-output.mp4")
	t.Setenv("FAKE_LOG", logPath)
	t.Setenv("FAKE_MODE", mode)
	t.Setenv("FAKE_ACTIVE", activePath)
	t.Setenv("FAKE_RACE_OUTPUT", raceOutput)
	return
}

func inputFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.mjpg")
	if err := os.WriteFile(path, []byte("mjpeg-frames"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCreatesPrivateOutputAndPassesExpectedFFmpegArguments(t *testing.T) {
	ffmpeg, dir := fakeFFmpeg(t)
	logPath, _, _ := setFakeEnv(t, dir, "success")
	input := inputFile(t)
	output := filepath.Join(t.TempDir(), "preview.mp4")
	if err := Run(context.Background(), ffmpeg, input, output, 25); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated-mp4" {
		t.Fatalf("output bytes = %q", data)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("output mode = %04o, want 0600", got)
	}
	argBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(argBytes), "\n"), "\n")
	joined := "\n" + strings.Join(args, "\n") + "\n"
	for _, arg := range []string{"-nostdin", "-y", "-f", "mjpeg", "-framerate", "25", "-i", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-f", "mp4"} {
		if !strings.Contains(joined, "\n"+arg+"\n") {
			t.Errorf("ffmpeg arguments %q do not include %q", args, arg)
		}
	}
	absInput, err := filepath.Abs(input)
	if err != nil {
		t.Fatal(err)
	}
	foundInput := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-i" && args[i+1] == absInput {
			foundInput = true
		}
	}
	if !foundInput {
		t.Errorf("ffmpeg args do not pass absolute input %q: %q", absInput, args)
	}
}

func TestRunCommandFailureAndEmptyOutputDoNotPublish(t *testing.T) {
	for _, mode := range []string{"fail", "empty"} {
		t.Run(mode, func(t *testing.T) {
			ffmpeg, dir := fakeFFmpeg(t)
			setFakeEnv(t, dir, mode)
			output := filepath.Join(t.TempDir(), "preview.mp4")
			err := Run(context.Background(), ffmpeg, inputFile(t), output, 10)
			if err == nil {
				t.Fatal("Run succeeded, want error")
			}
			if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("output exists after %s result: %v", mode, statErr)
			}
		})
	}
}

func TestRunPreservesPreexistingFileAndDanglingSymlink(t *testing.T) {
	for _, kind := range []string{"file", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			ffmpeg, dir := fakeFFmpeg(t)
			logPath, _, _ := setFakeEnv(t, dir, "success")
			input := inputFile(t)
			output := filepath.Join(t.TempDir(), "preview.mp4")
			const contents = "existing-output"
			if kind == "file" {
				if err := os.WriteFile(output, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing-target"), output); err != nil {
					t.Fatal(err)
				}
			}
			if err := Run(context.Background(), ffmpeg, input, output, 10); err == nil {
				t.Fatal("Run succeeded with preexisting output path")
			}
			if kind == "file" {
				got, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != contents {
					t.Fatalf("existing output changed to %q", got)
				}
			} else {
				target, err := os.Readlink(output)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(target, "missing-target") {
					t.Fatalf("dangling symlink target changed to %q", target)
				}
			}
			if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("ffmpeg was invoked despite existing output; log stat error %v", err)
			}
		})
	}
}

func TestRunRejectsInputAndFPSBeforeStartingFFmpeg(t *testing.T) {
	ffmpeg, dir := fakeFFmpeg(t)
	logPath, _, _ := setFakeEnv(t, dir, "success")
	missing := filepath.Join(t.TempDir(), "missing.mjpg")
	directory := t.TempDir()
	valid := inputFile(t)
	tests := []struct {
		name, input string
		fps         int
	}{
		{"missing input", missing, 10},
		{"nonregular input", directory, 10},
		{"fps below range", valid, 0},
		{"fps above range", valid, 61},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Run(context.Background(), ffmpeg, tt.input, filepath.Join(t.TempDir(), "preview.mp4"), tt.fps); err == nil {
				t.Fatal("Run succeeded, want validation error")
			}
			if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("ffmpeg was invoked before validation; log stat error %v", err)
			}
		})
	}
}

func TestRunRejectsOutputEqualToInput(t *testing.T) {
	ffmpeg, dir := fakeFFmpeg(t)
	logPath, _, _ := setFakeEnv(t, dir, "success")
	input := inputFile(t)
	if err := Run(context.Background(), ffmpeg, input, input, 10); err == nil {
		t.Fatal("Run succeeded when output equals existing input")
	}
	got, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mjpeg-frames" {
		t.Fatalf("input changed to %q", got)
	}
	if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ffmpeg was invoked despite output equal to input; log stat error %v", err)
	}
}

func TestRunAlreadyCanceledDoesNotStartChild(t *testing.T) {
	ffmpeg, dir := fakeFFmpeg(t)
	logPath, _, _ := setFakeEnv(t, dir, "success")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, ffmpeg, inputFile(t), filepath.Join(t.TempDir(), "preview.mp4"), 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child ran despite canceled context; log stat error %v", err)
	}
}

func TestRunCancellationStopsActiveChildPromptly(t *testing.T) {
	ffmpeg, dir := fakeFFmpeg(t)
	logPath, activePath, _ := setFakeEnv(t, dir, "active")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	input := inputFile(t)
	output := filepath.Join(t.TempDir(), "preview.mp4")
	go func() {
		done <- Run(ctx, ffmpeg, input, output, 10)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(activePath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			select {
			case err := <-done:
				t.Fatalf("child did not become active; Run returned %v; args log exists %t", err, fileExists(logPath))
			case <-time.After(time.Second):
				t.Fatal("Run did not finish after cancel")
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("canceling active child took %s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("canceling active child did not stop Run promptly")
	}
}

func TestRunPublicationRacePreservesRacedOutput(t *testing.T) {
	ffmpeg, dir := fakeFFmpeg(t)
	_, _, racedOutput := setFakeEnv(t, dir, "race")
	output := filepath.Join(t.TempDir(), "preview.mp4")
	// Point the fake child at the output it will create while Run is rendering.
	t.Setenv("FAKE_RACE_OUTPUT", output)
	if err := Run(context.Background(), ffmpeg, inputFile(t), output, 10); err == nil {
		t.Fatal("Run succeeded despite output being created during rendering")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "raced-output" {
		t.Fatalf("raced output changed to %q (race fixture %q)", got, racedOutput)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
