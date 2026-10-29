# Experimental capture runtime

Offline extraction is Go-only. Live capture currently requires an external Android ARM64 SDK/runtime and QEMU on Linux x86_64. These dependencies are not included or downloaded automatically. An open client does not make its proprietary backend an independent open implementation of the network protocol.

Supply compatible runtime files and SDK initialization from a source you are authorized to use. Their provenance and distribution conditions require separate review; providing them locally is not a blanket licensing conclusion. No APK, firmware, SDK binary, phone runtime, camera credentials or initialization value belongs in this repository.

## Build the bridge

The freestanding bridge targets the Android ARM64 ABI. A compatible Zig compiler and external ARM64 Bionic `libc.so`/`libdl.so` are required:

```sh
ANDROID_LIBDIR=/path/to/external/arm64/bionic bash scripts/build-native.sh
go build -o bin/hdcam ./cmd/hdcam
```

Set `ZIG` to an explicit compiler executable if needed. The script produces `bin/capture-arm64` and `bin/local_only-arm64.so`; generated binaries are ignored by Git. The bridge source declares only the ABI functions it uses and is not a portable native Linux application. The Android dynamic linker and shared dependencies must exist in the external sysroot.

## Private configuration

Copy `examples/config.example.json` to `config.local.json`, set permissions to `0600`, and replace every placeholder. The example subnet is illustrative and must match the intended LAN. Paths are resolved relative to the configuration file. UID, camera username/password and SDK initialization are required; there is no device-specific or universal-password fallback.

```sh
cp examples/config.example.json config.local.json
chmod 600 config.local.json
# Edit the local file before running the following commands.
bin/hdcam doctor --config config.local.json
bin/hdcam capture --config config.local.json --duration 10s --video-mode 1 --output output/first-capture
```

`doctor --config` checks file privacy, bounded configuration, private IPv4 prefix, basic ELF architecture markers and paths. It does not load the SDK, authenticate or test connectivity. A valid configured runtime can use QEMU outside PATH; a missing optional PATH tool is still listed in the report. Basic ELF checks do not authenticate a supplied executable.

`--duration` accepts whole seconds from `1s` to `1h`. Modes 1 and 2 are the demonstrated 640×480 and 320×240 selectors; they are not a guarantee for arbitrary firmware. The output directory must be new. Use one session per camera. Unknown JSON fields are rejected, and the private configuration is limited to 64 KiB.

## Process boundary and cancellation

Go launches QEMU as an external executable without a shell. SDK initialization, UID and command JSON are sent through stdin using a bounded length-prefixed message; they are not placed in process arguments or environment variables. Native stdout carries raw media only. Native stderr is discarded in the capture path to avoid relaying undocumented SDK output.

The request starts with five little-endian 32-bit words: magic `0x31434448`, initialization length, UID length, command length and duration in seconds, followed by those three strings. Bounds are checked on both sides. The bridge verifies required SDK symbols, preserves partial reads with timeout code `-3`, uses a monotonic deadline and closes the session on ordinary completion/errors.

The parent provides a bounded context and propagates interruption to process cancellation. The child is killed and waited on when consumption fails or the context is canceled. Abrupt termination cannot guarantee a graceful vendor-library shutdown. Incomplete media remains a `.part` result for one-shot capture. The separate `serve` command adds frame watchdogs and reconnect supervision; see [viewing.md](viewing.md).

## Network filter scope

The preload filter checks imported `connect`, `sendto`, `sendmsg` and `sendmmsg` destinations, and rejects non-numeric DNS lookups through `getaddrinfo`/`gethostbyname`. It allows the explicitly configured RFC1918 IPv4 subnet, IPv4 broadcast and loopback. IPv6 is not supported. Invalid/missing subnet configuration does not allow arbitrary IP destinations.

This is a compatibility guard for the known SDK call paths, not a sandbox for malicious libraries or kernel-level firewall enforcement. Direct syscalls or a reachable local proxy can bypass that model. It does not change the camera firmware's own internet connections. A strict network security boundary requires separate OS/router policy.

## Current limits

- Only Linux x86_64 capture has been validated.
- Runtime ABI and SDK behavior depend on externally supplied files; public tests use synthetic helpers, not proprietary dependencies.
- Capture exports video. Browser viewing and reconnect supervision are available through `serve`; remote access needs an existing transport and appropriate TLS configuration. Audio decoding and synchronization remain unimplemented.
- Counter discontinuities are measured, not automatically repaired or attributed to a specific cause.
