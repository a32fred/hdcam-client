package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/hdcam-client/internal/live"
	"example.com/hdcam-client/internal/status"
)

func statusCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	origin := flags.String("url", "http://127.0.0.1:8080", "viewer origin")
	username := flags.String("username", "hdcam", "viewer username")
	passwordFile := flags.String("token-file", "", "private viewer password file")
	timeout := flags.Duration("timeout", 5*time.Second, "request deadline, at most 60s")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *passwordFile == "" || !validViewerName(*username) || *timeout <= 0 || *timeout > 60*time.Second {
		return 2
	}
	password, err := loadToken(*passwordFile)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	health, err := status.Read(ctx, status.Options{URL: *origin, Username: *username, Password: password, Timeout: *timeout})
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	report := struct {
		live.Health
		DiagnosticsAvailable bool `json:"diagnostics_available"`
	}{health, health.CapturePhase != ""}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	if !health.Online {
		return 1
	}
	return 0
}
