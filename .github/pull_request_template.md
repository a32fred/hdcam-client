## What this changes

<!-- One paragraph. Link the issue if there is one. -->

## Why

<!-- The problem this solves. -->

## How it was verified

<!-- Commands run and their result. -->

```
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

## Checklist

- [ ] `gofmt` clean, `go vet ./...` and `go test -race ./...` pass locally.
- [ ] Tests are offline: no camera, SDK, network or Python required.
- [ ] No SDK/runtime binary, firmware, APK, packet capture, camera identifier,
      credential or real camera imagery is included.
- [ ] Documentation under `docs/` updated if behaviour changed.
- [ ] Any new limitation is stated in the README rather than hidden.
