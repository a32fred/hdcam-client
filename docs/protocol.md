# Media stream format

The reader handles the media envelope observed at the SDK output, not a raw UDP packet or a complete network protocol implementation.

Each envelope starts with a 32-byte little-endian header:

| Offset | Bytes | Field |
|---|---|---|
| 0 | 4 | Magic `0xa815aa55` (`55 aa 15 a8` on the wire) |
| 4 | 1 | Media type; type 3 is JPEG video in the supported extraction path |
| 5 | 1 | Raw stream identifier |
| 6 | 2 | Auxiliary field; not interpreted |
| 8 | 4 | Raw timestamp; not interpreted as UTC |
| 12 | 4 | Video sequence counter |
| 16 | 4 | Payload length |
| 20 | 12 | Reserved/uninterpreted bytes |
| 32 | variable | Payload |

The default payload limit is 4 MiB. Reader calls may fragment headers or payloads at any byte boundary. The reader preserves source errors delivered alongside data and distinguishes a clean EOF from an incomplete final envelope. A format error is terminal; the reader does not search ahead for a plausible new header.

Statistics count bytes consumed, complete envelopes, consumed trailing bytes and video counter discontinuities. A 32-bit counter wrap from its maximum value to zero is continuous. Positive gaps count skipped IDs; duplicate or backwards counters are discontinuities without being counted as billions of missing frames. Counters do not identify whether the camera, SDK or network caused a gap.

The exporter skips non-video envelopes. Each video payload must contain exactly one JPEG, optionally followed by up to three zero-valued alignment bytes. The exporter strips this padding; nonzero trailing data, four or more zero bytes, or a second JPEG are rejected. Width and height are limited to 4096 each, and the combined pixel count is limited to 8,847,360 before full decoding. These are processing limits, not claims about camera capabilities.

`result.json` distinguishes:

- `frames`: canonical JPEGs fully validated and written after padding removal.
- `non_video_frames`: complete envelopes skipped because their type is not video.
- `rejected_frames`: video payloads rejected by image validation.
- `media`: envelope statistics, including complete envelopes even when their image payload is rejected.
- `complete`: the export completed successfully; it does not guarantee that every source/camera frame existed or was received.

A truncated final envelope is omitted and reported in `media.trailing_bytes`. Other source/format/write errors return failure with the report accumulated so far. The CLI preserves the partial MJPEG file under `.part` and writes `complete:false` when it can write the report. A metadata-output failure may leave an otherwise valid MJPEG file while the command returns nonzero.

The raw timestamps are not used to invent frame timing. Extraction produces JPEG concatenation; the separate `preview` command renders a silent MP4 at an explicitly assumed constant frame rate. Audio/video synchronization is not implemented.

## Audio

With an audio callback enabled, type-11 payloads are decoded independently to PCM16LE, mono at the observed 8 kHz rate. Payloads must be 1–2048 bytes. The nibble order is high first. Predictor and step index reset to zero for each packet. The decoder uses the 89-entry IMA step table with index adjustments -1,-1,-1,-1,2,4,6,8, but the increment is calculated as `step * magnitude / 4 + step / 8` with separate integer truncation. This differs from bit-by-bit IMA implementations; generic ADPCM containers are not assumed equivalent. Predictor and index are clamped after each sample. Output has four times as many bytes as the input.

The Go implementation produced identical PCM bytes to the original decoder for 77 reference packets. This verifies that fixture, not universal compatibility with other camera families. Invalid audio packets increment `rejected_audio_frames`; accepted packets update `audio_frames` and `audio_samples`. `non_video_frames` includes those audio envelopes. Without audio enabled, non-video frames are skipped as before.
