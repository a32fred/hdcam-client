# Recordings with audio and playback

The Linux A/V archive stores validated JPEG frames, a separate PCM WAV, and a timeline based on when the computer received the data. It does not claim the camera's clock is understood or guarantee exact A/V synchronization. The live audio amplification setting does not alter the stored PCM samples. Archive storage/playback on other operating systems returns an unsupported-feature error; the feature does not prevent building the offline CLI there.

## Record from an already running viewer

This path opens HTTP video/audio streams from the existing viewer. It creates no additional SDK connection and requires no restart of that viewer or the camera.

```sh
install -d -m700 "$HOME/.local/share/hdcam"
bin/hdcam record --source http://127.0.0.1:8080 \
  --username YOUR_VIEWER_NAME --token-file /private/path/viewer.token \
  --archive-dir "$HOME/.local/share/hdcam/recordings" \
  --audio --duration 30s --segment-frames 300 --keep-segments 100 --max-bytes 536870912
```

Use the actual viewer port and its password file. Cleartext HTTP sources must use a literal loopback address; remote sources require HTTPS. Redirects are refused so viewer credentials are not forwarded to another destination. Credentials are read from a private file, not supplied as command arguments.

`--duration 0` records until interrupted. A source connection failure ends the command with a nonzero exit; any valid current segment can still be finalized. An optional `examples/hdcam-record.service` restarts the recorder if desired. Adapt its origin port, username and paths before installing it; create the private archive parent first. This example is separate from the camera viewer service.

The command's report counts received video frames and PCM samples. `audio_samples_dropped` counts PCM omitted before the first video frame or at a byte-budget boundary. A request for audio fails if no samples were received. These counters do not guarantee that every camera-produced frame arrived or remains retained.

## Browse without changing the live service

```sh
bin/hdcam recordings --archive-dir "$HOME/.local/share/hdcam/recordings" \
  --username YOUR_VIEWER_NAME --token-file /private/path/viewer.token \
  --listen 127.0.0.1:8081
```

Open `http://127.0.0.1:8081/recordings`. Use the configured viewer username/password. This standalone listener is restricted to literal loopback addresses; an explicitly chosen authenticated external transport can expose it separately if needed.

Select a completed segment, play/pause, or seek on the timeline. Frame requests are serialized, and late responses from an earlier selection/seek are discarded. Slow connections skip frames rather than stopping all video updates. WAV playback uses standard HTTP byte ranges. Audio can also be downloaded independently. Reload the page to refresh the segment list while recording continues. Retention can remove a segment while someone is viewing it; a new request for it then returns404.

## Record in the capture process on a future start

```sh
bin/hdcam serve --config /private/path/camera.json \
  --username YOUR_VIEWER_NAME --token-file /private/path/viewer.token --audio \
  --archive-dir "$HOME/.local/share/hdcam/recordings" \
  --segment-frames 300 --keep-segments 100 --archive-max-bytes 536870912
```

This attaches the same protected `/recordings` browser to the live viewer. It is an alternative to the separate recorder; do not open two writers on the same archive. `--record-dir` remains the legacy video-only format and cannot be combined with `--archive-dir`.

## Retention and interrupted writes

The archive root is private and marked with its format version. Existing marked archives can be reopened; unrelated nonempty directories are refused. A process lock prevents simultaneous writers. A segment becomes visible only after its media and index have been closed/synced and its directory has been atomically renamed. Actual I/O failures retain a partial directory and return an error.

Two limits apply: completed-segment count and managed logical file bytes. The byte budget counts completed segments, current media/index, and preserved partial/corrupt segment data. Oldest validated completed segments are removed when necessary; unrecognized files and symlinks are never retention targets. Preserved partials are not played or automatically deleted and can consume the available budget. Lower limits are applied when reopening an archive.

A byte-driven audio boundary finalizes the valid segment and can omit PCM until the next video frame starts a new segment; the drop counter reports that omission. An archive budget too small for a frame and metadata returns an error. Ordinary write failures do not publish a misleading completed segment.

The budget uses logical file lengths, not filesystem allocation blocks or a reservation of free device space. Marker/lock files and unrelated objects are excluded. Open playback files can temporarily retain space after their directory entries are removed. Other applications can still fill the disk; resulting write errors are surfaced. Keep recordings on a filesystem with suitable free space.

Only completed segments appear in the browser. `.part` directories remain available for manual inspection; preserving them is not automatic repair or proof that every last frame survived a power loss.
