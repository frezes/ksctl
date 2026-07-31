# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-07-31

### Added

- Add the `kube` command suite with kubectl-compatible Kubernetes reads,
  mutations, rollouts, debugging, streaming, and Cluster management through
  KubeSphere authentication and `--cluster` routing.

### Changed

- **Breaking:** Move `describe`, `logs`, and `top` from the root to
  `kube describe`, `kube logs`, and `kube top`; remove the former top-level
  paths.
- **Breaking:** Move `--request-timeout` from the ksctl root to the `kube`
  command group.
- **Breaking:** Change the Go module path from `github.com/kubesphere/ksctl`
  to `kubesphere.io/ksctl`.
- Reorganize reusable KubeSphere client, Extension, and secure-file behavior
  under focused public `pkg/` packages.

### Fixed

- Fix nested `kube` command help and expose connection flags consistently.
- Preserve upgraded transport configuration for `kube` requests.
- Stabilize request-timeout coverage.

### Documentation

- Refocus the English and Chinese CLI guides on ksctl workflows and simplify
  the Kubernetes architecture documentation.

## [0.2.0] - 2026-07-30

### Added

- Add host-scoped `extension` discovery, exact-version lifecycle management,
  configuration, multicluster placement, waiting, status, and diagnosis.
- Add `auth whoami` to verify the selected KubeSphere credential and display
  the server-side User and global role.
- Add kubectl-compatible `ksctl-*` executable plugins, including longest-name
  dispatch and `plugin list` diagnostics.
- Add `tenant get` commands for KSE Workspaces, Namespaces, and Clusters with
  Workspace and member-Cluster routing plus kubectl-style table output.
- Add kubeconfig generation for the selected KubeSphere Context and Cluster.
- Add kubectl-compatible `logs` and resource-metrics `top` commands.
- Add authenticated raw KubeSphere API requests through `ksctl api`.
- Add the release-only `unictl-ks` companion entrypoint.

### Changed

- Scope `--namespace` to Kubernetes resource commands while preserving
  kubeconfig Namespace defaults.

### Security

- Revoke cached Access Tokens through the KubeSphere logout endpoint on a
  best-effort basis before clearing local login state.
- Bind explicit endpoints to explicit tokens and harden configuration and TLS
  boundaries.

## [0.1.0] - 2026-07-17

### Added

- Standalone `ksctl` entrypoint.
- KubeSphere authentication, Fleet/User-scoped token caching, and context management.
- Kubernetes-compatible `get` and `describe` commands with cross-cluster discovery.
- Linux and macOS release archives for amd64 and arm64.

### Security

- Hide passwords during interactive login.
- Contain token cache paths and prevent sanitized-name collisions.
- Atomically persist credential files with mode `0600`.
- Redact stored credentials from `config view` unless `--raw` is explicit.

### Fixed

- Remove the non-functional `--workspace` flag.
- Keep Go module metadata tidy and reproducible.

[Unreleased]: https://github.com/frezes/ksctl/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/frezes/ksctl/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/frezes/ksctl/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/frezes/ksctl/releases/tag/v0.1.0
