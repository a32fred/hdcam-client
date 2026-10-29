# Viewing and recording

Build `bin/hdcam`, then create a password file outside this checkout:

```sh
bin/hdcam token --output /private/path/viewer.token
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token
```

Open `http://127.0.0.1:8080/`, use username `hdcam` and the private token file contents as the password. Supply `--username YOUR_VIEWER_NAME` to choose a different username. The private password file accepts a nonempty single-line password; the `token` command generates a random password without printing it. Files must have mode0600. Camera credentials and viewer credentials are separate. Changing viewer credentials does not change the camera's own credentials. Restart the service after changing its password file. Browser Basic authentication can remain cached until cleared; a private tab can be useful when switching accounts.

The browser primarily receives JPEGs through `/live`, a same-origin WebSocket. It acknowledges decoded frames; the server permits at most four unacknowledged frames and selects the latest available image instead of accumulating a capture queue. The browser decodes one frame at a time and retains at most one newer pending frame. Sessions use origin verification and the same HTTP authentication as the page. This removes the per-image HTTP round trip that can limit snapshot playback on slower links.

If WebSocket is unavailable, the browser falls back to one latest snapshot request at a time. ETags suppress unchanged images. WebSocket retries resume automatically; obsolete requests/decodes cannot replace a newer session's image. Hidden tabs release the video connection and reconnect on visibility or Back/Forward cache restoration. Frame rate still depends on source delivery, network and decoding. `/stream` remains available for multipart MJPEG clients. The page reports stale/unavailable video.

Routes `/`, `/viewer.js`, `/live`, `/stream`, `/snapshot`, `/audio` and `/health` all require authentication. Snapshot and health return 503 if no complete video frame arrived in15seconds. A retained image can be frozen; the page labels that state instead of claiming it is live. `/live` rejects invalid/oversized acknowledgements, cross-origin upgrades and clients that leave frames unacknowledged for15seconds.

## Optional microphone audio

```sh
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token --audio
```

Microphone capture is off unless `--audio` is supplied. Click **Ouvir áudio** to start browser playback; automatic sound is not attempted. Audio uses PCM16 little endian, mono, 8 kHz, decoded from independently reset type-11 ADPCM packets. The page shows signal peak in dBFS and allows 1–20× amplification (default 4×). Amplification can increase noise or clip; received nonzero samples do not prove that a microphone is acoustically working. Browser/OS mute and output volume also affect audibility.

Audio and video are independently timed; lip synchronization and fixed end-to-end latency are not guaranteed. Each audio viewer has an eight-packet bounded queue; bursts drop oldest queued packets rather than disconnecting or accumulating delayed sound. Browser scheduling also bounds pending sound. `/audio` is a raw PCM stream for this viewer, not a WAV download.

One SDK session is shared by the process. There are 16 total simultaneous media response slots (snapshot, MJPEG and audio combined). The camera is read on independent video/audio channels; no second camera connection is needed for sound. Writes have five-second deadlines; idle streaming deadlines are cleared so an outage does not permanently end a healthy HTTP/2 stream. Slow video consumers skip older frames.

The supervisor retries exited/frozen captures with 1–30 second exponential backoff. An attempt with no initial video for 45 seconds or no subsequent video for 15 seconds is canceled. Sessions renew after one hour. Start only one capture process for a camera; additional direct clients can interfere.

## Recording and export

```sh
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token \
  --record-dir /private/path/new-recording --segment-frames 300 --keep-segments 12
bin/hdcam capture --audio --config /private/path/camera.json --duration 10s --output /private/path/new-capture
bin/hdcam extract --audio --output /private/path/new-extraction SDK_STREAM_FILE
```

The legacy `--record-dir` service recorder records video only, even when live audio is enabled. It keeps the last 12 completed segments plus the current partial segment by default. This bounds frame count rather than duration or disk bytes. The directory must be new. Recording failure stops the service; normal shutdown commits the last segment, while abrupt termination can preserve `.part` files. For resumable video/audio archives, byte retention and protected browser playback, use the new [recordings](recordings.md) commands or `serve --archive-dir`.

One-shot capture/extraction with `--audio` writes a separate PCM WAV alongside MJPEG. No received audio makes the requested audio export fail. Actual read/process/write failures retain partial files. An incomplete final input envelope is intentionally omitted and counted as trailing bytes; completed valid video/audio can still be exported with `complete:true`, meaning the export completed rather than every source frame was delivered. Native channel assembly omits incomplete final envelopes before Go receives them, so those native trailing bytes are not counted by the Go parser.

## Phone outside the home network

The notebook must stay powered on, connected to the camera LAN and running this service. The default listener is loopback-only. An existing authenticated SSH tunnel or private reverse proxy can forward it without opening camera ports. The client does not configure a router or install a remote transport.

For an explicitly chosen temporary test, an already installed `cloudflared` can expose the loopback listener through HTTPS:

```sh
cloudflared tunnel --no-autoupdate --url http://127.0.0.1:8080
```

This uses a third-party relay and creates a public HTTPS address. Viewer authentication remains mandatory; never put credentials into a URL. Quick Tunnels have no uptime guarantee and the address changes when restarted. Stopping `cloudflared` removes access through that tunnel. This is a test transport, not an installed persistent hosting service. See [Cloudflare Quick Tunnels](https://developers.cloudflare.com/tunnel/get-started/quick-tunnels/).

For direct private-network listening, supply a certificate/key trusted by the phone:

```sh
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token \
  --listen 192.168.50.10:8443 --tls-cert /private/path/server.crt --tls-key /private/path/server.key
```

Non-loopback listeners, including wildcard addresses, require TLS and the viewer password. Use an actually reachable notebook address and matching trusted certificate. Validation from the notebook through a tunnel is different from a successful 4G/5G test on a phone.

See [recovery.md](recovery.md) for reset recovery limits.
