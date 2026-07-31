# ksctl CLI Guide

**English** | [简体中文](cli_zh.md)

## Introduction

`ksctl` is a command-line client for KubeSphere 4.x. It connects to a
KubeSphere Endpoint to inspect resources and manage KubeSphere extensions.
The `kube` command provides nearly the full kubectl operation surface through
KubeSphere authentication and supports cross-Cluster routing through
`--cluster`. Tenant commands show how resources accessible to the current
tenant are distributed across Workspaces, Namespaces, and Clusters.

Top-level `get` and tenant inspection are read-only. Commands under `kube` and
Extension lifecycle commands such as `install`, `configure`, and `uninstall`
can change server state.

Before you begin, you need:

- A reachable KubeSphere 4.x API Endpoint.
- A KubeSphere account or bearer Token.
- The `ksctl` executable.

## Command syntax

Commands generally follow this form:

```text
ksctl COMMAND [TYPE] [NAME] [flags]
```

Use built-in help as the complete reference for the installed release:

```bash
ksctl help
ksctl COMMAND --help
```

## Command groups

| Group | Use it to | Commands | Availability |
| --- | --- | --- | --- |
| Kubernetes operations | Use nearly the full kubectl operation surface through KubeSphere. | `kube` | Available |
| Tenant management | Inspect Workspaces and their Namespaces and Clusters. | `tenant` | Available |
| Extension management | Discover, install, configure, diagnose, and remove extensions. | `extension` | Available |
| Application management | Manage KubeSphere applications. | — | Not yet available |
| Other | Authenticate, call APIs, and use plugins. | `auth`, `config`, `api`, `plugin` | Available |

## Manage Kubernetes resources

Top-level `ksctl get` is a concise, read-only entry point for Kubernetes and
discovered KubeSphere resources. `ksctl kube` provides nearly the full kubectl
operation surface while using ksctl connection, authentication, Context, and
Cluster selection.

### Select the resource scope

| Concept | Meaning |
| --- | --- |
| Context | Selects the KubeSphere connection and identity. |
| Cluster | Selects the target Cluster; use `--cluster` for a single command. |
| Namespace | Selects a Kubernetes Namespace or KubeSphere Project. |
| Workspace | Represents the tenant scope used to inspect accessible Namespaces and Clusters. |

Use the saved Context or override its scope for one command:

```bash
ksctl get deployments,pods -n demo --cluster member-1
ksctl kube apply -f app.yaml --cluster member-1
```

`kube` does not read or write `~/.kube/config`; use
`ksctl config generate kubeconfig` when you explicitly need a kubeconfig.
Mutating operations use the resolved KubeSphere credential's Kubernetes RBAC
permissions and receive no additional confirmation from ksctl. Use
`ksctl kube --help` and the upstream kubectl documentation for operation syntax
and behavior.

## Manage tenants

A Workspace groups tenant access to Namespaces and Clusters. The native
`workspace`, `namespace`, and `cluster` forms read these relationships from
the KubeSphere tenant API. Other resource names use kubectl discovery,
selection, and output behavior to read arbitrary Kubernetes resources.

| Command | Purpose |
| --- | --- |
| `ksctl tenant get workspace [NAME]` | List accessible Workspaces or show one Workspace. |
| `ksctl tenant get namespace` | List Namespaces, optionally filtered by Workspace. |
| `ksctl tenant get cluster` | List Clusters, optionally filtered by Workspace. |
| `ksctl tenant get TYPE [NAME]` | Get an arbitrary Kubernetes resource in one Namespace. |
| `ksctl tenant get TYPE -A` | Get a resource across all authorized Namespaces. |
| `ksctl tenant get TYPE --workspace NAME` | Get a namespaced resource across the Workspace's Namespaces. |

List tenant resources and filter them by Workspace:

```bash
ksctl tenant get workspace
ksctl tenant get workspace platform
ksctl tenant get ns --workspace platform
ksctl tenant get ns --workspace platform --cluster member-1
ksctl tenant get cluster --workspace platform
ksctl tenant get pods
ksctl tenant get pods -A
ksctl tenant get deployments --workspace platform
ksctl tenant get widgets.example.io --workspace platform -o yaml
```

Accepted names include `workspace`/`workspaces`,
`namespace`/`namespaces`/`ns`, and `cluster`/`clusters`.

