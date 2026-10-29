package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"example.com/hdcam-client/internal/config"
	mjpeg "example.com/hdcam-client/internal/export"
	"example.com/hdcam-client/internal/process"
)

func buildRequest(c config.Config, duration time.Duration, mode int) ([]byte, error) {
	return buildAVRequest(c, duration, mode, false)
}

func buildAVRequest(c config.Config, duration time.Duration, mode int, withAudio bool) ([]byte, error) {
	if err := c.ValidateAccess(); err != nil {
		return nil, err
	}
	if duration < time.Second || duration > time.Hour || duration%time.Second != 0 || (mode != 1 && mode != 2) {
		return nil, errors.New("duration must be whole seconds from 1s to 1h; video mode must be 1 or 2")
	}
	command, err := json.Marshal(struct {
		Pro       string `json:"pro"`
		Cmd       int    `json:"cmd"`
		Video     int    `json:"video"`
		CamsMode  int    `json:"camsmode"`
		User      string `json:"user"`
		Password  string `json:"pwd"`
		DeviceMAC string `json:"devmac"`
		Audio     int    `json:"audio,omitempty"`
	}{"stream", 111, mode, 0, c.Username, c.Password, "0000", func() int {
		if withAudio {
			return 1
		}
		return 0
	}()})
	if err != nil {
		return nil, err
	}
	var input bytes.Buffer
	magic := uint32(0x31434448)
	if withAudio {
		magic = 0x32434448
	}
	for _, v := range []uint32{magic, uint32(len(c.SDKInit)), uint32(len(c.UID)), uint32(len(command)), uint32(duration / time.Second)} {
		if err := binary.Write(&input, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	input.WriteString(c.SDKInit)
	input.WriteString(c.UID)
	input.Write(command)
	return input.Bytes(), nil
}

func runtimeSpec(c config.Config, request []byte) process.Spec {
	return process.Spec{
		Path:  c.QEMU,
		Args:  []string{"-L", c.Sysroot, "-E", "LD_LIBRARY_PATH=" + strings.Join(c.LibraryPath, ":"), "-E", "HDCAM_LIBRARY=" + c.Library, "-E", "LD_PRELOAD=" + c.NetworkGuard, "-E", "HDCAM_ALLOWED_SUBNET=" + c.AllowedSubnet, c.Bridge},
		Env:   []string{"PATH=" + os.Getenv("PATH"), "LANG=C"},
		Input: bytes.NewReader(request), Stderr: io.Discard,
	}
}

func Run(ctx context.Context, c config.Config, duration time.Duration, mode int, destination io.Writer) (report mjpeg.Report, err error) {
	return RunFrames(ctx, c, duration, mode, func(frame []byte) error {
		n, err := destination.Write(frame)
		if err == nil && n != len(frame) {
			return io.ErrShortWrite
		}
		return err
	})
}

func RunFrames(ctx context.Context, c config.Config, duration time.Duration, mode int, consume func([]byte) error) (report mjpeg.Report, err error) {
	return RunAV(ctx, c, duration, mode, consume, nil)
}

func RunAV(ctx context.Context, c config.Config, duration time.Duration, mode int, consume, consumeAudio func([]byte) error) (report mjpeg.Report, err error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return report, errors.New("native capture is supported only on Linux x86_64")
	}
	request, err := buildAVRequest(c, duration, mode, consumeAudio != nil)
	if err != nil {
		return report, err
	}
	bounded, cancel := context.WithTimeout(ctx, duration+30*time.Second)
	defer cancel()
	err = process.Run(bounded, runtimeSpec(c, request), func(source io.Reader) error {
		var decodeErr error
		report, decodeErr = mjpeg.AVFrames(bounded, source, consume, consumeAudio)
		return decodeErr
	})
	if err != nil {
		return report, fmt.Errorf("capture failed: %w", err)
	}
	return report, nil
}
