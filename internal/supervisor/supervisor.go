// Package supervisor runs an attempt repeatedly until its context is canceled.
package supervisor

import (
	"context"
	"errors"
	"time"
)

type Attempt func(context.Context) error

type Options struct {
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
	StableAfter time.Duration
}

// Run retries attempt after every return, including nil, until ctx is canceled.
// onRetry is called once after each attempt and before its retry delay.
func Run(ctx context.Context, opts Options, attempt Attempt, onRetry func(error, time.Duration)) error {
	if err := validate(opts, attempt); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("supervisor: nil context")
	}

	delay := opts.MinBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		err := attempt(ctx)
		duration := time.Since(started)
		if duration >= opts.StableAfter {
			delay = opts.MinBackoff
		}
		if onRetry != nil {
			onRetry(err, delay)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
		delay = nextBackoff(delay, opts.MaxBackoff)
	}
}

func validate(opts Options, attempt Attempt) error {
	if attempt == nil {
		return errors.New("supervisor: nil attempt")
	}
	if opts.MinBackoff <= 0 || opts.MaxBackoff <= 0 || opts.StableAfter <= 0 || opts.MinBackoff > opts.MaxBackoff {
		return errors.New("supervisor: require 0 < MinBackoff <= MaxBackoff and StableAfter > 0")
	}
	return nil
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current >= max || current > max/2 {
		return max
	}
	return current * 2
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
