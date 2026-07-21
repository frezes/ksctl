# ksctl CLI Guide

## Overview

`ksctl` is a command-line client for inspecting KubeSphere 4.x resources and
the Kubernetes resources exposed through KubeSphere. Its built-in resource
commands are read-only: `get` displays resources and `describe` displays their
detailed state.

The release provides two equivalent entrypoints backed by the same command
tree:

```bash
ksctl get workspaces
kubectl ks get workspaces
```

The examples in this guide use `ksctl`. Replace `ksctl` with `kubectl ks` when
using the kubectl plugin.

## Prerequisites

- A reachable KubeSphere 4.x API endpoint
- A KubeSphere account or bearer token
- The `ksctl` executable, or the `kubectl-ks` executable on `PATH` for use as
  `kubectl ks`

## Command syntax

Resource commands use this general syntax:

```text
ksctl COMMAND TYPE [NAME] [flags]
```

- `COMMAND` is `get` or `describe`.
- `TYPE` is a resource type advertised by server discovery. Singular, plural,
  short, versioned, and group-qualified names are accepted when advertised.
- `NAME` selects one resource. Omit it to select a list, or use the forms shown
  by the command-specific help.
- Flags select a connection and scope, filter resources, or change output.

Use built-in help to inspect the live command surface:

```bash
ksctl help
ksctl get --help
ksctl config generate kubeconfig --help
```

## Commands

| Command | Description |
| --- | --- |
| `ksctl get TYPE [NAME]` | Display one or more resources. |
| `ksctl describe TYPE [NAME_PREFIX]` | Display resource details and related information. |
| `ksctl auth login [ENDPOINT]` | Authenticate with a username and password, then save a Context and token cache. |
| `ksctl auth whoami` | Verify the selected credential and display the server-side User and global role. |
| `ksctl auth logout [CONTEXT]` | Delete cached login credentials for the current or named Context. |
| `ksctl config view` | Display the merged configuration, redacted by default. |
| `ksctl config current-context` | Display the current Context name. |
| `ksctl config use-context NAME` | Select an existing Context. |
| `ksctl config generate kubeconfig` | Write the selected user's kubeconfig to stdout. |
| `ksctl plugin list` | List and diagnose `ksctl-*` executable plugins on `PATH`. |
| `ksctl completion SHELL` | Generate a completion script for bash, fish, PowerShell, or zsh. |
| `ksctl version` | Display the ksctl, KubeSphere, and Kubernetes versions. |

## Authentication

### Interactive login

Omitting the Endpoint, Username, or Password in a terminal starts the guided
login flow:

```text
$ ksctl auth login
endpoint: https://kubesphere.example.com
username: admin
password:
fleet [kubesphere.example.com]:
context [kubesphere.example.com-admin]:
Logged in to "kubesphere.example.com-admin"
```

The password is read without echo, used only for the login request, and not
persisted. Press Enter at the Fleet and Context prompts to accept their derived
defaults. Fleet defaults to the Endpoint Host; Context defaults to
`<fleet>-<username>`. The new Context becomes current.

### Non-interactive login

When Endpoint, Username, and Password are all supplied, login does not prompt.
Omitted Fleet and Context names are derived silently, which makes the command
suitable for scripts:

```bash
export KS_PASSWORD='your-password'
ksctl auth login https://prod.example.com \
  --username admin \
  --password "$KS_PASSWORD" \
  --fleet prod \
  --context prod-admin
```

Take care not to expose the password through shell history, logs, or process
inspection when using `--password`.

### Current identity

Verify the selected Context's credentials and display its server-side identity:

```text
$ ksctl auth whoami
Username: admin
Global Role: platform-admin
```

`auth whoami` requires a current or explicitly selected Context because the
Context supplies the User name. It authenticates to KubeSphere and reads
`/kapis/iam.kubesphere.io/v1beta1/users/<username>`; it does not merely echo
local configuration. A User without the `iam.kubesphere.io/globalrole`
annotation is displayed as `Global Role: <none>`.

### Logout

Log out the current Context or name another Context explicitly:

```bash
ksctl auth logout
ksctl auth logout prod-admin
```

Logout makes a best-effort request to the Fleet's `/oauth/logout` endpoint
using the cached Access Token, then removes the token cache for the selected
Fleet and User regardless of the remote result. It does not delete Contexts or
manually configured credentials. Contexts that select the same Fleet and User
share that cache and logout state. A configured `bearerToken` or
`bearerTokenFile` can still authenticate later commands.

## Scope and connection selection

