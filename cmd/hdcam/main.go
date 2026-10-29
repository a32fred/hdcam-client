package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"example.com/hdcam-client/internal/capture"
	"example.com/hdcam-client/internal/config"
	"example.com/hdcam-client/internal/diagnostic"
	mjpeg "example.com/hdcam-client/internal/export"
	"example.com/hdcam-client/internal/preview"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, output, diagnosticOutput io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(output, "Usage: hdcam doctor [--config FILE] | hdcam extract --output NEW_DIRECTORY STREAM_FILE | hdcam capture --config FILE --duration 10s --video-mode 1 --output NEW_DIRECTORY | hdcam token --output FILE | hdcam serve --config FILE --token-file FILE | hdcam preview --output NEW_MP4 --fps 10 INPUT_MJPEG")
		fmt.Fprintln(output, "Recording: hdcam record --source VIEWER_ORIGIN --token-file FILE --archive-dir DIRECTORY [--audio] [--duration 30s]")
		fmt.Fprintln(output, "Playback: hdcam recordings --archive-dir DIRECTORY --token-file FILE [--listen 127.0.0.1:8081]")
		fmt.Fprintln(output, "Status: hdcam status --url VIEWER_ORIGIN --token-file FILE [--username VIEWER_NAME]")
		return 0
	}
	switch args[0] {
	case "status":
		return statusCommand(args[1:], output, diagnosticOutput)
	case "record":
		return recordCommand(args[1:], output, diagnosticOutput)
	case "recordings":
		return recordingsCommand(args[1:], output, diagnosticOutput)
	case "discover":
		return discoverCommand(args[1:], output, diagnosticOutput)
	case "wifi-plan":
		return wifiPlanCommand(args[1:], output, diagnosticOutput)
	case "preview":
		flags := flag.NewFlagSet("preview", flag.ContinueOnError)
		flags.SetOutput(diagnosticOutput)
		destination := flags.String("output", "", "new MP4 file")
		fps := flags.Int("fps", 10, "assumed constant preview frame rate, not camera timing")
		ffmpeg := flags.String("ffmpeg", "ffmpeg", "FFmpeg executable")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 1 || *destination == "" || *fps < 1 || *fps > 60 {
			return 2
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := preview.Run(ctx, *ffmpeg, flags.Arg(0), *destination, *fps); err != nil {
			fmt.Fprintln(diagnosticOutput, err)
			return 1
		}
		fmt.Fprintf(output, "preview rendered at assumed %d fps; camera timing is not reconstructed\n", *fps)
		return 0
	case "token":
		return tokenCommand(args[1:], output, diagnosticOutput)
	case "serve":
		return serveCommand(args[1:], output, diagnosticOutput)
	case "doctor":
		flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
		flags.SetOutput(diagnosticOutput)
		profile := flags.String("config", "", "private capture configuration")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return 2
		}
		checked := false
		if *profile != "" {
			if _, err := config.Load(*profile); err != nil {
				fmt.Fprintln(diagnosticOutput, err)
				return 1
			}
			checked = true
		}
		tools := diagnostic.CheckTools([]string{"ffmpeg", "qemu-aarch64"})
		if err := json.NewEncoder(output).Encode(struct {
			Tools            []diagnostic.ToolResult `json:"tools"`
			RuntimeChecked   bool                    `json:"runtime_checked"`
			ConnectionTested bool                    `json:"connection_tested"`
		}{tools, checked, false}); err != nil {
			return 1
		}
		if checked {
			return 0
		}
		for _, tool := range tools {
			if !tool.Available {
				return 1
			}
		}
		return 0
	case "capture":
		flags := flag.NewFlagSet("capture", flag.ContinueOnError)
		flags.SetOutput(diagnosticOutput)
		profile := flags.String("config", "", "private capture configuration")
		duration := flags.Duration("duration", 10*time.Second, "whole seconds, 1s to 1h")
		mode := flags.Int("video-mode", 1, "1=640x480, 2=320x240")
		withAudio := flags.Bool("audio", false, "capture microphone audio to WAV without A/V synchronization")
		destination := flags.String("output", "", "new output directory")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *duration < time.Second || *duration > time.Hour || *duration%time.Second != 0 || (*mode != 1 && *mode != 2) {
			fmt.Fprintln(diagnosticOutput, "duration must be whole seconds from 1s to 1h; video mode must be 1 or 2")
			return 2
		}
		if *profile == "" || *destination == "" || flags.NArg() != 0 {
			fmt.Fprintln(diagnosticOutput, "capture requires --config and --output")
			return 2
		}
		c, err := config.Load(*profile)
		if err != nil {
			fmt.Fprintln(diagnosticOutput, err)
			return 1
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		err = writeOutput(*destination, output, func(dst io.Writer) (mjpeg.Report, error) {
			if !*withAudio {
				return capture.Run(ctx, c, *duration, *mode, dst)
			}
			return withAudioFile(*destination, func(pcm func([]byte) error) (mjpeg.Report, error) {
				return capture.RunAV(ctx, c, *duration, *mode, jpegSink(dst), pcm)
			})
		})
		if err != nil {
			fmt.Fprintln(diagnosticOutput, err)
			return 1
		}
		return 0
	case "extract":
		flags := flag.NewFlagSet("extract", flag.ContinueOnError)
		flags.SetOutput(diagnosticOutput)
		destination := flags.String("output", "", "new output directory")
		withAudio := flags.Bool("audio", false, "decode type-11 audio to a separate WAV")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if *destination == "" || flags.NArg() != 1 {
			fmt.Fprintln(diagnosticOutput, "Usage: hdcam extract --output NEW_DIRECTORY STREAM_FILE")
			return 2
		}
		if err := extractAV(flags.Arg(0), *destination, output, *withAudio); err != nil {
			fmt.Fprintln(diagnosticOutput, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(diagnosticOutput, "unknown command:", args[0])
		return 2
	}
}

func extract(input, destination string, output io.Writer) error {
	return extractAV(input, destination, output, false)
}

func extractAV(input, destination string, output io.Writer, withAudio bool) error {
	source, err := os.Open(input)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input must be a regular file")
	}
	return writeOutput(destination, output, func(dst io.Writer) (mjpeg.Report, error) {
		if !withAudio {
			return mjpeg.WriteMJPEG(context.Background(), source, dst)
		}
		return withAudioFile(destination, func(pcm func([]byte) error) (mjpeg.Report, error) {
			return mjpeg.AVFrames(context.Background(), source, jpegSink(dst), pcm)
		})
	})
}

func writeOutput(destination string, output io.Writer, produce func(io.Writer) (mjpeg.Report, error)) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return fmt.Errorf("output directory must be new: %w", err)
	}
	partial := filepath.Join(destination, "video.mjpg.part")
	video, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	report, captureErr := produce(video)
	closeErr := video.Close()
	if captureErr == nil {
		captureErr = closeErr
	}
	if captureErr == nil {
		captureErr = os.Rename(partial, filepath.Join(destination, "video.mjpg"))
	}
	result := struct {
		mjpeg.Report
		Complete bool `json:"complete"`
	}{report, captureErr == nil}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destination, "result.json"), append(encoded, '\n'), 0600); err != nil {
		return err
	}
	if captureErr != nil {
		return captureErr
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
