package capture

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/a32fred/hdcam-client/internal/config"
)

func TestBuildRequestAndPrivateInput(t *testing.T) {
	c := config.Config{UID: "TEST-UNIT", Username: "example-user", Password: "private-value", SDKInit: "private-init", QEMU: "qemu", Bridge: "bridge", Library: "sdk", NetworkGuard: "guard", Sysroot: "android", LibraryPath: []string{"libs"}, AllowedSubnet: "192.168.56.0/24"}
	request, err := buildRequest(c, 4*time.Second, 2)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(request) != 0x31434448 || binary.LittleEndian.Uint32(request[16:]) != 4 {
		t.Fatal("wrong bridge header")
	}
	initLen := int(binary.LittleEndian.Uint32(request[4:]))
	uidLen := int(binary.LittleEndian.Uint32(request[8:]))
	commandLen := int(binary.LittleEndian.Uint32(request[12:]))
	if len(request) != 20+initLen+uidLen+commandLen {
		t.Fatal("wrong lengths")
	}
	var command struct {
		Video    int
		Password string `json:"pwd"`
	}
	if err := json.Unmarshal(request[20+initLen+uidLen:], &command); err != nil {
		t.Fatal(err)
	}
	if command.Video != 2 || command.Password != c.Password {
		t.Fatal("command changed")
	}
	spec := runtimeSpec(c, request)
	public := strings.Join(append(spec.Args, spec.Env...), " ")
	if strings.Contains(public, c.Password) || strings.Contains(public, c.SDKInit) {
		t.Fatal("secrets in process args/environment")
	}
	input, err := io.ReadAll(spec.Input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, request) {
		t.Fatal("private input changed")
	}
}

func TestBuildRequestRejectsInvalidParameters(t *testing.T) {
	c := config.Config{UID: "TEST", Username: "example", Password: "private", SDKInit: "test-init"}
	for _, item := range []struct {
		duration time.Duration
		mode     int
	}{{0, 1}, {1500 * time.Millisecond, 1}, {time.Hour + time.Second, 1}, {time.Second, 3}} {
		if _, err := buildRequest(c, item.duration, item.mode); err == nil {
			t.Fatal("invalid capture accepted")
		}
	}
}