ksctl separates connection identity from resource scope:

| Concept | Meaning |
| --- | --- |
| Fleet | A KubeSphere Endpoint, TLS settings, and its Fleet-scoped Users. |
| User | A KubeSphere username and optional configured credential sources within one Fleet. |
| Context | A Fleet/User selection with an optional default Cluster. |
| Cluster | A KubeSphere member Cluster selected for a request. |
| Namespace | A Kubernetes Namespace or KubeSphere Project selected for a resource command. |

The current Context supplies the Endpoint, User, and default Cluster. Override
it for one invocation with `--context`; override its Cluster with `--cluster`.
Use `-n` or `--namespace` for a Namespace or Project, and `-A` or
`--all-namespaces` where supported.

```bash
ksctl get workspaces --context prod-admin
ksctl get clusters --context prod-admin
ksctl get pods -A --cluster member-1
ksctl get deployments -n demo --cluster member-1
```

An Endpoint and bearer Token can also be supplied without creating a Context:

```bash
ksctl get workspaces \
  --endpoint https://kubesphere.example.com \
  --token "$KS_TOKEN"
```

## Configuration and credentials

### Configuration file

ksctl uses `~/.ksctl/config.yaml`, independently of kubeconfig. Set
`KSCTL_CONFIG` to select another file.

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
        bearerTokenFile: /secure/path/prod.token
contexts:
  prod-admin:
    fleet: prod
    user: admin
    defaultCluster: member-1
