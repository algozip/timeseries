# Contributing

Open an issue for bug reports or API proposals. Include your Go version, OS,
and a small reproduction for bugs. Discuss substantial API or storage-format
changes before implementing them.

Contributions are submitted under the project's CC BY-NC 4.0 license. Only
submit material you have the right to license under those terms.

The module requires Go 1.23 or later and has no external dependencies. Keep
changes focused, preserve compatibility with supported `.zts` formats, and
document public API changes. Performance changes should include before/after
benchmarks using the same toolchain and machine.

Run these checks from the repository root before opening a pull request:

```sh
gofmt -w .
go mod tidy
go build ./...
go vet ./...
go test -race ./...
```

For iteration and moving-average changes, also run:

```sh
go test -run '^$' -bench . -benchmem -count=5 .
```

Use `GOWORK=off` when an enclosing Go workspace would otherwise affect checks.
CI checks the minimum Go version, the latest two stable releases, and current
Go on Linux, macOS, and Windows.

Releases use semantic version tags (`vMAJOR.MINOR.PATCH`). Consumers install
the module directly with `go get`; the library does not need compiled release
binaries. Breaking API changes may occur while the module is at major version
zero and must be documented.
