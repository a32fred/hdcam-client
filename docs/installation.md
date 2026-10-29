# Installation

The offline parser, extractor and tests use only Go 1.26. Native capture and viewing currently target Linux x86_64 with separately supplied QEMU, ARM64 Bionic runtime and the matching vendor SDK. These dependencies are not installed or distributed by this repository. See [runtime.md](runtime.md) before configuring capture.

```sh
go test ./...
go build -o bin/hdcam ./cmd/hdcam
install -Dm755 bin/hdcam "$HOME/.local/bin/hdcam"
```

Keep the private profile and HTTP password outside this checkout, for example under `~/.config/hdcam/` with directory permissions 0700 and file permissions 0600. Configure absolute runtime paths in the profile. Building a binary on another OS does not establish capture support there.

For an optional Linux user service, adapt `examples/hdcam.service`, place it at `~/.config/systemd/user/hdcam.service`, then run:

```sh
systemctl --user daemon-reload
systemctl --user enable --now hdcam.service
journalctl --user -u hdcam.service
```

The example uses the loopback-only viewer. It does not start recording, enable user lingering, prevent suspend, configure network isolation, or install a remote-access transport. Starting a second CLI capture against the same camera can interfere with the service. Stop it first with `systemctl --user stop hdcam.service` when testing another capture.
