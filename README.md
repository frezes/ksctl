# KubeSphere CLI (ksctl)

`ksctl` is a command-line client for inspecting KubeSphere 4.x resources and
the Kubernetes resources exposed through KubeSphere. Use it as the standalone
`ksctl` command or install `kubectl-ks` and invoke the same command tree as
`kubectl ks`.

## Features

- Inspect KubeSphere and Kubernetes resources with kubectl-compatible `get`
  and `describe` commands.
- Inspect KSE tenant Workspaces, Namespaces, and Clusters with `tenant get`.
- Log in interactively or supply credentials for scripts and automation.
- Select KubeSphere Contexts, member Clusters, Namespaces, and Projects.
- Generate kubeconfig for the selected KubeSphere user and Cluster.
- Extend the command surface with kubectl-style `ksctl-*` executable plugins.

The built-in resource commands are read-only.

## Install a release

Release archives are available for Linux and macOS on amd64 and arm64. Choose
the standalone `ksctl_VERSION_OS_ARCH.tar.gz` archive or the kubectl plugin
`kubectl-ks_VERSION_OS_ARCH.tar.gz` archive from the GitHub Release.

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

On Linux, verify with `sha256sum -c -` instead of `shasum -a 256 -c -`. To
install the kubectl plugin, download the matching `kubectl-ks` archive and put
the extracted `kubectl-ks` executable on `PATH`.

## Build from source

Go 1.26 or later is required. Build both entrypoints into `bin/`:

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
ksctl tenant get workspace
ksctl tenant get ns --workspace demo --cluster member-1
ksctl tenant get cluster --workspace demo
```

Interactive login prompts for missing connection and account values, reads the
password without echo, and selects the new Context for later commands.

## Documentation

- [CLI guide](docs/cli.md) — commands, authentication, configuration, resource
  workflows, kubeconfig generation, and plugins.
- [Design](docs/design.md) — architecture, client boundaries, routing,
  persistence, security properties, and compatibility.

## Development

```bash
make build
make test
make verify
make clean
```

- `build` creates `bin/ksctl` and `bin/kubectl-ks`.
- `test` runs all Go tests once.
- `verify` checks formatting and modules, then runs vet, normal tests, race
  tests, and both builds.
- `clean` removes the generated binaries.
