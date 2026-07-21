# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add `auth whoami` to verify the selected KubeSphere credential and display
  the server-side User and global role.
- Add kubectl-compatible `ksctl-*` executable plugins to both `ksctl` and
  `kubectl ks`, including longest-name dispatch and `plugin list` diagnostics.

## [0.1.0] - 2026-07-17

### Added

- Standalone `ksctl` and `kubectl ks` entrypoints.
- KubeSphere authentication, Fleet/User-scoped token caching, and context management.
- Kubernetes-compatible `get` and `describe` commands with cross-cluster discovery.
- Linux and macOS release archives for amd64 and arm64.

### Security

- Hide passwords during interactive login.
- Contain token cache paths and prevent sanitized-name collisions.
- Atomically persist credential files with mode `0600`.
- Redact stored credentials from `config view` unless `--raw` is explicit.

### Fixed

- Display `kubectl ks` consistently in plugin help.
- Remove the non-functional `--workspace` flag.
- Keep Go module metadata tidy and reproducible.

[0.1.0]: https://github.com/frezes/ksctl/releases/tag/v0.1.0
