# Third-party components

No third-party binaries are included in this source tree. The project's own source is MIT licensed (see LICENSE); that grant does not extend to any component listed below.

The MIT license covers only source code authored for this project. It does not extend to any manufacturer SDK, runtime binary, firmware image or decompiled material, none of which are distributed here.

| Component | Use | Distribution status |
|---|---|---|
| Go toolchain / standard library | Build and Go-only processing | Installed separately; follow upstream conditions |
| github.com/coder/websocket v1.8.15 | Authenticated browser video WebSocket transport | ISC; pinned in go.mod/go.sum; notice in third_party/coder-websocket/LICENSE.txt |
| QEMU | Execute the external ARM64 runtime | Not bundled; review upstream license and the particular distribution |
| Android/Bionic linker and libraries | External capture runtime | Not bundled; verify provenance and applicable notices per file |
| Manufacturer SDK | Camera transport | Proprietary external dependency; distribution permission not established |
| FFmpeg | Optional MP4 preview rendering | Installed separately; review the build's own conditions |

The C bridge exposes a narrow API boundary; it does not replace the SDK or authorize redistribution. No SDK initialization constant, proprietary implementation, APK, firmware image or decompiled source is supplied here.

Source provenance has been audited for the published tree: no decompiled material, APK, firmware image, packet capture or real device identifier is included. Do not apply the project's license to externally supplied runtime files. If you add a dependency, add its notice here and pin it in `go.mod`.