For arbitrary resources, a Cluster administrator's successful `-A` request
uses the native Kubernetes all-Namespaces endpoint. If that endpoint returns
`403`, ksctl resolves the tenant's accessible Namespaces, queries each
Namespace with at most eight requests in flight, and merges the List or Table
before passing it to the kubectl printer. `--workspace` performs the same
bounded query using only that Workspace's Namespaces.

`--workspace` is mutually exclusive with `--namespace` and
`--all-namespaces`, applies only to collection queries for namespaced
resources, and cannot be used with raw or file input. Every Namespace must
succeed; an error discards aggregate output instead of printing a partial
result. Tenant multi-Namespace watch is unsupported, so use `--namespace` for
watching. Cluster-scoped resources cannot use `--workspace`.

For native tenant resources, `--workspace` continues to filter Namespace and
Cluster results. `--cluster` routes Namespace and arbitrary Kubernetes
requests through a selected member Cluster, but it does not change Workspace
or tenant Cluster requests. Native tenant output can be `table`, `json`, or
`yaml`; arbitrary resources accept the normal kubectl get output formats.

## Manage extensions

Extension resources are always managed on the KubeSphere host. A Context
default Cluster is ignored, while explicitly passing the global `--cluster`
flag returns an error. Extension commands do not define the
resource-command-local `--namespace` flag. Use extension placement flags to
select member Clusters, or `diagnose --target-cluster` to inspect a member
status.

| Command | Purpose |
| --- | --- |
| `ksctl extension list` | List and filter available extensions. |
| `ksctl extension show NAME` | Show extension or exact-version details. |
| `ksctl extension versions NAME` | List available versions. |
| `ksctl extension status [NAME]` | Show or watch installation status. |
| `ksctl extension install NAME` | Install an extension. |
| `ksctl extension upgrade NAME --version VERSION` | Upgrade to an exact version. |
| `ksctl extension configure NAME` | Update configuration or placement. |
| `ksctl extension uninstall NAME` | Remove an extension. |
| `ksctl extension diagnose NAME` | Diagnose extension controller state. |

### Discover extensions

Inspect the catalog, available versions, and installation state:

```bash
ksctl extension list --installed
ksctl extension list --category observability
ksctl extension show logging
ksctl extension versions logging
```

Use `-o table`, `-o wide`, `-o json`, or `-o yaml` where supported. JSON and
YAML retain the complete server objects.

### Manage the lifecycle

Install uses the extension's recommended version when `--version` is omitted.
Upgrade always requires an exact version:

```bash
ksctl extension install logging
ksctl extension install logging --version 1.2.1 --wait
ksctl extension upgrade logging --version 1.3.0 --wait
ksctl extension configure logging --config ./logging-values.yaml
ksctl extension uninstall logging --wait
```

Lifecycle requests are asynchronous by default and return after the API
accepts the request. Add `--wait` to poll for completion. Uninstall deletes
the InstallPlan without an interactive confirmation.

Configuration input must be a non-empty YAML mapping. Use `--config FILE`, or
`--config -` to read it from stdin.

### Configure placement and diagnose

Select member Clusters and add per-Cluster overrides:

```bash
ksctl extension install logging --clusters member-a,member-b
ksctl extension configure logging --override member-a=./member-a.yaml
ksctl extension diagnose logging --target-cluster member-a
```

`--all-clusters` resolves the currently eligible Cluster set and saves that
snapshot; it is not a dynamic selector. Diagnosis checks controller state but
does not retrieve logs, Secrets, or rendered Helm values.

Use verbosity 7 or lower for extension commands. Higher REST debug verbosity
can reveal extension configuration.

## Other commands

### Authenticate and select a Context

| Command | Purpose |
| --- | --- |
| `ksctl auth login [ENDPOINT]` | Log in and select the saved Context. |
| `ksctl auth whoami` | Verify credentials and show the server-side identity. |
| `ksctl auth logout [CONTEXT]` | Remove cached login credentials. |
| `ksctl config current-context` | Show the selected Context. |
| `ksctl config use-context NAME` | Select another Context. |
| `ksctl config view` | Show merged, redacted configuration. |

Omitting required login values in a terminal starts the guided flow:

```bash
ksctl auth login
```

