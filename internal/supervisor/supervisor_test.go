package supervisor

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRetriesAttemptsUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wantErr := errors.New("attempt failed")
	var attempts atomic.Int32
	var callbacks atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{MinBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, StableAfter: time.Second}, func(context.Context) error {
			n := attempts.Add(1)
			if n <= 2 {
				return wantErr
			}
			cancel()
			return nil
		}, func(error, time.Duration) { callbacks.Add(1) })
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempt count = %d, want 3", got)
	}
	if got := callbacks.Load(); got != 3 {
		t.Errorf("callback count = %d, want one per attempt (3)", got)
	}
}

func TestRunCancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{MinBackoff: time.Hour, MaxBackoff: time.Hour, StableAfter: time.Second}, func(context.Context) error {
			return errors.New("retry me")
		}, func(error, time.Duration) {
			select {
			case <-started:
			default:
				close(started)
			}
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("attempt did not start")
	}
	time.Sleep(10 * time.Millisecond)
	begin := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(begin); elapsed > 200*time.Millisecond {
			t.Errorf("cancellation took %s during one-hour backoff", elapsed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run did not interrupt backoff promptly")
	}
}

func TestRunRejectsInvalidOptions(t *testing.T) {
	valid := Options{MinBackoff: time.Millisecond, MaxBackoff: time.Second, StableAfter: time.Second}
	attempt := func(context.Context) error { return nil }
	tests := []struct {
		name string
		opts Options
	}{
		{"zero minimum", Options{MaxBackoff: time.Second, StableAfter: time.Second}},
		{"minimum above maximum", Options{MinBackoff: time.Second, MaxBackoff: time.Millisecond, StableAfter: time.Second}},
		{"zero maximum", Options{MinBackoff: time.Millisecond, StableAfter: time.Second}},
		{"zero stable duration", Options{MinBackoff: time.Millisecond, MaxBackoff: time.Second}},
		{"negative duration", Options{MinBackoff: -time.Millisecond, MaxBackoff: time.Second, StableAfter: time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Run(context.Background(), tt.opts, attempt, nil); err == nil {
				t.Fatal("Run succeeded, want validation error")
			}
		})
	}
	if err := Run(context.Background(), valid, nil, nil); err == nil {
		t.Fatal("Run accepted nil attempt")
	}
}

func TestNextBackoffCapsWithoutOverflow(t *testing.T) {
	if got := nextBackoff(time.Duration(math.MaxInt64/2+1), time.Duration(math.MaxInt64)); got != time.Duration(math.MaxInt64) {
		t.Fatalf("nextBackoff overflowed: got %s", got)
	}
}
