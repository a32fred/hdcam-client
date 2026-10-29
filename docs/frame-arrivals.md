# Frame-arrival trace

`capture --trace` writes a numeric trace of complete SDK media envelopes and a summary. It does not infer the sensor clock or use the raw field at envelope offset8 as a timestamp.

```sh
bin/hdcam capture --config /private/path/camera.json --duration 60s \
  --video-mode 1 --audio --trace --output /private/path/new-vga-measurement
bin/hdcam capture --config /private/path/camera.json --duration 60s \
  --video-mode 2 --audio --trace --output /private/path/new-qvga-measurement
```

These are live capture commands. Run them only in an agreed test window with a single camera session; they do not stop an existing viewer automatically. Keep lighting, camera position, network and audio options the same when comparing resolutions. The current viewer can be left running while preparing code/offline tests; actual paired captures require its camera session to be stopped first.

## Files and clock boundary

The new output directory is private (0700); generated files are0600:

- `frame-arrivals.jsonl`: one row per complete envelope. A failed run retains `.part` instead. Publication refuses to overwrite an existing final trace.
- `frame-trace-summary.json`: mode requested, actual first validated JPEG dimensions when available, trace statistics and `trace_complete`.
- Existing `video.mjpg`, optional `audio.wav` and `result.json` retain their normal meanings.

Rows contain `monotonic_ns`, media `type`, `stream_id`, source `sequence`, uninterpreted `raw_time_u32` and `payload_bytes`. No image/audio content, camera ID, credentials or SDK initialization are written into the trace. Treat real device traces as private research data.

The clock is Go elapsed monotonic time from this run's start. Each observation is taken immediately after the parser has read a complete envelope, before JPEG validation and media output. Complete malformed JPEG payloads are therefore counted as arrivals but rejected by the exporter; incomplete envelopes are not observations. An envelope fully consumed during cancellation can be observed without subsequently being exported.

This boundary is **not** a sensor interrupt or packet-arrival timestamp. SDK buffering, native reassembly, process pipes, synchronous decoding/output of previous frames and trace I/O influence subsequent observation times. The trace is buffered to avoid flushing on each row, but instrumentation is not zero-overhead. Existing native assembly can omit an incomplete final envelope before Go sees it.

## Summary

`received_fps = (video_frames - 1) / (last_video_ns - first_video_ns)` with nanoseconds converted to seconds. Fewer than two frames or a zero interval yields zero. This excludes startup and the tail after the last frame; it measures complete type3 envelopes consumed by the client. `valid_jpeg_frames` separately counts frames actually validated/exported.

Inter-frame gap median, nearest-rank95th percentile and maximum are in host milliseconds. They expose bursts and pauses without pretending the camera produced frames on that clock.

Sequence metrics apply only to type3: modulo32-bit wrap is continuous; duplicates, backward/reset-like transitions and missing forward IDs are distinct. A discontinuity count is not a count of missing IDs, and missing IDs are not automatically lost sensor frames. Statistics currently aggregate all type3 stream IDs; the comparison expects one video stream. The raw rows preserve stream IDs for a separate analysis if they change.

`raw_time_constant` reports whether offset8 was identical across observed video envelopes. It does not establish that field's meaning. Up to one million envelopes are accepted per trace so interval storage remains bounded; write/validation errors latch and fail the capture.

`trace_complete` describes the trace producer and its flush/close, before the outer video close/rename and final result output. Check `result.json` and the CLI exit code as well. Existing recovery of an incomplete final source envelope does not prove a complete transport tail or loss-free capture.

No nominal sensor FPS, sensor-output FPS or loss-stage attribution is derived by this command. Those require independent hardware/firmware observables and correct version provenance.
