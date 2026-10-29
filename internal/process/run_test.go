package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

const helperEnv = "HDCAM_PROCESS_TEST_HELPER"

func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	mode := ""
	for i, arg := range os.Args {
		if arg == "--process-test-helper" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}
	switch mode {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "exit":
		fmt.Fprint(os.Stderr, "helper failure")
		os.Exit(7)
	case "sleep":
		time.Sleep(30 * time.Second)
	case "write-forever":
		chunk := bytes.Repeat([]byte("x"), 32*1024)
		for {
			if _, err := os.Stdout.Write(chunk); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	case "stderr":
		fmt.Fprint(os.Stderr, "diagnostic from child")
	}
	os.Exit(0)
}

func helperSpec(mode string, input io.Reader, stderr io.Writer) Spec {
	return Spec{
		Path:   os.Args[0],
		Args:   []string{"-test.run=^TestHelperProcess$", "--", "--process-test-helper", mode},
		Env:    []string{helperEnv + "=1"},
		Input:  input,
		Stderr: stderr,
	}
}

func boundedContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Second)
}

func TestRunStreamsBinaryStdinAndStdout(t *testing.T) {
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(i * 31)
	}
	var output bytes.Buffer
	ctx, cancel := boundedContext(t)
	defer cancel()
	err := Run(ctx, helperSpec("echo", bytes.NewReader(input), io.Discard), func(r io.Reader) error {
		_, err := io.Copy(&output, r)
		return err
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !bytes.Equal(output.Bytes(), input) {
		t.Fatalf("binary echo mismatch: got %d bytes, want %d", output.Len(), len(input))
	}
}

func TestRunReturnsWaitError(t *testing.T) {
	ctx, cancel := boundedContext(t)
	defer cancel()
	err := Run(ctx, helperSpec("exit", nil, io.Discard), func(io.Reader) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("want child exit status 7, got %v", err)
	}
}

func TestRunReturnsStartError(t *testing.T) {
	ctx, cancel := boundedContext(t)
	defer cancel()
	err := Run(ctx, Spec{Path: "/definitely/missing/hdcam-helper"}, func(io.Reader) error { return nil })
	if err == nil {
		t.Fatal("expected executable start error")
	}
}

func TestRunAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, helperSpec("sleep", nil, io.Discard), func(io.Reader) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRunCancellationTerminatesChild(t *testing.T) {
	ctx, cancel := boundedContext(t)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, helperSpec("sleep", nil, io.Discard), func(io.Reader) error { return nil })
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not terminate child promptly after cancellation")
	}
}

func TestRunConsumerErrorKillsAndWaitsForChild(t *testing.T) {
	ctx, cancel := boundedContext(t)
	defer cancel()
	consumerErr := errors.New("consumer stopped")
	start := time.Now()
	err := Run(ctx, helperSpec("write-forever", nil, io.Discard), func(r io.Reader) error {
		buf := make([]byte, 1)
		if _, err := io.ReadFull(r, buf); err != nil {
			return err
		}
		return consumerErr
	})
	if !errors.Is(err, consumerErr) {
		t.Fatalf("consumer error was not preserved: %v", err)
	}
	if time.Since(start) >= 2*time.Second {
		t.Fatal("Run did not kill and wait for child promptly after consumer error")
	}
}

func TestRunCapturesStderr(t *testing.T) {
	var stderr bytes.Buffer
	ctx, cancel := boundedContext(t)
	defer cancel()
	err := Run(ctx, helperSpec("stderr", nil, &stderr), func(io.Reader) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := stderr.String(), "diagnostic from child"; got != want {
		t.Fatalf("stderr=%q, want %q", got, want)
	}
}
