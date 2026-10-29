# hdcam-client

An offline Go command-line tool for validating and extracting JPEG frames from HDCAM SDK stream files, with an experimental native capture path. The Go code uses the Go 1.26 standard library. Proprietary SDK/runtime binaries are not bundled.

## Build and test

```sh
go test ./...
go build -o bin/hdcam ./cmd/hdcam
```

Native ARM64 runtime binaries can be built with `bash scripts/build-native.sh`. Set `ANDROID_LIBDIR` to an external ARM64 Bionic library directory; set `ZIG` if Zig is not available as `zig` on `PATH`. This build does not require Python. See [docs/runtime.md](docs/runtime.md) for runtime setup.

## Commands

```sh
bin/hdcam doctor [--config PRIVATE_FILE]
bin/hdcam extract --output NEW_DIRECTORY STREAM_FILE
bin/hdcam capture --config PRIVATE_FILE --duration 10s --video-mode 1 --output NEW_DIRECTORY
```

`doctor` checks tool paths and, when given a configuration, validates its runtime ELF metadata. It makes no connection. Without `--config`, it exits 1 if `ffmpeg` or `qemu-aarch64` is missing from `PATH`; with a valid config, it exits 0 even if an optional `PATH` tool is missing. The config must be supplied externally as a private regular file with mode `0600`.

`extract` reads 32-byte SDK envelopes, validates JPEG payloads, and writes `video.mjpg` and `result.json` in a new output directory. Put flags before the input file. A truncated final envelope is recoverable and omitted. `complete: true` means export finished successfully, not that every camera frame was received. Corruption or a write error leaves `video.mjpg.part` and reports `complete: false` when the report can be written.

`capture` requires an external private config and runs for a whole-number duration from 1 second to 1 hour. Video mode `1` is VGA (640x480); mode `2` is QVGA (320x240). Native capture is experimental and supported only on Linux x86_64; this README does not claim a current live camera validation.

This version does not provide a browser UI, remote access, audio decoding, or MP4 rendering. Other operating systems and camera models are not claimed as supported.

See [docs/protocol.md](docs/protocol.md) for stream protocol notes and [docs/runtime.md](docs/runtime.md) for runtime details.

The module path `example.com/hdcam-client` is temporary until an owner and permanent name are chosen. No license has been selected yet; choose one before publishing the code.
