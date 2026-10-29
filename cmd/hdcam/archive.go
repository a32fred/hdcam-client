package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"example.com/hdcam-client/internal/archive"
	"example.com/hdcam-client/internal/archivefeed"
	"example.com/hdcam-client/internal/live"
)

func validViewerName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && !strings.ContainsAny(name, ":\r\n\x00")
}
func recordCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	source := flags.String("source", "http://127.0.0.1:8080", "existing authenticated viewer origin")
	username := flags.String("username", "hdcam", "viewer username")
	tokenFile := flags.String("token-file", "", "private viewer password file")
	directory := flags.String("archive-dir", "", "resumable archive directory")
	duration := flags.Duration("duration", 30*time.Second, "recording duration; 0 means until interrupted")
	frames := flags.Int("segment-frames", 300, "video frames per segment")
	keep := flags.Int("keep-segments", 100, "completed segments to retain")
	budget := flags.Int64("max-bytes", 512<<20, "managed archive byte budget")
	audio := flags.Bool("audio", false, "record PCM audio from the existing viewer")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *tokenFile == "" || *directory == "" || !validViewerName(*username) || *duration < 0 || *duration > 24*time.Hour || *frames < 1 || *frames > 100000 || *keep < 1 || *budget < 1 {
		return 2
	}
	password, err := loadToken(*tokenFile)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	feedOptions := archivefeed.Options{Source: *source, Username: *username, Password: password, Audio: *audio}
	if err := archivefeed.Validate(feedOptions); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	writer, err := archive.Open(*directory, archive.Options{MaxFrames: *frames, Keep: *keep, MaxBytes: *budget, Audio: *audio})
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *duration > 0 {
		var bounded context.CancelFunc
		ctx, bounded = context.WithTimeout(ctx, *duration)
		defer bounded()
	}
	report, recordErr := archivefeed.Run(ctx, feedOptions, writer)
	stopped := ctx.Err() != nil && (errors.Is(recordErr, context.Canceled) || errors.Is(recordErr, context.DeadlineExceeded))
	if stopped {
		recordErr = nil
	}
	if report.VideoFrames == 0 {
		recordErr = errors.Join(recordErr, errors.New("no valid video frames received"))
	}
	if *audio && report.AudioSamples == 0 {
		recordErr = errors.Join(recordErr, errors.New("audio requested but no PCM samples received"))
	}
	recordErr = errors.Join(recordErr, writer.Close())
	summary := struct {
		archivefeed.Report
		AudioDropped uint64 `json:"audio_samples_dropped"`
		Complete     bool   `json:"complete"`
	}{report, writer.DroppedAudioSamples(), recordErr == nil}
	if err := json.NewEncoder(out).Encode(summary); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	if recordErr != nil {
		fmt.Fprintln(diagnostics, recordErr)
		return 1
	}
	return 0
}
func recordingsCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("recordings", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	directory := flags.String("archive-dir", "", "existing private archive")
	tokenFile := flags.String("token-file", "", "private viewer password file")
	username := flags.String("username", "hdcam", "viewer username")
	address := flags.String("listen", "127.0.0.1:8081", "literal loopback IP:port")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *directory == "" || *tokenFile == "" || !validViewerName(*username) {
		return 2
	}
	host, _, err := net.SplitHostPort(*address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		fmt.Fprintln(diagnostics, "recordings listener must be a literal loopback IP")
		return 2
	}
	password, err := loadToken(*tokenFile)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	reader, err := archive.NewReader(*directory)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer reader.Close()
	hub := live.New()
	hub.SetRecordings(archive.Handler(reader))
	protected := hub.HandlerWithCredentials(*username, password)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			copy := r.Clone(r.Context())
			url := *r.URL
			url.Path = "/recordings"
			copy.URL = &url
			r = copy
		}
		protected.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	served := make(chan error, 1)
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	go func() { served <- server.Serve(listener) }()
	fmt.Fprintf(out, "recordings available at http://%s/recordings\n", listener.Addr())
	select {
	case <-ctx.Done():
		err = nil
	case err = <-served:
	}
	server.Close()
	listener.Close()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	return 0
}
