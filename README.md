# KubeSphere CLI (ksctl)

`ksctl` is a command-line client for inspecting KubeSphere 4.x resources and
the Kubernetes resources exposed through KubeSphere.

## Features

- Inspect KubeSphere and Kubernetes resources with kubectl-compatible `get`
  and `describe`, stream container logs with `logs`, and view Metrics Server
  CPU/memory usage with `top`.
- Inspect KSE tenant Workspaces, Namespaces, and Clusters with `tenant get`.
- Log in interactively or supply credentials for scripts and automation.
- Select KubeSphere Contexts, member Clusters, Namespaces, and Projects.
- Generate kubeconfig for the selected KubeSphere user and Cluster.
- Extend the command surface with kubectl-style `ksctl-*` executable plugins.

The built-in resource commands are read-only.

## Install a release

Release archives are available for Linux and macOS on amd64 and arm64. Download
the matching `ksctl_VERSION_OS_ARCH.tar.gz` archive from the GitHub Release.

For example, install the macOS arm64 standalone binary:

```bash
version=v0.1.0
archive="ksctl_${version#v}_darwin_arm64.tar.gz"
curl -LO "https://github.com/frezes/ksctl/releases/download/${version}/${archive}"
curl -LO "https://github.com/frezes/ksctl/releases/download/${version}/checksums.txt"
grep "  ${archive}$" checksums.txt | shasum -a 256 -c -
tar -xzf "${archive}"
sudo install -m 0755 ksctl /usr/local/bin/ksctl
```

On Linux, verify with `sha256sum -c -` instead of `shasum -a 256 -c -`.

## Build from source

Go 1.26 or later is required. Build `ksctl` into `bin/`:

```bash
make build
./bin/ksctl version
```

## Quick start

Log in, then inspect KubeSphere and Kubernetes resources:

```bash
ksctl auth login
ksctl get workspaces
ksctl get pods -A
ksctl logs deployment/web -n demo --all-pods
ksctl top pod -n demo
ksctl tenant get workspace
ksctl tenant get ns --workspace demo --cluster member-1
ksctl tenant get cluster --workspace demo
```

Interactive login prompts for missing connection and account values, reads the
password without echo, and selects the new Context for later commands.

## Documentation

- [CLI guide (English)](docs/cli.md) — commands, scope, workflows, and
  troubleshooting.
- [CLI 指南（简体中文）](docs/cli_zh.md) — ksctl 命令、作用域、工作流和故障排查。
- [Design (English)](docs/design.md) — core design, cross-Cluster access,
  tenant and Extension flows, raw API requests, and authentication.
- [设计文档（简体中文）](docs/design_zh.md) — 核心设计、跨集群访问、租户与扩展
  组件流程、原始 API 请求和认证。

## Development

```bash
make build
make test
make verify
make clean
```

- `build` creates `bin/ksctl`.
- `test` runs all Go tests once.
- `verify` checks formatting and modules, then runs vet, normal tests, race
  tests, and the `ksctl` build.
- `clean` removes the generated binary.
