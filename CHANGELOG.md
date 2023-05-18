# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.5.1

* Added support for `brew-tap-repo` to set the Brew tap repository where to push the binary (config at `release.brew-tap-repo`).

## v0.5.0

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
