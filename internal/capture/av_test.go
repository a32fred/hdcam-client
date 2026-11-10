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

func TestBuildAVRequestEnablesAudioOnlyForAVMode(t *testing.T) {
	c := config.Config{UID: "TEST-UNIT", Username: "example-user", Password: "private-value", SDKInit: "private-init"}
	for _, tt := range []struct {
		name      string
		build     func() ([]byte, error)
		magic     uint32
		wantAudio bool
	}{
		{"audio", func() ([]byte, error) { return buildAVRequest(c, time.Second, 1, true) }, 0x32434448, true},
		{"legacy video", func() ([]byte, error) { return buildRequest(c, time.Second, 1) }, 0x31434448, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request, err := tt.build()
			if err != nil {
				t.Fatal(err)
			}
			if got := binary.LittleEndian.Uint32(request); got != tt.magic {
				t.Fatalf("magic %#x, want %#x", got, tt.magic)
			}
			initLen, uidLen, commandLen := int(binary.LittleEndian.Uint32(request[4:])), int(binary.LittleEndian.Uint32(request[8:])), int(binary.LittleEndian.Uint32(request[12:]))
			var command map[string]any
			if err := json.Unmarshal(request[20+initLen+uidLen:20+initLen+uidLen+commandLen], &command); err != nil {
				t.Fatal(err)
			}
			value, present := command["audio"]
			if tt.wantAudio && value != float64(1) {
				t.Fatalf("audio field = %v", value)
			}
			if !tt.wantAudio && present {
				t.Fatalf("legacy command unexpectedly includes audio: %v", value)
			}
			spec := runtimeSpec(c, request)
			public := strings.Join(append(spec.Args, spec.Env...), " ")
			if strings.Contains(public, c.Password) || strings.Contains(public, c.SDKInit) {
				t.Fatal("secret in process args/environment")
			}
			stdin, err := io.ReadAll(spec.Input)
			if err != nil || !bytes.Equal(stdin, request) {
				t.Fatalf("private request stdin changed: err=%v", err)
			}
		})
	}
}
