//go:build linux

package discovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunRejectsBeforeOpeningSocket(t *testing.T) {
	for _, duration := range []time.Duration{0, time.Second - 1, 10*time.Second + 1} {
		if _, err := Run(context.Background(), "no-interface-needed", duration); err == nil {
			t.Fatal("invalid duration accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "no-interface-needed", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
}
