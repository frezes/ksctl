# ksctl Design

**English** | [简体中文](design_zh.md)

This document explains the core design of ksctl for developers. For command
syntax and workflows, see the [CLI guide](cli.md).

## Goals and boundaries

ksctl provides one command-line interface for KubeSphere 4.x and the
Kubernetes APIs exposed through KubeSphere. It preserves upstream kubectl
operation behavior while keeping connection, authentication, and scope
selection consistent across commands.

The command surface has four distinct safety boundaries:

- top-level `get` and tenant inspection are read-only;
- generic Kubernetes reads and mutations run under `kube` with the same
  KubeSphere authentication and Cluster routing;
- Extension lifecycle commands provide purpose-built, controlled writes; and
- `api` is a raw authenticated escape hatch whose caller-selected requests may
  mutate server state.

Executable plugins run as independent programs under the user's authority and
are outside these built-in safeguards.

The following remain non-goals:

- A local reimplementation or fork of kubectl operation behavior
- Reading, merging, or writing `~/.kube/config`
- kubectl CLI self-management commands under `kube`
- Automatic fallback of failed member-Cluster operations to the host Cluster
- Aggregating resources across Clusters
- Auditing or sandboxing plugins
- Supporting KubeSphere 3.x

## Architecture overview

The design separates user intent, connection resolution, and server behavior.
The command layer captures the requested operation and explicit scope.
Connection and authentication resolution turn flags, environment values, and
the selected Context into one effective Endpoint, identity, and optional
Cluster. KubeSphere then serves its native APIs or proxies Kubernetes requests
to that Cluster.

Commands therefore do not need to understand KubeSphere proxy topology. A
command resolves its effective connection when it runs and reuses that result
throughout the invocation, so discovery, resource reads, supporting requests,
and error reporting use a consistent identity and scope.

## Core design

### Kubernetes command assembly

The root constructs one ksctl Kubernetes `RESTClientGetter` and one shared
kubectl Factory. Top-level `get` and `kube get` are separate instances of the
upstream get constructor attached to that Factory. `kube` assembles the
exported kubectl v0.36.2 operation constructors rather than wrapping the
top-level kubectl command:

```text
ksctl kube COMMAND
  -> upstream kubectl v0.36.2 command/options
  -> shared kubectl Factory
  -> ksctl Kubernetes RESTClientGetter
  -> discovery/RESTMapper/client as required
  -> KubeSphere Endpoint or /clusters/<cluster>
  -> Kubernetes API
```

The assembler tracks the upstream v0.36.2 operation list while deliberately
excluding `config`, `plugin`, `version`, `completion`, `options`, `kuberc`, and
empty `alpha`. This prevents a KubeSphere-authenticated command tree from
silently acquiring kubeconfig or kubectl CLI self-management behavior.

The `kube` parent owns persistent `--namespace` and `--request-timeout` flags.
The ksctl root owns `--cluster`, which is inherited by every `kube` operation
and scopes all of its remote Kubernetes requests.

Upgraded-transport commands such as `exec`, `attach`, `cp`, `port-forward`,
and `proxy` retain upstream SPDY, WebSocket, and HTTP Upgrade behavior through
the routed REST config. `top` is the only narrow behavior adapter: after the
upstream options complete, ksctl replaces their DiscoveryClient with the
shared Factory's DiscoveryClient so member-Cluster discovery fallback remains
consistent.

### Cross-Cluster resource access

A Context supplies the default Fleet, User, and optional Cluster. Explicit
connection or scope values override those defaults for one invocation. When a
Cluster is selected, Kubernetes discovery and resource requests use that
Cluster through KubeSphere; otherwise they use the Fleet Endpoint directly.

Discovery, reads, mutations, streaming, and metrics requests share the same
effective Cluster route. Namespace narrows namespaced resource requests but
does not select a Cluster. This keeps command syntax independent of routing
and prevents different supporting requests within one command from drifting
to different targets.

Some KubeSphere deployments expose individual discovery endpoints even when
aggregate discovery is incomplete. ksctl can recover an aggregate view from
the capabilities the server actually exposes, including registered extension
APIs, without maintaining a static public resource list.

Each resource command queries one selected Cluster. Cross-Cluster aggregation
is deliberately outside the command model.

### Tenant pipeline

Tenant resources use KubeSphere-native APIs rather than the generic Kubernetes
discovery pipeline. This makes their scope explicit even when similarly named
Kubernetes resources exist.

