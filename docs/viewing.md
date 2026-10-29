# Viewing and recording

Build `bin/hdcam`, then create a password file outside this checkout:

```sh
bin/hdcam token --output /private/path/viewer.token
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token
```

The password generator creates a new file with mode 0600 and never prints the password. Read it locally when you need to log in. Open `http://127.0.0.1:8080/`, use username `hdcam`, and use the file contents as the password. The camera profile and HTTP password are separate secrets. Browser Basic authentication stays valid until browser credentials are cleared or the service password is changed and the process restarted.

Routes `/`, `/stream`, `/snapshot` and `/health` all require authentication. The snapshot and health endpoint return 503 if no complete video frame arrived in 15 seconds. MJPEG viewers may retain their last displayed image after a disconnection; consult health to distinguish a frozen image. The stream waits through camera outages and resumes when frames return; reload if the browser or network itself closed the connection. There is no audio in the browser stream.

There is one camera connection per process and up to 16 concurrent stream viewers. Slow viewers skip older frames instead of building a queue; each write has a five-second deadline. The supervisor reconnects after an attempt exits, using 1 to 30 second exponential backoff. A session with no initial frame for 45 seconds, or no subsequent complete frame for 15 seconds, is canceled and retried. Long-running sessions are renewed after one hour. Do not run a second capture process for the same camera alongside `serve`.

Optional recording:

```sh
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token \
  --record-dir /private/path/new-recording --segment-frames 300 --keep-segments 12
```

The recording directory must be new. The recorder keeps the last 12 completed segments by default, plus the current partial segment; this is a frame-count limit rather than a duration or disk-byte quota. Each complete segment contains concatenated JPEG images. Frame rate varies and camera timestamps are not yet interpreted. Recording failure stops the service rather than silently disabling recording. Normal shutdown closes the final segment; abrupt process termination can leave `.part` files. Segments contain validated complete JPEGs, but shutdown does not imply every camera frame was delivered. Keep recordings on a private filesystem with sufficient space.

## Phone outside the home network

The notebook must stay powered on, connected to the camera LAN and running the capture service. A browser on another network cannot reach the default loopback listener directly.

The default service can be reached through an existing authenticated SSH tunnel or private network reverse proxy. For an SSH client supporting local forwarding, forward a local port to `127.0.0.1:8080` on the home notebook, then browse the local forwarded address. SSH reachability must already exist; the camera client does not create it or configure routers. No camera ports need to be forwarded.

For direct listening on a private network address, provide a TLS certificate/key trusted by the phone:

```sh
bin/hdcam serve --config /private/path/camera.json --token-file /private/path/viewer.token \
  --listen 192.168.50.10:8443 --tls-cert /private/path/server.crt --tls-key /private/path/server.key
```

Use the notebook's actual reachable address and a matching trusted certificate. Non-loopback listeners require TLS, including wildcard listeners. The HTTP password remains required with TLS. No cloud account, relay, DNS setup, VPN installation, router mapping or automatic remote discovery is included. This supports an existing secure remote transport; it does not prove cellular-to-home connectivity without a separate outside-LAN test.

## After a reset

See [recovery.md](recovery.md). Session retries cannot restore Wi-Fi settings erased by a factory reset.
