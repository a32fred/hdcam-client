package process

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

type Spec struct {
	Path   string
	Args   []string
	Env    []string
	Input  io.Reader
	Stderr io.Writer
}

func Run(ctx context.Context, spec Spec, consume func(io.Reader) error) error {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Env = spec.Env
	cmd.Stdin = spec.Input
	cmd.Stderr = spec.Stderr
	cmd.WaitDelay = time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stdout.Close()
		case <-done:
		}
	}()

	consumerErr := consume(stdout)
	_ = stdout.Close()
	if consumerErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	close(done)

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(consumerErr, waitErr)
}
