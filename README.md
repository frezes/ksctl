# KubeSphere CLI (ksctl)

`ksctl` is a command-line client for KubeSphere 4.x and the Kubernetes
resources exposed through KubeSphere. It provides familiar kubectl-style
inspection commands alongside KubeSphere authentication, tenant, Extension,
and API workflows.

## Highlights

- Inspect Kubernetes and discovered KubeSphere resources with familiar
  kubectl-style commands.
- Authenticate, switch saved Contexts, and target host or member Clusters.
- Explore tenant Workspaces, Namespaces, and Clusters.
- Discover, install, configure, diagnose, and remove KubeSphere Extensions.
- Send authenticated raw API requests and extend the CLI with `ksctl-*`
  executable plugins.

The built-in `get`, `describe`, `logs`, `top`, and `tenant get` workflows are
read-only. Extension lifecycle commands can change KubeSphere state.

## Install a release

Release archives are available for Linux and macOS on amd64 and arm64.
Download the matching `ksctl_VERSION_OS_ARCH.tar.gz` archive from
[GitHub Releases](https://github.com/frezes/ksctl/releases).

For example, install the macOS arm64 binary:

```bash
version=v0.2.0
archive="ksctl_${version#v}_darwin_arm64.tar.gz"
curl -LO "https://github.com/frezes/ksctl/releases/download/${version}/${archive}"
curl -LO "https://github.com/frezes/ksctl/releases/download/${version}/checksums.txt"
grep "  ${archive}$" checksums.txt | shasum -a 256 -c -
tar -xzf "${archive}"
sudo install -m 0755 ksctl /usr/local/bin/ksctl
```

On Linux, select the matching Linux archive and verify it with
`sha256sum -c -` instead of `shasum -a 256 -c -`.

## Build from source

Go 1.26 or later is required. Build `ksctl` into `bin/`:

```bash
make build
./bin/ksctl version
```

## Quick start

Log in, verify the selected identity and Context, then explore the resources
available to it:

```bash
ksctl auth login
ksctl auth whoami
ksctl config current-context

ksctl get pods -A
ksctl describe deployment web -n demo
ksctl logs deployment/web -n demo --all-pods
ksctl top pod -n demo

ksctl tenant get workspace
ksctl extension list
ksctl api /kapis/version
```

Interactive login prompts for missing connection and account values, reads the
password without echo, saves the new Context, and makes it current. The
`web` workload and `demo` Namespace above are example names; replace them with
resources from your environment. The `top` command requires Metrics Server in
the selected Cluster.

## Command overview

| Command | Purpose |
| --- | --- |
| `auth` | Log in, inspect the current identity, and log out. |
| `config` | Inspect and select Contexts or generate kubeconfig. |
| `get`, `describe` | Inspect Kubernetes and discovered KubeSphere resources. |
| `logs` | Read Pod or workload container logs. |
| `top` | View current Pod or Node CPU and memory usage. |
| `tenant` | Inspect tenant Workspaces, Namespaces, and Clusters. |
| `extension` | Discover and manage KubeSphere Extensions. |
| `api` | Send an authenticated request to a KubeSphere API path. |
| `plugin` | List `ksctl-*` executable plugins available on `PATH`. |
| `completion` | Generate shell completion scripts. |
| `version` | Print client and server version information. |

Run `ksctl COMMAND --help` for the complete reference for an installed release.
See the [CLI guide](docs/cli.md) for command workflows, flags, and
troubleshooting.

## Scope and connection

| Concept | Meaning |
| --- | --- |
| Context | Selects a saved KubeSphere connection and identity. |
| Cluster | Selects the KubeSphere host or a member Cluster. |
| Namespace | Selects a Kubernetes Namespace or KubeSphere Project for resource commands. |
| Workspace | Filters tenant relationships inspected with `tenant get`. |

Use the global `--context` and, where supported, `--cluster` flags to override
the saved scope for one command. Use `--endpoint` together with `--token` for a
direct connection without a saved Context. Kubernetes resource commands also
accept the command-local `--namespace` flag.

## Documentation

- [CLI guide (English)](docs/cli.md) — commands, scope, workflows, security, and
  troubleshooting.
- [Design (English)](docs/design.md) — core design, cross-Cluster access,
  tenant and Extension flows, raw API requests, and authentication.

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
- `clean` removes the generated development binary.
