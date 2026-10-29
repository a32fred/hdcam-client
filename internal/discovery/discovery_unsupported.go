//go:build !linux

package discovery

import (
	"context"
	"errors"
	"time"
)

func Run(context.Context, string, time.Duration) ([]Device, error) {
	return nil, errors.New("LAN discovery is supported only on Linux")
}
