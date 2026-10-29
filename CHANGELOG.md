# Changelog

## Unreleased

- Go streaming media parser and JPEG validation/extraction.
- Experimental Linux x86_64 capture using an external ARM64 SDK/runtime.
- Private configuration, runtime diagnostics and cancelable isolated subprocesses.
- Authenticated browser MJPEG and snapshots, with TLS required for non-loopback listeners.
- A shared latest-frame feed for multiple viewers, capture watchdog and reconnect backoff.
- Segmented MJPEG recording with completed-segment retention.
- Optional FFmpeg MP4 previews at an explicitly assumed constant frame rate.
- PCM WAV writer; vendor ADPCM capture/decoding is not integrated.
- Offline tests, fuzz target, native guard tests and CI definition.
