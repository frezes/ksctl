# KubeSphere CLI (ksctl)

English | [简体中文](README_zh.md)

`ksctl` is a command-line tool for working with KubeSphere 4.x resources and
the Kubernetes resources exposed through KubeSphere.

The current command surface focuses on resource inspection. Its `get` and
`describe` commands reuse [kubectl v0.36.2](https://kubernetes.io/docs/reference/kubectl/)
resource discovery, REST mapping,
printing, selectors, watching, built-in describers, generic describe fallback,
and event handling.

## Prerequisites

- Go 1.26 or later
- A reachable KubeSphere 4.x API endpoint
- A KubeSphere account or bearer token

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

On Linux, verify with `sha256sum -c -` instead of `shasum -a 256 -c -`.
To install the plugin, select the matching `kubectl-ks` archive and place the
extracted `kubectl-ks` executable on `PATH`; invoke it as `kubectl ks`.

## Build from source

Build `ksctl` and `kubectl-ks` into `bin/`:

```bash
make build
```

Check the resulting binary:

```bash
./bin/ksctl version
```

## Quick start

Log in to KubeSphere.

When stdin is a terminal, ksctl reads the password without echoing it. The
password is used only for the login request and is never persisted.

```text
$ ./bin/ksctl auth login
endpoint: https://kubesphere.example.com
username: admin
password: your-password
fleet [kubesphere.example.com]:
context [kubesphere.example.com-admin]:
Logged in to "kubesphere.example.com-admin"
```

The new context becomes current after login, so subsequent commands can use its
endpoint and cached token directly:

```bash
./bin/ksctl get workspaces
./bin/ksctl get pods -A
```

## Syntax

Use the following syntax for resource commands:

```text
ksctl [command] [TYPE] [NAME] [flags]
```

- `command` is the operation to perform, such as `get` or `describe`.
- `TYPE` is a discovered resource type. Singular, plural, and short names are
  accepted when the API advertises them.
- `NAME` is the name of one resource. Omit it to operate on the resource list.
- `flags` select a context or scope, filter results, or change output.

Use `ksctl help`, `ksctl <command> --help`, or
`ksctl <command> <subcommand> --help` for command-specific help.

## Commands

| Command | Description |
| --- | --- |
| `ksctl get TYPE [NAME]` | Display one or more resources. |
| `ksctl describe TYPE [NAME]` | Display detailed resource state and related information. |
| `ksctl auth login [ENDPOINT]` | Authenticate with a username and password, then save the context and token cache. |
| `ksctl auth logout [CONTEXT]` | Delete cached credentials for a context. |
| `ksctl config view` | Display the merged ksctl configuration. |
| `ksctl config current-context` | Display the current context name. |
| `ksctl config use-context NAME` | Select an existing context. |
| `ksctl config generate kubeconfig` | Write the current logged-in user's kubeconfig to stdout. |
| `ksctl plugin list` | List and diagnose `ksctl-*` executable plugins on `PATH`. |
| `ksctl version` | Display the ksctl, KubeSphere, and Kubernetes versions. |

## Plugins

ksctl supports kubectl-style executable plugins. A plugin is an executable
whose name begins with `ksctl-` and is available on `PATH`. For example, an
executable named `ksctl-foo` provides both entrypoints:

```bash
ksctl foo [arguments and flags]
kubectl ks foo [arguments and flags]
```

Nested command words are joined with dashes. Given both `ksctl-foo` and
`ksctl-foo-bar`, `ksctl foo bar` selects `ksctl-foo-bar`; unmatched words and
all following flags are passed to the selected executable. Dashes in command
words map to underscores in executable names, so `ksctl foo-bar` can invoke
`ksctl-foo_bar`.

List visible candidates and diagnose permissions, PATH shadowing, and built-in
command conflicts:

```bash
ksctl plugin list
ksctl plugin list --name-only
```

The list command prints each candidate before its associated diagnostics and
returns a non-zero status when warnings are found. Built-in commands cannot be
replaced or extended by plugins. The plugin name must appear before its flags: use
`ksctl foo --context prod`, not `ksctl --context prod foo`. ksctl passes
arguments and the inherited environment unchanged; each plugin must parse its
own flags and obtain any connection settings it needs.

Plugins are arbitrary programs running with your user privileges. ksctl does
not audit or sandbox them, so install and run only plugins you trust. See the
[kubectl plugin documentation](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/)
for the compatible executable naming and dispatch model.

## Scope and connection flags

Scope flags let the same resource commands address KubeSphere and Kubernetes
resources at different levels.

| Flag | Description |
| --- | --- |
| `--context NAME` | Use a named ksctl context. |
| `--cluster NAME` | Select a KubeSphere cluster. |
| `-n, --namespace NAME` | Select a Kubernetes namespace or KubeSphere project. |
| `--endpoint URL` | Override the KubeSphere API endpoint. |
| `--token TOKEN` | Override the bearer token. |
| `--request-timeout DURATION` | Set the timeout for a single server request. |

`KS_ENDPOINT` and `KS_TOKEN` provide endpoint and token defaults. Explicit
command-line flags take precedence.

## Output and filtering

`get` prints a server-provided table by default. Use `-o` to select another
output format:

```bash
ksctl get pods
ksctl get pods -o wide
ksctl get pod web-0 -o yaml
ksctl get deployments -o json
ksctl get pod web-0 -o jsonpath='{.status.phase}'
```

Filter, sort, or watch resources with kubectl-compatible flags:

```bash
ksctl get pods -l app=web
ksctl get pods --field-selector=status.phase=Running
ksctl get pods --sort-by=.metadata.name
ksctl get pods --watch
```

Run `ksctl get --help` for all supported output formats and selection flags.

## Common operations

Inspect KubeSphere resources:

```bash
ksctl get workspaces
ksctl describe workspace demo
ksctl get clusters
ksctl describe cluster member-1
```

Inspect Kubernetes resources through KubeSphere:

```bash
ksctl get deployments,pods -n demo -l app=web -o wide
ksctl describe deployment web -n demo
ksctl get pods -A --cluster member-1
ksctl describe pod/web-0 -n demo --cluster member-1
```

Use an endpoint and token without creating a context:

```bash
ksctl get workspaces \
  --endpoint https://kubesphere.example.com \
  --token "$KS_TOKEN"
```

## Configuration

ksctl uses `~/.ksctl/config.yaml`, independently of kubeconfig. Set
`KSCTL_CONFIG` to use another path.

`ksctl config view` redacts stored passwords, bearer tokens, and TLS private
key data. Use `ksctl config view --raw` only when the unredacted values are
required, and do not send raw output to logs or issue reports.

```yaml
apiVersion: ksctl.kubesphere.io/v1alpha1
kind: Config
currentContext: prod-admin
fleets:
  prod:
    host: https://prod.example.com
    users:
      admin:
        username: admin
        password: "<plaintext-password>"
  staging:
    host: https://staging.example.com
    users:
      admin:
        username: admin
        bearerToken: "<token>"
contexts:
  prod-admin:
    fleet: prod
    user: admin
    defaultCluster: ""
  staging-admin:
    fleet: staging
    user: admin
    defaultCluster: ""
```

New configuration directories are created with mode `0700`, and new
configuration files with mode `0600`. `username` defaults to the user map key
when omitted. Users are scoped to their Fleet, so different Fleets may both
contain an `admin` account. A User may configure `bearerTokenFile`,
`bearerToken`, or a plaintext `password`. Empty optional fields, empty User
maps, and an empty `tlsClientConfig` block are omitted; `defaultCluster` is
always displayed and defaults to an empty string. Root-level `users` are not
supported or migrated.

Use the config commands to inspect or switch contexts and to retrieve the
selected login's kubeconfig:

```bash
ksctl config view
ksctl config current-context
ksctl config use-context prod-admin
umask 077
ksctl config generate kubeconfig > member.kubeconfig
ksctl config generate kubeconfig --cluster member-1 > member-1.kubeconfig
```

Kubeconfig generation requires a selected login context. An explicit
`--cluster` overrides that context's `defaultCluster`; otherwise the default is
used. The kubeconfig is written unchanged to stdout and is never merged into
`~/.kube/config`. Kubeconfig output contains credentials; use a restrictive
umask such as `077` before redirecting it to a file.

## Authentication

`ksctl auth login` stores non-sensitive connection metadata in the config file
and the complete KubeSphere `/oauth/token` response in
`~/.ksctl/cache/tokens/<fleet>/<user>.json`. New token cache directories use
mode `0700`, and new cache files use mode `0600`. The password passed to
`auth login` is never persisted. A password explicitly written by the user in
the Config remains plaintext in the Config file.

Credentials are resolved in this order:

```text
--token > KS_TOKEN > bearerTokenFile > bearerToken > token cache > password
```

A configured Token File or Token is used directly and bypasses cache Refresh
and password login. Read, empty-file, and API authorization errors are returned
without trying another credential. An expired cached Access Token is refreshed
automatically when possible. If no usable cache remains and a Config password
is present, ksctl requests an Access Token for the current command only and
does not cache it.

`auth logout` removes only cached login state; manually configured credentials
remain unchanged. Contexts that select the same Fleet and User share one Token
cache and logout state. Old Context-level cache files are not read or migrated.

Use `--fleet` to choose a Fleet name during login. Without it, ksctl derives
the Fleet name from the Endpoint Host. Without `--context`, the Context name is
`<fleet>-<username>`; existing Contexts are never used to infer a Fleet.

In an interactive terminal, omitting Endpoint, Username, or Password starts the
guided login flow. After the missing required values are supplied, Fleet and
Context prompts display their derived defaults; press Enter to accept a default
or type a replacement. When Endpoint, Username, and Password are all supplied,
ksctl logs in without prompting and silently derives omitted Fleet and Context
names, so no separate non-interactive flag is required for automation.

```bash
export KS_PASSWORD='your-password'
ksctl auth login https://prod.example.com \
  --username admin \
  --password "$KS_PASSWORD" \
  --fleet prod \
  --context prod-admin
```

To remove the cache for the current or a named Context:

```bash
ksctl auth logout
ksctl auth logout local
```

## Development

The Makefile exposes these development targets:

```bash
make build
make test
make verify
make clean
```

- `build` creates `bin/ksctl` and `bin/kubectl-ks`.
- `test` runs all Go tests.
- `verify` is the local release gate: it checks formatting and module metadata,
  then runs vet, normal tests, race tests, and both builds.
- `clean` removes both binaries.
