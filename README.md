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

## Build from source

Build `ksctl` into `bin/ksctl`:

```bash
make build
```

Check the resulting binary:

```bash
./bin/ksctl version
```

## Quick start

Log in to KubeSphere. The password is used only for this request and is not
written to the configuration file.

```bash
export KS_PASSWORD='your-password'
./bin/ksctl auth login https://kubesphere.example.com \
  --username admin \
  --password "$KS_PASSWORD" \
  --context local
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
| `ksctl auth login ENDPOINT` | Authenticate with a username and password, then save the context and token cache. |
| `ksctl auth logout [CONTEXT]` | Delete cached credentials for a context. |
| `ksctl config view` | Display the merged ksctl configuration. |
| `ksctl config current-context` | Display the current context name. |
| `ksctl config use-context NAME` | Select an existing context. |
| `ksctl version` | Display client build information. |

## Scope and connection flags

Scope flags let the same resource commands address KubeSphere and Kubernetes
resources at different levels.

| Flag | Description |
| --- | --- |
| `--context NAME` | Use a named ksctl context. |
| `--cluster NAME` | Select a KubeSphere cluster. |
| `--workspace NAME` | Select a KubeSphere workspace. |
| `-n, --namespace NAME` | Select a Kubernetes namespace or KubeSphere project. |
| `--endpoint URL` | Override the KubeSphere API endpoint. |
| `--token TOKEN` | Override the bearer token. |
| `--request-timeout DURATION` | Set the timeout for a single server request. |
| `--no-interactive` | Fail instead of prompting for missing input. |
| `--insecure-skip-tls-verify` | Skip server certificate validation. |

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

```yaml
apiVersion: ksctl.kubesphere.io/v1alpha1
kind: Config
currentContext: local
clusters:
  local:
    host: https://kubesphere.example.com
    tlsClientConfig:
      insecure: false
users:
  admin:
    username: admin
    bearerTokenFile: ""
    bearerToken: ""
contexts:
  local:
    cluster: local
    user: admin
    defaultCluster: ""
    defaultWorkspace: ""
```

New configuration directories are created with mode `0700`, and new
configuration files with mode `0600`. `username` defaults to the user map key
when omitted.

Use the config commands to inspect or switch contexts:

```bash
ksctl config view
ksctl config current-context
ksctl config use-context local
```

## Authentication

`ksctl auth login` stores non-sensitive connection metadata in the config file
and the complete KubeSphere `/oauth/token` response in
`~/.ksctl/cache/tokens/<context>.json`. New token cache directories use mode
`0700`, and new cache files use mode `0600`. Passwords are never persisted.

Credentials are resolved in this order:

```text
--token > KS_TOKEN > token cache > bearerTokenFile > bearerToken
```

An expired cached access token is refreshed automatically when a refresh token
is available. If refresh fails, log in again. To remove the cached credentials
for the current or a named context:

```bash
ksctl auth logout
ksctl auth logout local
```

## Development

The Makefile exposes three development targets:

```bash
make build
make test
make clean
```

- `build` creates `bin/ksctl`.
- `test` runs all Go tests.
- `clean` removes `bin/ksctl`.
