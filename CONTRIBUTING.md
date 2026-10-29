# Contributing

Use Go 1.26 and the standard library unless a dependency solves a demonstrated requirement. Keep offline tests independent of the camera, SDK and internet. Run gofmt, `go vet ./...`, `go test ./...` and `go test -race ./...` before proposing changes.

Use synthetic media in tests. Do not commit SDK/runtime binaries, firmware, APKs, camera identifiers, credentials, actual camera imagery or packet captures. Capture support claims need hardware evidence and must identify limitations without exposing device-specific values. Building for another operating system is not evidence of working capture on that system.

The permanent module path and license remain pending; resolve them before public distribution.
