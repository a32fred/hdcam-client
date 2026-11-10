package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/a32fred/hdcam-client/internal/discovery"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func discoverCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	iface := flags.String("interface", "", "explicit local camera LAN interface")
	duration := flags.Duration("duration", 3*time.Second, "discovery window, 1s to 10s")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *iface == "" || *duration < time.Second || *duration > 10*time.Second {
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	devices, err := discovery.Run(ctx, *iface, *duration)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	if err := json.NewEncoder(out).Encode(devices); err != nil {
		return 1
	}
	return 0
}
