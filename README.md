# hdcam-client

[![Go checks](https://github.com/a32fred/hdcam-client/actions/workflows/ci.yml/badge.svg)](https://github.com/a32fred/hdcam-client/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8.svg)](https://go.dev/dl/)

A Go client for HDCAM-family cameras: offline stream parsing and extraction,
experimental local video/audio capture, authenticated browser viewing, segmented
recording and reconnect supervision.

**Status: v0.1.0-alpha, experimental.** The offline half (parse, extract, preview)
works on any machine with Go. The capture half needs an external ARM64
proprietary runtime that is **not** distributed here. Read
[Scope and limitations](#scope-and-limitations) before you invest time.

## What this is not

This is not a clean-room implementation of the vendor's transport. Capture goes
through the manufacturer SDK, loaded by a small C bridge. No SDK binary,
firmware image, APK, decompiled source or packet capture is included in this
repository, and none may be redistributed under this project's license.

## Requirements

| Part | Needs |
|---|---|
| Parse, extract, preview | Go 1.26+, FFmpeg for MP4 preview only |
| Browser viewing | Go 1.26+, a modern browser |
| Live capture | Linux x86_64, QEMU, an ARM64 Bionic sysroot, and a manufacturer SDK you obtained legitimately |
| Browser logic tests | Node 22+ (no npm packages) |

Windows and macOS builds of the offline tools are expected to work. Live capture
on those platforms is **not** validated and is not claimed.

## Install

```sh
git clone https://github.com/a32fred/hdcam-client.git
cd hdcam-client
go build -o bin/hdcam ./cmd/hdcam
```

Or without cloning:

```sh
go install github.com/a32fred/hdcam-client/cmd/hdcam@latest
```

## Quick start: offline, no camera

Extract frames from an existing stream file and render a preview:

```sh
bin/hdcam extract --output out-dir stream.bin
bin/hdcam preview --output preview.mp4 --fps 10 out-dir/video.mjpg
```

`extract` validates the 32-byte media envelopes and JPEG payloads, then writes
`video.mjpg` and `result.json` into a new private directory. A truncated final
envelope is reported and omitted. `complete: true` means the export finished,
not that every camera frame arrived.

## Quick start: live capture

```sh
bin/hdcam doctor --config private.json          # verify paths, no camera connection
bin/hdcam capture --config private.json --duration 10s --video-mode 1 --output out-dir
```

Mode `1` is VGA (640×480), mode `2` is QVGA (320×240). Duration must be whole
seconds, 1s to 1h. These modes were exercised on one camera; support for other
camera families is not established.

Then view it in a browser:

```sh
bin/hdcam token --output private.token
bin/hdcam serve --config private.json --token-file private.token
```

## Commands

```sh
bin/hdcam doctor     [--config PRIVATE_FILE]
bin/hdcam status     --url http://127.0.0.1:8080 --token-file PRIVATE_TOKEN_FILE
bin/hdcam extract    --output NEW_DIRECTORY STREAM_FILE
bin/hdcam capture    --config PRIVATE_FILE --duration 10s --video-mode 1 --output NEW_DIRECTORY
bin/hdcam capture    --audio --config PRIVATE_FILE --duration 10s --output NEW_DIRECTORY
bin/hdcam capture    --trace --audio --config PRIVATE_FILE --duration 60s --video-mode 1 --output NEW_DIRECTORY
bin/hdcam preview    --output NEW_FILE.mp4 --fps 10 INPUT_MJPEG
bin/hdcam token      --output NEW_PRIVATE_TOKEN_FILE
bin/hdcam serve      --config PRIVATE_FILE --token-file PRIVATE_TOKEN_FILE
bin/hdcam discover   --interface CAMERA_LAN_INTERFACE --duration 3s
bin/hdcam wifi-plan  --config PRIVATE_FILE --wifi-profile PRIVATE_WIFI_FILE
bin/hdcam record     --source http://127.0.0.1:8080 --token-file PRIVATE_TOKEN_FILE --archive-dir PRIVATE_ARCHIVE --audio --duration 30s
bin/hdcam recordings --archive-dir PRIVATE_ARCHIVE --token-file PRIVATE_TOKEN_FILE
```

Put flags before positional arguments. Profiles and token files must be private
regular files with mode `0600`.

## Documentation

| Page | Covers |
|---|---|
| [installation](docs/installation.md) | Build, native toolchain, dependencies |
| [runtime](docs/runtime.md) | External SDK/runtime setup and boundaries |
| [protocol](docs/protocol.md) | Envelope format, magic, framing |
| [viewing](docs/viewing.md) | Browser viewing, authentication, TLS |
| [recordings](docs/recordings.md) | Archive layout, quotas, playback |
| [diagnostics](docs/diagnostics.md) | `doctor`, `status`, health fields |
| [frame arrivals](docs/frame-arrivals.md) | Arrival tracing and its measurement boundary |
| [recovery](docs/recovery.md) | Post-reset Wi-Fi recovery status |

## Scope and limitations

Stated plainly, because the alternative is a bug report you did not deserve.

- **Capture is Linux x86_64 only.** It requires QEMU plus an ARM64 Bionic
  sysroot plus a manufacturer SDK you supply. `doctor` tells you what is missing.
- **One camera, one session per run.** No multi-camera, no plugin architecture.
- **Preview frame rate is assumed, not measured.** `preview` renders at a
  constant rate you pass in. It does not reconstruct camera timestamps and does
  not synchronise audio.
- **Audio/video sync is not guaranteed.** Audio decoding matched the reference
  decoder byte for byte on one private recording; live A/V timing is unproven.
- **Frame gaps are observed, not explained.** The capture stream shows counter
  jumps. No constant rate has been demonstrated and no single cause identified.
- **No firmware work.** No flash, OTA, factory reset or automatic Wi-Fi change.
  `wifi-plan` builds and validates a provisioning request in memory and sends
  nothing.
- **The network guard is a compatibility filter**, not a sandbox. It does not
  isolate the camera itself from the internet.
- **Motion detection is not implemented.**

## Security

Never commit camera UID, MAC, SSID, passwords, SDK init strings, private
profiles, recordings, firmware or packet captures. Report vulnerabilities
privately, as described in [SECURITY.md](SECURITY.md). Browser viewing requires
a separate password, and TLS is required for any non-loopback listener.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md)
first: tests must run offline, without a camera, SDK or network. By
participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

Source code in this repository is [MIT](LICENSE). Third-party components keep
their own terms; see [THIRD_PARTY.md](THIRD_PARTY.md). The MIT grant does not
extend to any SDK, runtime or firmware you supply yourself.
