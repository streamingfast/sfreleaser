# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.13.0

- Bumped to `Golang` `1.25`, this will pull `goreleaser/goreleaser-cross:v1.25` so expect some delays before your build starts.

## v0.12.4

- Improved git remote resolution: automatically uses matching remote based on owner when available, falls back to `--git-remote` flag or default `origin` when none or multiple remotes match. Uses local git config for fast lookup without server queries.
- Added `--repository` flag as alternative to `--owner`/`--project` pair; accepts formats like `owner/project`, `github.com/owner/project`, or `https://github.com/owner/project`.
- Fixed `gh` commands to always pass `--repo <owner>/<project>` flag to ensure correct repository context.

## v0.12.3

- Renamed `sfreleaser install` to `sfreleaser init`; `install` command still works with deprecation warning.
- Added support for Rust application variant during `init` with `no-binaries: true` automatically set.
- Fixed deprecated `archives.format` usage by switching to `archives.formats` (list format).

## v0.12.2

- Added `release.no-binaries` config option to skip binary builds when releasing application variants (useful when binaries are built through other means; cannot be used with library variant).

## v0.12.1

- Improved `sfreleaser changelog extract-section` to read for a github repository file directly.

- Improved `sfreleaser changelog extract-section` to accept `--github-output=changelog:$GITHUB_OUTPUT`

## v0.12.0

- Added `sfreleaser changelog extract-section` to be usable in GitHub CI for easy external release management.

- Removed deprecation warnings from Goreleaser for `snapshot` and `archives`.

- Some fixes for Substreams publishing.

## v0.11.1

- Fixed `--teamSlug` flag to use new version `--team-slug` when doing `substreams registry publish`.

## v0.11.0

- Initial release for supporting Substreams variant under language Rust.

  Requires latest Substreams CLI for correctly publish packages to the registry.

## v0.10.1

- Fixed `sfreleaser build` not using correct latest `goreleaser/goreleaser-cross:v1.24` image.

## v0.10.0

- Bumped to `Golang` `1.24`, this will pull `goreleaser/goreleaser-cross:v1.24` so expect some delays before your build starts.

## v0.9.1

- Fixed `--allow-dirty` not working with latest `goreleaser` version that we use by default.

- Fixed `brew-tap-repo` to properly handle the format `<owner>/<name>` as well as just `<name>` in which case owner is the global value.

## v0.9.0

- Bumped to `Golang` `1.23`, this will pull `goreleaser/goreleaser-cross:v1.23` so expect some delays before your build starts.

## v0.8.0

- Ensure `sfreleaser` works with Goreleaser 2.x

  > [!IMPORTANT]
  > You will need to use an up to date version of `goreleaser/goreleaser-cross:v1.22` or later for `sfreleaser` to work properly. If you have an error of the form `⨯ release failed after 0s error=only configurations files on  version: 1  are supported, yours is  version: 2 , please update your configuration`, update your image to latest version using `docker pull --platform linux/arm64 goreleaser/goreleaser-cross:v1.22` and ensure you `.sfreleaser` does use it properly.

- Fixed wrong error when a project was never release.

- Fixed CHANGELOG release version extraction to accept dots too.

- Fixed when LICENSE and README are not present or spelled a bit differently.

- Added support to override the Git remote used for commands with `sfreleaser --git-remote=sf ...`.

- Bumped to `Golang` `1.22`, this will pull `goreleaser/goreleaser-cross:v1.22` so expect some delays before your build starts.

## v0.7.2

- Enforce `--platform <platform>` when calling `docker run` to ensure the fastest image for the current's user machine is used.

- Now printing exact image used when performing the release.

## v0.7.1

- Bumped to `Golang` `1.21`, this will pull `goreleaser/goreleaser-cross:v1.21` so expect some delays before your build starts.

## v0.7.0

### Deprecation

The `release.upload-substreams-spkg` has been deprecated in favor of using `pre-build-hooks` and `upload-extra-assets` instead, the replacement code is converting `release.upload-substreams-spkg` using this new system internally.

Change

```yaml
release:
  upload-substreams-spkg: substreams.yaml
```

By

```yaml
release:
  pre-build-hooks:
    [
      'substreams pack -o "{{ .buildDir }}/{{ .global.Project }}-{{ .release.Version }}.spkg" substreams.yaml"',
    ]
  upload-extra-assets:
    ["{{ .buildDir }}/{{ .global.Project }}-{{ .release.Version }}.spkg"]
```

### Added

- If changelog list `Next` as the header, default prompted version is the next patch version.

- Extracted version from CHANGELOG is now much more selective.

- Prevent release if changelog extracted version and latest tag version are the same.

- Added `global.sfreleaser-min-version` configuration value to force users to upgrade to a new version of `sfreleaser`.

## v0.6.0

- Added `sfreleaser build` to build artifacts, `sfreleaser build --help` for all the juicy details of the new command.

- Bumped to `Golang` `1.20.5`, this will pull `goreleaser/goreleaser-cross:v1.20.5` so expect some delays before your build starts.`

  > **Note** `docker pull goreleaser/goreleaser-cross:v1.20.5` to "boostrap" this step.

- The platform `linux/arm64` is now built by default.

- When version is prompted in release, default value is now extracted from release notes' header.

- Speed up build by mounting local `go env GOCACHE` into the Docker container that build artifacts (only if language == `golang`).

## v0.5.5

- Validate that received `<version>` argument in `sfreleaser release <version>` actually follows our convention.

## v0.5.4

- Added a way to disable usage of PTY to call commands (define environment variable `SFRELEASER_DISABLE_PTY=true`).

## v0.5.3

### Fixed

- Fixed an issue when the github token has some leading or trailing spaces, like a new line.

## v0.5.2

### Changed

- Improved `sfreleaser release` to print some troubleshooting idea using `sfreleaser doctor`.

## v0.5.1

### Added

- Added support for `brew-tap-repo` to set the Brew tap repository where to push the binary (config at `release.brew-tap-repo`).

## v0.5.0

### Added

- Removed the need to have `.goreleaser.yaml` file in the repository (file is now generated on the fly).

- Added support for disabling Brew tap release (enabled by default).

- Added support for specifying `owner` (defaults to `streamingfast`).

- Added support for specifying `license` (defaults to `Apache-2.0`).

## v0.4.2

- Added support for resolving files relative to `.sfreleaser` location.

- Added support for specifying a non-default changelog file.

## v0.4.1

- Added checks that `docker` CLI exists and also that `docker info` works properly.

## v0.4.0

- Added full `CGO` support when building Go application/library, `.goreleaser.yaml` file now has `C_INCLUDE_PATH` and `LIBRARY_PATH` sets correctly so it's possible to build Go that depends on C libraries.

- Added config/flag value `goreleaser-docker-image` so it's possible to override `goreleaser` Docker image used.

## v0.3.0

- Added support for releasing Rust library project.

  This newly added support will publish Rust crates of a library. The crates to publish must be
  specified in the configuration file via the path `releaser.rust-crates` where the value is a list
  of crates name:

  ```yaml
  global: ...
  release:
    rust-crates:
      - crate1
      - crate2
  ```

  Order is important as it will be respected when doing the commands. A GitHub release will be produced just
  like for Golang.

  The crates publishing happen only if release is published right now. Otherwise, if the command complete
  and release is not published yet, commands to publish the crates manually is printed.