Fleet groups an Endpoint, TLS settings, and its Users. During login, the Fleet
name defaults to the Endpoint host and the Context name defaults to
`<fleet>-<username>`. The new Context becomes current.

The password entered by the guided flow is not persisted. For non-interactive
login, avoid exposing `--password` through shell history, logs, or process
inspection. `config view` redacts credentials by default; do not share
`config view --raw` output. Logout clears cached login credentials but does
not delete the Context.

### Override the connection

Use an Endpoint and bearer Token without creating a Context:

```bash
ksctl get workspaces \
  --endpoint https://kubesphere.example.com \
  --token "$KS_TOKEN"
```

An explicit `--endpoint` or `KS_ENDPOINT` must be paired with an explicit
`--token` or `KS_TOKEN`. ksctl never combines an overridden Endpoint with
credentials from the selected Context.

### Generate kubeconfig

Generate kubeconfig for the selected User and Cluster:

```bash
umask 077
ksctl config generate kubeconfig --cluster member-1 > member-1.kubeconfig
```

The server response is written unchanged to stdout and is not merged into
`~/.kube/config`. The output contains credentials, so store it with restrictive
permissions.

### Call an API or use a plugin

Send an authenticated request to a server-relative KubeSphere API path:

```bash
ksctl api /kapis/version
```

`api` defaults to GET. Supplying `--data` without `--method` changes the
default to POST, and the response body is written unchanged. Run
`ksctl api --help` before sending mutating requests.

`API_PATH` is sent unchanged. Neither `--cluster`, `--namespace`, nor a Context
default Cluster adds scope automatically. To target a member Cluster, include
`/clusters/CLUSTER/...` in `API_PATH`.

An executable named `ksctl-foo` provides the external command `ksctl foo`:

```bash
ksctl plugin list
ksctl foo --context prod
```

Plugin flags must follow the plugin name. Plugins run with your user
privileges and are not audited or sandboxed by ksctl; install and run only
plugins you trust.

## Global options and environment variables

| Flag | Purpose |
| --- | --- |
| `--endpoint URL` | Override the KubeSphere API Endpoint. |
| `--token TOKEN` | Override the KubeSphere bearer Token. |
| `--context NAME` | Use a named ksctl Context. |
| `--cluster NAME` | Select a KubeSphere member Cluster. |
| `-v, --v LEVEL` | Set log verbosity. |

Top-level `get` defines command-local `-n, --namespace NAME`. `ksctl kube`
defines persistent `-n, --namespace NAME` and
`--request-timeout DURATION` (`0` means no limit), inherited by its operation
commands. Neither flag is a root connection flag.

| Variable | Purpose |
| --- | --- |
| `KSCTL_CONFIG` | Select another ksctl configuration file. |
| `KS_ENDPOINT` | Supply the default KubeSphere API Endpoint. |
| `KS_TOKEN` | Supply the default KubeSphere bearer Token. |

Explicit flags override environment variables, which override Context defaults
where the setting supports them. Subcommands may define additional flags or
give a flag a command-specific meaning.

## Common workflows

### Tenant workflow

A tenant can discover assigned scope and then inspect workloads in a selected
Namespace and Cluster:

```bash
ksctl auth login
ksctl auth whoami
ksctl tenant get workspace
ksctl tenant get ns --workspace demo
ksctl tenant get cluster --workspace demo
ksctl get deployments,pods -n demo --cluster member-1
```

### Administrator workflow

An administrator can confirm the active environment, inspect platform state,
and manage an extension:

```bash
ksctl auth login
ksctl auth whoami
ksctl config current-context
ksctl config use-context prod-admin
ksctl get pods -A --cluster member-1
ksctl tenant get workspace
ksctl extension list --installed
ksctl extension install logging --version 1.2.1 --wait
ksctl extension diagnose logging
ksctl api /kapis/version
```

## Troubleshooting

- Run `ksctl help` or `ksctl COMMAND --help` to confirm syntax for the
  installed release.
- Run `ksctl config current-context` to confirm the selected Context.
- Run `ksctl config view` to inspect redacted connection and scope settings.
- An unknown resource type usually means the selected server or Cluster did
  not advertise it through discovery.
- A login-required error means no usable credential was found for the selected
  connection.
- Avoid `ksctl config view --raw` during routine troubleshooting because it can
  reveal credentials and TLS private key data.
