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

- WebSocket browser video with bounded acknowledgement flow, latest-frame delivery and same-origin checks; snapshot fallback remains.
- Cancel obsolete image decodes and reconnect after tab visibility/Back-Forward cache changes.
- Propagate application cancellation to upgraded connections during shutdown.

- Capture phase, retry/failure counters, recent received frame rate and frame age in the protected health endpoint.
- `status` reads legacy/current viewer health with bounded requests, private-file authentication and sanitized errors.
- Native startup/stream errors retain their stage even when joined with an empty media output error.

- Optional capture arrival tracing: monotonic per-envelope JSONL, raw counters and received-frame/gap statistics, with private partial-safe output.
- Separate complete-envelope arrival observations from JPEG validation and source timestamps; no sensor-rate claim is inferred.
