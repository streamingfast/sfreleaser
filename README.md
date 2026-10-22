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

`sfreleaser release` runs with `--non-interactive` automatically when the `CI` environment variable is true (set by GitHub Actions and most CI providers): the `<version>` argument is then required and every question is answered "no", leaving the release in draft mode. Add `--yes` to answer "yes" to every question instead, which deletes an existing draft for the same version, pulls a stale checkout and publishes the release right away (a published release is never deleted):

```bash
sfreleaser release v1.2.3 --non-interactive
```

The `build/` directory `sfreleaser` writes to must be ignored by Git (in `.gitignore` or `.git/info/exclude`), otherwise the dirty check refuses to release.

### Development Version

The `sfreleaser` binary uses a build injected value for the `version` which is later used to compare against `sfreleaser-min-version` check in the config file.

If you build locally, either you the `devel/sfreleaser` script which inject it or `go install -ldflags "-X main.version=0.7.1" ./cmd/sfreleaser` where `0.7.1` should be set to the version you want to "simulate".
