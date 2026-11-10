# Contributing

Use Go 1.26 and the standard library unless a dependency solves a demonstrated requirement. Keep offline tests independent of the camera, SDK and internet. Run gofmt, `go vet ./...`, `go test ./...` and `go test -race ./...` before proposing changes.

For browser playback logic, run `node --test internal/live/viewer_test.js internal/archive/recordings_test.js` with Node22 or newer. These tests use built-in Node modules and synthetic DOM/network clocks; no npm packages or real camera data are required. The application backend itself does not require Node.

Use synthetic media in tests. Do not commit SDK/runtime binaries, firmware, APKs, camera identifiers, credentials, actual camera imagery or packet captures. Capture support claims need hardware evidence and must identify limitations without exposing device-specific values. Building for another operating system is not evidence of working capture on that system.

The module path is `github.com/a32fred/hdcam-client` and the project's own code is MIT licensed (see LICENSE). Do not apply that license to externally supplied runtime files.

Keep pull requests narrow. A change that widens scope, adds a dependency without a demonstrated requirement, or weakens a documented limitation will be declined.
