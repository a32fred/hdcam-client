package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
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
	"example.com/hdcam-client/internal/capture"
	"example.com/hdcam-client/internal/config"
	"example.com/hdcam-client/internal/live"
	"example.com/hdcam-client/internal/record"
	"example.com/hdcam-client/internal/supervisor"
)

func loadToken(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("token must be a private regular file (0600)")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if len(value) == 0 || len(data) > 1024 || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("password must contain 1 to 1024 characters on one line")
	}
	return value, nil
}
func tokenCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("token", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	path := flags.String("output", "", "new private token file")
	if flags.Parse(args) != nil || *path == "" || flags.NArg() != 0 {
		return 2
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	file, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	_, writeErr := fmt.Fprintln(file, hex.EncodeToString(secret[:]))
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	fmt.Fprintln(out, "private token file created")
	return 0
}
func serveCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	profile := flags.String("config", "", "private camera profile")
	tokenFile := flags.String("token-file", "", "private HTTP password file")
	username := flags.String("username", "hdcam", "HTTP viewer username")
	address := flags.String("listen", "127.0.0.1:8080", "literal IP:port; non-loopback requires TLS")
	cert := flags.String("tls-cert", "", "TLS certificate file")
	key := flags.String("tls-key", "", "TLS private key file")
	mode := flags.Int("video-mode", 1, "1=VGA, 2=QVGA")
	withAudio := flags.Bool("audio", false, "enable microphone capture and independently timed browser audio")
	recording := flags.String("record-dir", "", "optional new segmented recording directory")
	frames := flags.Int("segment-frames", 300, "frames per segment; no fixed time implied")
	keep := flags.Int("keep-segments", 12, "completed segments to retain")
	archiveDir := flags.String("archive-dir", "", "resumable A/V archive directory")
	archiveBytes := flags.Int64("archive-max-bytes", 512<<20, "managed archive byte budget including preserved partials")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *profile == "" || *tokenFile == "" || (*mode != 1 && *mode != 2) || *frames <= 0 || *keep <= 0 {
		return 2
	}
	if !validViewerName(*username) {
		fmt.Fprintln(diagnostics, "invalid viewer username")
		return 2
	}
	if (*recording != "" && *archiveDir != "") || *archiveBytes <= 0 {
		fmt.Fprintln(diagnostics, "choose one recording format and a positive archive byte budget")
		return 2
	}
	host, _, err := net.SplitHostPort(*address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || (*cert == "") != (*key == "") || (!ip.IsLoopback() && *cert == "") {
		fmt.Fprintln(diagnostics, "use a literal loopback IP, or supply --tls-cert and --tls-key for remote listening")
		return 2
	}
	token, err := loadToken(*tokenFile)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	var tlsConfig *tls.Config
	if *cert != "" {
		pair, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 1
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	}
	c, err := config.Load(*profile)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	var recorder *record.Recorder
	if *recording != "" {
		recorder, err = record.New(*recording, *frames, *keep)
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 1
		}
	}
	var archival *archive.Writer
	var catalog *archive.Reader
	if *archiveDir != "" {
		archival, err = archive.Open(*archiveDir, archive.Options{MaxFrames: *frames, Keep: *keep, MaxBytes: *archiveBytes, Audio: *withAudio})
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 1
		}
		catalog, err = archive.NewReader(*archiveDir)
		if err != nil {
			archival.Close()
			fmt.Fprintln(diagnostics, err)
			return 1
		}
		defer catalog.Close()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	hub := live.NewWithAudio(*withAudio)
	if catalog != nil {
		hub.SetRecordings(archive.Handler(catalog))
	}
	server := &http.Server{Addr: *address, TLSConfig: tlsConfig, Handler: hub.HandlerWithCredentials(*username, token), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		if recorder != nil {
			recorder.Close()
		}
		if archival != nil {
			archival.Close()
		}
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	// Load certificates before starting camera work so a TLS setup failure is immediate.
	served := make(chan error, 1)
	go func() {
		if tlsConfig != nil {
			served <- server.ServeTLS(listener, "", "")
		} else {
			served <- server.Serve(listener)
		}
	}()
	captured := make(chan struct{})
	go func() {
		defer close(captured)
		supervisor.Run(ctx, supervisor.Options{MinBackoff: time.Second, MaxBackoff: 30 * time.Second, StableAfter: time.Minute}, func(attemptCtx context.Context) error {
			var recordErr error
			err := live.Watch(attemptCtx, 45*time.Second, 15*time.Second, func(watchCtx context.Context, touch func()) error {
				var audioConsumer func([]byte) error
				if *withAudio {
					audioConsumer = func(pcm []byte) error {
						hub.PublishAudio(pcm)
						if archival != nil {
							recordErr = archival.WriteAudio(pcm)
							if recordErr != nil {
								cancel()
							}
							return recordErr
						}
						return nil
					}
				}
				_, err := capture.RunAV(watchCtx, c, time.Hour, *mode, func(frame []byte) error {
					touch()
					hub.Publish(frame)
					if archival != nil {
						recordErr = archival.WriteVideo(frame)
						if recordErr != nil {
							cancel()
							return recordErr
						}
					}
					if recorder != nil {
						recordErr = recorder.WriteFrame(frame)
						if recordErr != nil {
							cancel()
							return recordErr
						}
					}
					return nil
				}, audioConsumer)
				return err
			})
			if archival != nil {
				if flushErr := archival.Flush(); flushErr != nil {
					cancel()
					return flushErr
				}
			}
			if recordErr != nil {
				return recordErr
			}
			return err
		}, func(_ error, delay time.Duration) {
			if ctx.Err() == nil {
				fmt.Fprintf(diagnostics, "capture ended; retrying in %s\n", delay)
			}
		})
	}()
	fmt.Fprintf(out, "listening on %s; HTTP user: %s; password: contents of token file\n", listener.Addr(), *username)
	var serverErr error
	select {
	case serverErr = <-served:
		cancel()
	case <-ctx.Done():
	case <-captured:
		cancel()
	}
	cancel()
	// Cancel active streams too: Shutdown alone waits for long-lived viewers.
	server.Close()
	listener.Close()
	<-captured
	var closeErr error
	if recorder != nil {
		closeErr = recorder.Close()
	}
	if archival != nil {
		closeErr = errors.Join(closeErr, archival.Close())
		if dropped := archival.DroppedAudioSamples(); dropped > 0 {
			fmt.Fprintf(diagnostics, "archive omitted %d audio samples at segment boundaries\n", dropped)
		}
	}
	if closeErr != nil {
		fmt.Fprintln(diagnostics, closeErr)
		return 1
	}
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		fmt.Fprintln(diagnostics, serverErr)
		return 1
	}
	return 0
}
