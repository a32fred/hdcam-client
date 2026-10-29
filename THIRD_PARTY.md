# Third-party components

No third-party binaries are included in this source tree. The project's own source license has not been selected; this must be resolved before the first public release.

| Component | Use | Distribution status |
|---|---|---|
| Go toolchain / standard library | Build and Go-only processing | Installed separately; follow upstream conditions |
| QEMU | Execute the external ARM64 runtime | Not bundled; review upstream license and the particular distribution |
| Android/Bionic linker and libraries | External capture runtime | Not bundled; verify provenance and applicable notices per file |
| Manufacturer SDK | Camera transport | Proprietary external dependency; distribution permission not established |
| FFmpeg | Optional downstream rendering/playback | Not bundled or invoked by current extraction; review the build's own conditions |

The C bridge exposes a narrow API boundary; it does not replace the SDK or authorize redistribution. No SDK initialization constant, proprietary implementation, APK, firmware image or decompiled source is supplied here.

Before publication, audit source provenance, choose the license for the project's own code, and verify any dependency instructions. Do not apply the project's license to externally supplied runtime files.
