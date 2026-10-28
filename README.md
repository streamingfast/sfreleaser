## StreamingFast `sfreleaser`

> **Note** This tool is meant for StreamingFast usage and is not a generic release tool. If you like it, feel free to use it but your are not our main target.

This is a tool we use internally at StreamingFast to automate release process in a standardize way. The tool can support different language and variant. Currently supported project types:

- Language: `Golang`, Variant: `Application`
- Language: `Golang`, Variant: `Library`
- Language: `Rust`, Variant: `Library`

The `sfreleaser` usually simply wraps instructions for other tools, mainly:

- [goreleaser](https://goreleaser.com/)
- [gh](https://github.com/cli/cli#github-cli)
- [docker](https://docker.com)

The `sfreleaser release` usually builds the necessary artifacts, configures `goreleaser`, uploads extra artifacts if necessary and performs the release on GitHub in draft mode. You have then the possibility to review it and publish it.

### Running in CI

`sfreleaser` runs with the global `--non-interactive` flag automatically when the `CI` environment variable is true (set by GitHub Actions and most CI providers): the `<version>` argument is then required and every question is answered "no", leaving the release in draft mode. Add `--yes` to answer "yes" to every question instead, which deletes an existing draft for the same version, pulls a stale checkout and publishes the release right away (a published release is never deleted):

```bash
sfreleaser release v1.2.3 --non-interactive
```

The `build/` directory `sfreleaser` writes to must be ignored by Git (in `.gitignore` or `.git/info/exclude`), otherwise the dirty check refuses to release.

The GitHub Actions `GITHUB_TOKEN` can only write to its own repository, not to the Homebrew tap. Set `HOMEBREW_TAP_TOKEN` to a token that can write to the tap; it is then used only for the tap. Like `GITHUB_TOKEN`, it is written to `build/.env.release`.

### Native builder

`--builder=native` (config at `release.builder`, honored by `build` and `release`) builds the binaries of a Go application with `go build` directly, without Docker nor the `goreleaser-cross` image. Cgo code is cross-compiled with [zig](https://ziglang.org): the pinned zig version is used from `PATH` when it is the one installed, otherwise it is downloaded once to the sfreleaser cache directory (`--zig-path`, config at `release.zig-path`, points to another binary). The macOS SDK stubs darwin binaries link against are embedded in `sfreleaser`, and written to the same cache directory. With `cgo-disabled`, zig is not needed at all.

The binaries are written to `build/dist/<os>-<arch>/<binary>`. Linux binaries require glibc 2.17 or later, darwin ones macOS 11 or later. The build does not see the host's C flags, include and library paths, `go env -w` settings nor `go.work`, as with Goreleaser's container.

The embedded macOS SDK only covers what Go itself links: `libSystem`, `libresolv`, `libobjc`, `CoreFoundation` and `Security`, without their headers. A project whose cgo code includes Apple headers (`<CoreFoundation/CoreFoundation.h>`, ...) or links other frameworks (`IOKit`, `Foundation`, ...) for darwin must keep `--builder=goreleaser-docker`, whose image has the full SDK. Likewise, cgo dependencies found through `pkg-config` are not available. On `release`, they are packaged as Goreleaser did (`<project>_<os>_<arch>.tar.gz` holding the binary, README and LICENSE, plus `checksums.txt` unless `checksums-disabled`), attached to the draft release created with `gh`, and the Homebrew formula is rendered and committed to the tap (or opened as a pull request with `brew-pull-request`) unless `brew-disabled`.

To refresh the embedded macOS SDK stubs from a newer [osxcross](https://github.com/crazy-max/docker-osxcross) image, run `go generate -run macossdkgen` in `cmd/sfreleaser` (it needs Docker), or `go run ./cmd/sfreleaser/internal/macossdkgen -image <image> -out cmd/sfreleaser/macos_sdk` for another image.

### Development Version

The `sfreleaser` binary uses a build injected value for the `version` which is later used to compare against `sfreleaser-min-version` check in the config file.

If you build locally, either you the `devel/sfreleaser` script which inject it or `go install -ldflags "-X main.version=0.7.1" ./cmd/sfreleaser` where `0.7.1` should be set to the version you want to "simulate".