Workspace and tenant Cluster reads are Fleet-scoped and ignore the selected
resource Cluster. Namespace reads use the selected Cluster because Namespace
membership is Cluster-specific. An optional Workspace narrows Namespace and
tenant Cluster collections without changing which control plane owns the
request.

Human-readable output uses stable kubectl-style tables. Structured output
preserves the server response so clients do not lose fields that ksctl does not
yet understand.

### Extension management

Extension catalog and InstallPlan state belong to the host KubeSphere control
plane. Extension commands therefore do not inherit a Context's default
resource Cluster. Multicluster deployment is expressed separately as explicit
InstallPlan placement.

Placement records the eligible Cluster set selected for the operation instead
of leaving membership as a dynamic query. The KubeSphere controller owns
execution on the host and members, while ksctl owns input validation, accepted
intent, progress reporting, and actionable failure messages.

Install, upgrade, configure, and uninstall are purpose-built lifecycle writes,
not generic mutation verbs. Updates guard the accepted intent against
conflicting or stale state. Dependencies are validated without being installed
implicitly.

Lifecycle operations are asynchronous unless waiting is requested. Waiting
starts from the accepted operation rather than trusting an older terminal
status, tracks host and member targets independently, and treats deletion or
same-name recreation as a changed operation. Diagnosis summarizes controller,
dependency, workload, and member state without retrieving Secrets,
configuration values, or application logs.

### Raw API requests

The `api` command reuses normal KubeSphere connection, credential, TLS,
timeout, and user-agent resolution, but it does not add resource semantics.
The caller owns the server-relative path, method, and optional request body.

Selected Cluster scope is not added automatically. A caller that needs a
Cluster-scoped API must express that routing in the supplied path. This keeps
the raw transport predictable and avoids silently changing arbitrary paths.

Response bytes are passed through unchanged. HTTP error bodies remain visible
to the caller while the command still reports failure. ksctl does not type,
format, redact, or apply Extension lifecycle safeguards to raw API operations,
so the caller owns both mutation and disclosure risk.

### Authentication and configuration

A Fleet owns one KubeSphere Endpoint, its TLS settings, and Fleet-scoped
Users. A Context selects one Fleet and User and may provide a default Cluster.
This keeps credentials tied to their server while allowing multiple reusable
connection selections.

Explicit flags take precedence over environment values, which take precedence
over configured connection state. An explicit Endpoint must be paired with an
explicit Token and cannot borrow credentials from the selected Context.
Authoritative configured Token files and Tokens fail closed when unusable
instead of silently falling through to weaker credentials.

After explicit and configured Tokens, authentication can reuse a valid cached
Token or refresh an expired one. A configured Password is the final
command-local fallback and its successful response is not cached implicitly.
Unreadable or malformed cache data is reported rather than ignored.

Login is the explicit cache-creation workflow. It authenticates first, then
updates Config and the selected Fleet/User Token cache as one recoverable
operation. A persistence failure restores the previous cache state, and the
supplied Password is never stored. Logout makes a best-effort remote
revocation, always attempts to delete the selected local cache, and preserves
Config.

## Security and compatibility

Top-level `get` and tenant inspection do not mutate resources. The `kube`
namespace is a general Kubernetes administration surface: commands such as
apply, delete, drain, debug, and rollout may change the selected Cluster and
are authorized by Kubernetes RBAC for the resolved KubeSphere credential.
ksctl does not add confirmation or retry a member-Cluster failure against the
host Cluster.

Interactive Password input is not echoed, and login never persists the
supplied Password. Config and Token cache updates use restricted permissions
and atomic replacement. A Password manually placed in Config remains plaintext
and is the user's responsibility.

Raw configuration, generated kubeconfig, raw API responses, and container logs
may contain credentials or application secrets. ksctl does not inspect or
redact those outputs; the caller owns their destination and lifecycle.

Extension lifecycle writes retain their purpose-built validation and waiting
semantics. Raw API requests may mutate server state, and plugins run with the
user's privileges and inherited environment. Neither receives the lifecycle
safeguards of the Extension command surface.

ksctl supports KubeSphere 4.x. Its aligned Kubernetes dependencies must be
upgraded together because resource discovery, mapping, printing, streaming,
and kubectl-compatible command behavior evolve as one integration surface.
