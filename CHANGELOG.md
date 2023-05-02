# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
