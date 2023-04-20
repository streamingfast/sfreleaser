## StreamingFast `sfreleaser`

> **Note** This tool is meant for StreamingFast usage and is not a generic release tool. If you like it, feel free to use it but your are not our main target.

This is a tool we use internally at StreamingFast to automate release process in a standardize way. The tool can support different language and variant. Currently supported project types:

- Language: Golang, Variant: Application
- Language: Golang, Variant: Library

The `sfreleaser` usually simply wraps instructions for other tools, mainly:

- [goreleaser](https://goreleaser.com/)
- [gh](https://github.com/cli/cli#github-cli)
- [docker](https://docker.com)

The `sfreleaser release` usually builds the necessary artifacts, configures `goreleaser`, uploads extra artifacts if necessary and performs the release on GitHub in draft mode. You have then the possibility to review it and publish it.
