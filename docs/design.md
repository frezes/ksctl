# ksctl Design

**English** | [简体中文](design_zh.md)

This document explains the core design of ksctl for developers. For command
syntax and workflows, see the [CLI guide](cli.md).

## Goals and boundaries

ksctl provides one command-line interface for KubeSphere 4.x and nearly the
full kubectl operation surface through KubeSphere. It preserves upstream
kubectl behavior while keeping connection, authentication, and scope selection
consistent across commands.

The command surface has four distinct safety boundaries:

- top-level `get` and tenant inspection are read-only;
- general Kubernetes operations run under `kube` with KubeSphere
  authentication and Cluster routing;
- Extension lifecycle commands provide purpose-built, controlled writes; and
- `api` is a raw authenticated escape hatch whose caller-selected requests may
  mutate server state.

Executable plugins run as independent programs under the user's authority and
are outside these built-in safeguards.

ksctl does not use kubeconfig as its persistent configuration model, aggregate
resources across Clusters, audit or sandbox plugins, or support KubeSphere 3.x.

## Architecture overview

The design separates user intent, connection resolution, and server behavior.
The command layer captures the requested operation and explicit scope.
Connection and authentication resolution turn flags, environment values, and
the selected Context into one effective Endpoint, identity, and optional
Cluster. KubeSphere then serves its native APIs or proxies Kubernetes requests
to that Cluster.

Commands therefore do not need to understand KubeSphere proxy topology. A
command resolves its effective connection when it runs and reuses that result
throughout the invocation, so Kubernetes operations, supporting requests, and
error reporting use a consistent identity and scope.

## Core design

### Cross-Cluster resource access

A Context supplies the default Fleet, User, and optional Cluster. Explicit
connection or scope values override those defaults for one invocation. When a
Cluster is selected, Kubernetes discovery and resource requests use that
Cluster through KubeSphere; otherwise they use the Fleet Endpoint directly.

Kubernetes operations and their supporting requests share the same effective
Cluster route. Namespace narrows namespaced resource requests but does not
select a Cluster. This keeps command syntax independent of routing and
prevents different requests within one operation from drifting to different
targets.

Some KubeSphere deployments expose individual discovery endpoints even when
aggregate discovery is incomplete. ksctl can recover an aggregate view from
the capabilities the server actually exposes, including registered extension
APIs, without maintaining a static public resource list.

Each Kubernetes operation queries one selected Cluster. Cross-Cluster
aggregation is deliberately outside the command model.

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
command may change the selected Cluster and is authorized by Kubernetes RBAC
for the resolved KubeSphere credential. It does not use local kubeconfig or
retry a failed member-Cluster operation against the host Cluster.

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

ksctl supports KubeSphere 4.x and tracks upstream Kubernetes compatibility as
one integration surface.
