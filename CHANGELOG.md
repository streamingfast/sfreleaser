# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.6.0

* Added `sfreleaser build` to build artifacts, `sfreleaser build --help` for all the juicy details of the new command.

* Bumped to `Golang` `1.20.5`, this will pull `goreleaser/goreleaser-cross:v1.20.5` so expect some delays before your build starts.`

  > **Note** `docker pull goreleaser/goreleaser-cross:v1.20.5` to "boostrap" this step.

* The platform `linux/arm64` is now built by default.

* When version is prompted in release, default value is now extracted from release notes' header.

* Speed up build by mounting local `go env GOCACHE` into the Docker container that build artifacts (only if language == `golang`).

## v0.5.5

* Validate that received `<version>` argument in `sfreleaser release <version>` actually follows our convention.

## v0.5.4

* Added a way to disable usage of PTY to call commands (define environment variable `SFRELEASER_DISABLE_PTY=true`).

## v0.5.3

### Fixed

* Fixed an issue when the github token has some leading or trailing spaces, like a new line.

## v0.5.2

### Changed

* Improved `sfreleaser release` to print some troubleshooting idea using `sfreleaser doctor`.

## v0.5.1

### Added

* Added support for `brew-tap-repo` to set the Brew tap repository where to push the binary (config at `release.brew-tap-repo`).

## v0.5.0

### Added

* Removed the need to have `.goreleaser.yaml` file in the repository (file is now generated on the fly).

* Added support for disabling Brew tap release (enabled by default).

* Added support for specifying `owner` (defaults to `streamingfast`).

* Added support for specifying `license` (defaults to `Apache-2.0`).

## v0.4.2

* Added support for resolving files relative to `.sfreleaser` location.

* Added support for specifying a non-default changelog file.

## v0.4.1

* Added checks that `docker` CLI exists and also that `docker info` works properly.

## v0.4.0

* Added full `CGO` support when building Go application/library, `.goreleaser.yaml` file now has `C_INCLUDE_PATH` and `LIBRARY_PATH` sets correctly so it's possible to build Go that depends on C libraries.

* Added config/flag value `goreleaser-docker-image` so it's possible to override `goreleaser` Docker image used.

## v0.3.0

* Added support for releasing Rust library project.

  This newly added support will publish Rust crates of a library. The crates to publish must be
  specified in the configuration file via the path `releaser.rust-crates` where the value is a list
  of crates name:

  ```yaml
  global:
    ...
  release:
    rust-crates:
    - crate1
    - crate2
  ```

  Order is important as it will be respected when doing the commands. A GitHub release will be produced just
  like for Golang.

  The crates publishing happen only if release is published right now. Otherwise, if the command complete
  and release is not published yet, commands to publish the crates manually is printed.
