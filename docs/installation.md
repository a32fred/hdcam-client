# Installation

The parser, extractor and tests use Go1.26 and the modules pinned in `go.mod`/`go.sum`. A first build needs access to those modules or an existing module cache; offline extraction does not require a camera or SDK. Native capture currently targets Linux x86_64 with separately supplied QEMU, ARM64 Bionic runtime and the matching vendor SDK. Proprietary dependencies are not installed or distributed by this repository. See [runtime.md](runtime.md) before configuring capture.

```sh
go test ./...
go build -o bin/hdcam ./cmd/hdcam
install -Dm755 bin/hdcam "$HOME/.local/bin/hdcam"
```

Keep the private profile and HTTP password outside this checkout, for example under `~/.config/hdcam/` with directory permissions 0700 and file permissions 0600. Configure absolute runtime paths in the profile. Building a binary on another OS does not establish capture support there.

For native capture, install your built bridge and guard outside the checkout as well:

```sh
install -Dm755 bin/capture-arm64 "$HOME/.local/lib/hdcam/capture-arm64"
install -Dm644 bin/local_only-arm64.so "$HOME/.local/lib/hdcam/local_only-arm64.so"
```

Set `bridge` and `network_guard` in the private profile to those absolute paths. SDK, QEMU and Android runtime paths still point at separately supplied dependencies. This prevents cleaning build output in the checkout from removing the installed bridge.

For an optional Linux user service, adapt `examples/hdcam.service`, place it at `~/.config/systemd/user/hdcam.service`, then run:

```sh
systemctl --user daemon-reload
systemctl --user enable --now hdcam.service
journalctl --user -u hdcam.service
```

The example uses the loopback-only viewer. Add `--username YOUR_VIEWER_NAME` to `ExecStart` for a custom viewer account and `--audio` to enable the microphone. It restarts unexpected exits, retries without a start-rate cutoff, and stops the capture subprocess with the service. It does not start recording or configure network isolation.

To start the user manager at boot and keep it alive after logout, explicitly enable lingering for your account:

```sh
loginctl enable-linger "$USER"
loginctl show-user "$USER" -p Linger
```

That setting does not prevent notebook suspend or power loss. The camera is unavailable while its host sleeps; capture watchdogs reconnect after execution/network access resume, but an actual suspend/reboot cycle needs a host-specific test. Starting a second CLI capture against the same camera can interfere with the service. Stop it first with `systemctl --user stop hdcam.service` when testing another capture.

An optional `examples/hdcam-tunnel.service` starts an already installed `cloudflared`. Adapt its loopback port to the viewer, then install and enable it like the viewer unit. It uses a third-party Quick Tunnel; the public URL changes whenever a new tunnel is created, including after reboot. Retrieve the current URL from `journalctl --user -u hdcam-tunnel.service`. Enabling a unit alone does not start it now; `enable --now` does. Keep an existing tunnel running if preserving its current URL matters, and only start the new tunnel unit after stopping that existing connector. A fixed remote address requires a different persistent tunnel configuration.
