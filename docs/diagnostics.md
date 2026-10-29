# Capture diagnostics

`doctor` checks local runtime metadata. `status` checks the viewer that is already running; it does not initialize the SDK, open a camera session, reset hardware or alter the active stream.

```sh
bin/hdcam status --url http://127.0.0.1:8080 \
  --username YOUR_VIEWER_NAME --token-file /private/path/viewer.token --timeout 5s
```

Use the actual viewer origin/port. HTTP is restricted to literal loopback addresses; remote status requires HTTPS. Credentials come from a private regular file with mode0600, never from the URL. Redirects are refused. Responses are limited to64KiB; malformed or slow responses return fixed errors without reflecting their bodies or credentials.

Exit codes: 0 means a valid online response, 1 means offline or a request/authentication/response error, and 2 means invalid CLI arguments. Offline responses still print the JSON report; a failed request does not print a misleading health report.

## Health fields

The protected `/health` endpoint retains `online`, `frames`, and `last_frame`. Updated servers also expose:

| Field | Meaning |
|---|---|
| `capture_phase` | `idle`, `connecting`, `streaming`, `retrying`, or `stalled` |
| `capture_attempts` | Attempts begun in this server process |
| `capture_sessions` | Attempts that reached their first valid video frame |
| `capture_failures` | Failed attempts; normal timed session renewal is excluded |
| `source_fps` | Estimated received JPEG rate from recent arrival timestamps |
| `last_frame_age_ms` | Age of the most recent valid video frame; -1 means unknown |
| `last_failure_code`, `last_failure_at` | Most recent failure, retained after recovery |
| `retry_at` | Scheduled retry time, when applicable |

Counters reset when the server process restarts. Frame rate uses up to128 timestamps within the most recent five seconds and measures intervals between them. It is zero before two usable samples, outside that interval, or while a new attempt is connecting/retrying. This is neither the camera's nominal sensor FPS nor the browser's displayed FPS. Last-frame age helps distinguish a recent rate estimate from an input pause. A streaming phase becomes `stalled` after15seconds without a new valid frame.

The legacy `online` field means a valid frame arrived within15seconds. It can remain true briefly during reconnection because the last image is still recent. Use phase and age together. A historical failure is not evidence that the current connection remains broken.

## Failure categories

| Code | Interpretation |
|---|---|
| `startup_failed` | Native initialization, library setup or session startup failed |
| `stream_failed` | Native command/write/read stage failed |
| `runtime_missing` | Configured executable was unavailable or an executable lookup was rejected |
| `invalid_media` | A malformed or unexpected incomplete media envelope caused failure |
| `no_video` | An otherwise completed attempt supplied no valid JPEGs |
| `stalled` | The frame watchdog canceled the child while supervision remained active |
| `timeout` | An attempt deadline expired |
| `capture_error` | Other capture failure |

Categories deliberately omit raw SDK diagnostics, initialization strings, camera IDs and passwords. A native startup exit takes precedence over an accompanying empty-output error; otherwise a failed connection could be mislabeled merely as `no_video`. Parent shutdown is excluded from retry-failure counters. Normal one-hour session renewal retries without adding a failure.

## Older servers

The CLI accepts the original three-field response. It sets `diagnostics_available:false` and `last_frame_age_ms:-1` when the extra fields are absent. Default zero diagnostic values in that report are not measurements from the older server. New diagnostic fields become available only when the server is next updated; installing or running this status client alone does not modify a running viewer.
