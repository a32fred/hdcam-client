package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorMissingTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var output, diagnostic bytes.Buffer
	if code := run([]string{"doctor"}, &output, &diagnostic); code != 1 {
		t.Fatalf("code %d", code)
	}
	var report struct {
		Tools []struct {
			Name      string
			Available bool
		}
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Tools) != 2 || report.Tools[0].Available || report.Tools[1].Available {
		t.Fatalf("report: %s", output.String())
	}
}

func TestExtractAndPreserveOutput(t *testing.T) {
	dir := t.TempDir()
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 32)
	binary.LittleEndian.PutUint32(header, 0xa815aa55)
	header[4] = 3
	binary.LittleEndian.PutUint32(header[12:], 1)
	binary.LittleEndian.PutUint32(header[16:], uint32(jpegData.Len()))
	input := filepath.Join(dir, "fixture.dat")
	data := append(header, jpegData.Bytes()...)
	data = append(data, header[:12]...)
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "result")
	var output, diagnostic bytes.Buffer
	args := []string{"extract", "--output", destination, input}
	if code := run(args, &output, &diagnostic); code != 0 {
		t.Fatalf("code %d: %s", code, diagnostic.String())
	}
	actual, err := os.ReadFile(filepath.Join(destination, "video.mjpg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, jpegData.Bytes()) {
		t.Fatal("JPEG bytes changed")
	}
	var report struct {
		Complete bool
		Frames   uint64
		Media    struct {
			TrailingBytes uint64 `json:"trailing_bytes"`
		}
	}
	encoded, err := os.ReadFile(filepath.Join(destination, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Frames != 1 || report.Media.TrailingBytes != 12 {
		t.Fatalf("report %s", encoded)
	}
	if code := run(args, &output, &diagnostic); code == 0 {
		t.Fatal("existing output accepted")
	}
	after, _ := os.ReadFile(filepath.Join(destination, "video.mjpg"))
	if !bytes.Equal(after, actual) {
		t.Fatal("existing capture changed")
	}
}

func TestUnknownCommand(t *testing.T) {
	var output, diagnostic bytes.Buffer
	if run([]string{"unknown"}, &output, &diagnostic) != 2 {
		t.Fatal("unknown command should fail")
	}
}

func TestCaptureRejectsBeforeStarting(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "not-created")
	var output, diagnostic bytes.Buffer
	args := []string{"capture", "--config", "/missing/config.json", "--duration", "1500ms", "--output", destination}
	if run(args, &output, &diagnostic) != 2 || !strings.Contains(diagnostic.String(), "duration") {
		t.Fatalf("invalid options: %s", diagnostic.String())
	}
	diagnostic.Reset()
	args[4] = "4s"
	if run(args, &output, &diagnostic) != 1 {
		t.Fatalf("missing config: %s", diagnostic.String())
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("created capture output before validating config")
	}
}

func TestExtractKeepsIncompleteResultOnFormatError(t *testing.T) {
	dir := t.TempDir()
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	h := make([]byte, 32)
	binary.LittleEndian.PutUint32(h, 0xa815aa55)
	h[4] = 3
	binary.LittleEndian.PutUint32(h[16:], uint32(jpegData.Len()))
	data := append(h, jpegData.Bytes()...)
	data = append(data, make([]byte, 32)...)
	input := filepath.Join(dir, "input.dat")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "failed")
	var output, diagnostic bytes.Buffer
	if run([]string{"extract", "--output", destination, input}, &output, &diagnostic) == 0 {
		t.Fatal("format error reported as success")
	}
	if _, err := os.Stat(filepath.Join(destination, "video.mjpg")); !os.IsNotExist(err) {
		t.Fatalf("published failed result: %v", err)
	}
	part, err := os.ReadFile(filepath.Join(destination, "video.mjpg.part"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(part, jpegData.Bytes()) {
		t.Fatal("partial result lost")
	}
	encoded, err := os.ReadFile(filepath.Join(destination, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct{ Complete bool }
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if report.Complete {
		t.Fatal("failed extraction marked complete")
	}
}
