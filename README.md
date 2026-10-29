# hdcam-client

A Go client for validating HDCAM SDK streams, experimental local video/audio capture, authenticated browser viewing, segmented video recording and reconnect supervision. Own code uses the Go 1.26 standard library, embedded browser JavaScript and a small C bridge. Proprietary SDK/runtime binaries are supplied separately; this is not an independent implementation of the vendor's transport.

## Build and test

```sh
go test ./...
go vet ./...
go test -race ./...
go build -o bin/hdcam ./cmd/hdcam
```

Native ARM64 runtime binaries can be built with `bash scripts/build-native.sh`. Set `ANDROID_LIBDIR` to an external ARM64 Bionic library directory and `ZIG` to the installed Zig executable if needed. No Python is required. See [installation](docs/installation.md) and [runtime setup](docs/runtime.md).

## Commands

```sh
bin/hdcam doctor [--config PRIVATE_FILE]
bin/hdcam extract --output NEW_DIRECTORY STREAM_FILE
bin/hdcam capture --config PRIVATE_FILE --duration 10s --video-mode 1 --output NEW_DIRECTORY
bin/hdcam capture --audio --config PRIVATE_FILE --duration 10s --output NEW_DIRECTORY
bin/hdcam preview --output NEW_FILE.mp4 --fps 10 INPUT_MJPEG
bin/hdcam token --output NEW_PRIVATE_TOKEN_FILE
bin/hdcam serve --config PRIVATE_FILE --token-file PRIVATE_TOKEN_FILE
bin/hdcam serve --audio --config PRIVATE_FILE --token-file PRIVATE_TOKEN_FILE
bin/hdcam discover --interface CAMERA_LAN_INTERFACE --duration 3s
bin/hdcam wifi-plan --config PRIVATE_FILE --wifi-profile PRIVATE_WIFI_FILE
bin/hdcam record --source http://127.0.0.1:8080 --token-file PRIVATE_TOKEN_FILE --archive-dir PRIVATE_ARCHIVE --audio --duration 30s
bin/hdcam recordings --archive-dir PRIVATE_ARCHIVE --token-file PRIVATE_TOKEN_FILE
```

`doctor` validates tool paths and optional runtime ELF metadata without connecting. Without `--config`, missing FFmpeg or QEMU on PATH returns exit 1; a valid configured runtime returns exit 0 independently of optional PATH tools. Profiles must be private regular files (0600).

`extract` validates the 32-byte media envelopes and JPEG payloads, then writes `video.mjpg` and `result.json` in a new private output directory. A truncated last envelope is reported and omitted. `complete: true` means export finished, not that every camera frame was received. Failure preserves `.part` and writes `complete: false` when possible. Put flags before the input path.

`capture` uses the external SDK backend on Linux x86_64. Duration must be whole seconds from 1s to 1h. Mode 1 is VGA (640×480); mode 2 is QVGA (320×240). These modes were privately exercised on one camera; support for other camera families or capture platforms is not established.

`preview` uses external FFmpeg to render a silent H.264 MP4 at an **assumed constant frame rate**. It does not reconstruct camera timestamps or synchronize audio. Existing outputs are never replaced.

`serve` opens one camera session and distributes validated frames to authenticated browser viewers. Snapshot polling displays the latest frame without buffering an old stream; multipart MJPEG is also available. It retries failed/frozen captures and optionally records retained video segments. `--audio` explicitly enables microphone capture and independent browser sound with adjustable amplification. Remote listeners require TLS. See [viewing and remote access](docs/viewing.md) and [post-reset recovery](docs/recovery.md). Remote networking must already be available; the client does not configure a VPN, router or cloud relay.

One-shot `capture`/`extract --audio` writes PCM WAV alongside video. The Go ADPCM decoder matched the original decoder byte for byte on the private reference recording; live browser audio is integrated, with no guaranteed A/V synchronization. Discovery can find candidate UID/MAC/IP values on an explicitly selected Linux interface. `wifi-plan` validates and constructs a post-reset provisioning request in memory but sends nothing; factory-reset recovery is not hardware-validated. Motion detection and cross-platform native capture remain unimplemented. No flash, OTA or automatic Wi-Fi changes are performed.

`record` archives video/audio from an already running viewer without opening another camera session or restarting it. `recordings` serves protected playback separately. New Linux archives retain completed segments by count and managed file bytes, preserve interrupted parts, and resume after reopening. `serve --archive-dir` can attach the same recorder/player directly on a future start. See [recordings](docs/recordings.md) for quotas, timeline limitations and commands; the legacy `--record-dir` format remains video-only.

Protocol details are in [docs/protocol.md](docs/protocol.md). The example user service is optional and is not installed automatically.

The module path `example.com/hdcam-client` is temporary until an owner and permanent name are chosen. No license has been selected yet; choose one before publishing. See [THIRD_PARTY.md](THIRD_PARTY.md) for external runtime boundaries.
