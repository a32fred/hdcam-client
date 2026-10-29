# Changelog

## Unreleased

- Go streaming media parser and JPEG validation/extraction.
- Experimental Linux x86_64 capture using an external ARM64 SDK/runtime.
- Private configuration, runtime diagnostics and cancelable isolated subprocesses.
- Authenticated browser MJPEG and snapshots, with TLS required for non-loopback listeners.
- A shared latest-frame feed for multiple viewers, capture watchdog and reconnect backoff.
- Segmented MJPEG recording with completed-segment retention.
- Optional FFmpeg MP4 previews at an explicitly assumed constant frame rate.
- PCM WAV writing and optional microphone capture/decoding.
- Offline tests, fuzz target, native guard tests and CI definition.

- Optional simultaneous video/audio capture from one SDK session; independent Go ADPCM decoder and streaming WAV export.
- Browser sound with amplification and signal level, off until explicitly enabled on the server and started by the viewer.
- Low-latency SDK reads and latest-frame snapshot polling to avoid proxy-buffered video bursts.
- Linux LAN discovery with explicit interface binding and bounded replies.
- Offline post-reset Wi-Fi request preparation; no credential transmission or automatic reset.

- Resumable Linux A/V archives with atomic segment directories, WAV files, host-arrival timelines and count/byte retention.
- `record` subscribes to an existing authenticated viewer without a second SDK session; `recordings` provides separate protected playback.
- Browser playback with seek/pause, WAV range requests and delayed-response regressions.
- `serve --archive-dir` can attach recording/playback directly; legacy flat video recording remains available separately.