```

Users belong to a Fleet, so different Fleets may each contain a User named
`admin`. When `username` is omitted, the User map key is used. A User may
configure `bearerTokenFile`, `bearerToken`, or a plaintext `password`. A
password written manually into this file remains plaintext even though
`auth login` itself never persists passwords.

New configuration directories use mode `0700`; new configuration files use
mode `0600`.

### Context commands

Inspect or select Context state with:

```bash
ksctl config current-context
ksctl config use-context prod-admin
ksctl config view
```

`config view` redacts passwords, bearer tokens, and TLS private key data. Use
`config view --raw` only when unredacted values are required, and never copy
raw output into logs, issue reports, or chat messages.

### Credential resolution

Credentials are selected in this order:

```text
--token > KS_TOKEN > bearerTokenFile > bearerToken > token cache > password
```

- `--token` and `KS_TOKEN` bypass configured and cached credentials.
- A configured Token File or Token is used directly and bypasses token cache
  refresh and password login. Read errors, empty Token Files, and subsequent
  API authorization failures do not fall through to another credential.
- A cached Access Token is reused while valid. If expired, its Refresh Token is
  used when possible and the refreshed response replaces the cache.
- If no usable cache remains and the selected User has a Password, ksctl logs
  in for the current command only and does not cache that response.
- If no credential is usable, the command reports that login is required.

`auth login` stores the complete KubeSphere OAuth response under
`~/.ksctl/cache/tokens/<fleet>/<user>.json`. New cache directories use mode
`0700`, and new cache files use mode `0600`.

## Inspect resources

ksctl resolves resource types from KubeSphere server discovery rather than a
static local registry. Accepted names, API groups, versions, and whether a
resource is namespaced therefore depend on the connected server.

### Get resources

`get` displays one object or a resource list. It uses the kubectl v0.36.2
resource builder and printers, including multi-type requests:

```bash
ksctl get workspaces
ksctl get workspace demo
ksctl get deployments,pods -n demo
ksctl get pod/web-0 -n demo
```

### Describe resources

`describe` produces human-readable details and related Events. Kubernetes
types use kubectl's built-in Describers; other discovered resources use its
generic fallback.

```bash
ksctl describe workspace demo
ksctl describe cluster member-1
ksctl describe deployment web -n demo
ksctl describe pod/web-0 -n demo --cluster member-1
```

`describe` accepts an exact name, a name prefix, or selectors as described by
`ksctl describe --help`. It does not support structured `-o` output; use `get`
for that.

### Select scope

```bash
ksctl get pods -n demo
ksctl get pods -A
ksctl get pods -A --cluster member-1
ksctl describe deployment web -n demo --cluster member-1
```

### Filter, sort, and watch

The resource commands retain kubectl-compatible selectors and list behavior:

```bash
ksctl get pods -l app=web
ksctl get pods --field-selector=status.phase=Running
ksctl get pods --sort-by=.metadata.name
ksctl get pods --watch
```

Run `ksctl get --help` and `ksctl describe --help` for their complete command
flags.

### Select output

`get` requests a server-provided table by default. Use `-o` to select another
format:

```bash
ksctl get pods -o wide
ksctl get pod web-0 -o yaml
ksctl get deployments -o json
ksctl get pod web-0 -o jsonpath='{.status.phase}'
ksctl get pods -o custom-columns=NAME:.metadata.name
```

Supported formats are listed by `ksctl get --help` and come from the pinned
kubectl implementation.

## Generate kubeconfig

Generate kubeconfig for the current logged-in User:

```bash
ksctl config generate kubeconfig
```

An explicit `--cluster` overrides the current Context's `defaultCluster`.
Otherwise, the default is used. The server response is written unchanged to
stdout and is never merged into `~/.kube/config`.

Kubeconfig contains credentials. Use a restrictive umask before redirecting it
to a file:

```bash
umask 077
ksctl config generate kubeconfig --cluster member-1 > member-1.kubeconfig
```

Generation requires a selected Context because the User identity comes from
that Context even when connection flags are supplied.

## Executable plugins

An executable whose name begins with `ksctl-` and is available on `PATH` can
provide an external command. For example, `ksctl-foo` provides both forms:

```bash
ksctl foo [arguments and flags]
kubectl ks foo [arguments and flags]
```

Nested words are joined with dashes. If both `ksctl-foo` and
`ksctl-foo-bar` exist, `ksctl foo bar` selects the longest match. Dashes in a
command word map to underscores in the executable name, so `ksctl foo-bar`
can invoke `ksctl-foo_bar`.

Plugin flags must follow the plugin name:

```bash
ksctl foo --context prod
```

Do not use `ksctl --context prod foo`; flags before an unknown plugin name are
rejected. ksctl passes unmatched arguments and the inherited environment to
the selected executable unchanged. Each plugin must parse its own flags and
obtain any connection settings it needs.

List and diagnose candidates with:

```bash
ksctl plugin list
ksctl plugin list --name-only
```

The listing reports non-executable files, PATH shadowing, and conflicts with
built-in commands. It returns a non-zero status when no plugins are found,
plugin directories cannot be read, or warnings are present. Built-in commands
cannot be replaced or extended by plugins.

Plugins are arbitrary programs that run with your user privileges. ksctl does
not audit or sandbox them; install and run only plugins you trust.

## Environment variables

| Variable | Purpose |
| --- | --- |
| `KSCTL_CONFIG` | Select an alternate ksctl configuration file. |
| `KS_ENDPOINT` | Supply the default KubeSphere API Endpoint. |
| `KS_TOKEN` | Supply the default KubeSphere bearer Token. |

Explicit command-line flags take precedence over their environment defaults.

## Global flags

| Flag | Purpose |
| --- | --- |
| `--endpoint URL` | Override the KubeSphere API Endpoint. |
| `--token TOKEN` | Override the KubeSphere bearer Token. |
| `--context NAME` | Use a named ksctl Context. |
| `--cluster NAME` | Select a KubeSphere member Cluster. |
| `-n, --namespace NAME` | Select a Kubernetes Namespace or KubeSphere Project. |
| `--request-timeout DURATION` | Limit the duration of a single server request; `0` means no limit. |
| `-v, --v LEVEL` | Set log verbosity. |

Subcommands may add flags or, as with `auth login --context`, define a local
flag with command-specific meaning.

## Common workflows

Inspect KubeSphere resources:

```bash
ksctl get workspaces
ksctl describe workspace demo
ksctl get clusters
ksctl describe cluster member-1
```

Inspect Kubernetes resources through a member Cluster:

```bash
ksctl get deployments,pods -n demo -l app=web -o wide --cluster member-1
ksctl describe deployment web -n demo --cluster member-1
```

Switch environments:

```bash
ksctl config current-context
ksctl config use-context staging-admin
ksctl get workspaces
```

## Troubleshooting

- Run `ksctl help` or `ksctl COMMAND --help` to confirm syntax and flags for
  the installed version.
- Run `ksctl config current-context` to confirm which Context is selected.
- Run `ksctl config view` to inspect redacted connection and scope settings.
- Run `ksctl plugin list` to diagnose executable permissions, PATH shadowing,
  and built-in command conflicts.
- An unknown resource type usually means the connected server did not
  advertise it through discovery; confirm the Endpoint, Context, Cluster, and
  Namespace or Project.
- A login-required error means no usable explicit, configured, or cached
  credential was found for the selected connection.

Avoid `ksctl config view --raw` during routine troubleshooting because its
output can contain credentials and TLS private key data.
