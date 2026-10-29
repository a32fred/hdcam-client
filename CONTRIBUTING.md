# Contributing

Use Go 1.26 and the standard library unless a dependency solves a demonstrated requirement. Keep offline tests independent of the camera, SDK and internet. Run gofmt, `go vet ./...`, `go test ./...` and `go test -race ./...` before proposing changes.

For browser playback logic, run `node --test internal/archive/recordings_test.js` with Node22 or newer. These tests use built-in Node modules and a synthetic DOM/network clock; no npm packages or real camera data are required. The application backend itself does not require Node.

Use synthetic media in tests. Do not commit SDK/runtime binaries, firmware, APKs, camera identifiers, credentials, actual camera imagery or packet captures. Capture support claims need hardware evidence and must identify limitations without exposing device-specific values. Building for another operating system is not evidence of working capture on that system.

The permanent module path and license remain pending; resolve them before public distribution.
