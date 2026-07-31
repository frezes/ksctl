# ksctl Kube Command Suite Design

Date: 2026-07-30

## Summary

Add a `kube` command group that exposes kubectl v0.36.2's Kubernetes operation
commands through ksctl's KubeSphere authentication, Context selection, and
member-Cluster routing.

The existing top-level `get` command remains available for concise resource
inspection. Apart from the separately approved removal of the root
`--request-timeout` flag, its resource behavior does not change. `describe`,
`logs`, and `top` move under `kube` and are removed from the root immediately.
`kube get` provides the same resource behavior as the top-level `get`.

The shared command builder exposes the resulting tree through both supported
entry points:

```text
ksctl kube ...
unictl ks kube ...
```

There is no `kubectl ks` entry point.

## Goals

- Expose the complete kubectl v0.36.2 Kubernetes operation command set under
  `ksctl kube`.
- Reuse upstream kubectl command constructors rather than copying or forking
  kubectl operation implementations.
- Route every remote Kubernetes operation through ksctl's selected
  KubeSphere Endpoint, credential, and optional Cluster.
- Add `--cluster` as the deliberate ksctl extension to the familiar kubectl
  command behavior.
- Scope `--request-timeout` to the `kube` command group instead of exposing it
  at the ksctl root.
- Preserve the current top-level `get` command and add equivalent
  `ksctl kube get` behavior.
- Move `describe`, `logs`, and `top` under `kube` without compatibility
  aliases or hidden forwarding commands at the root.
- Keep help, examples, output streams, discovery fallback, and errors
  consistent across the `ksctl` and `unictl ks` entry points.

## Non-goals

- Do not expose kubectl's kubeconfig and CLI self-management commands:
  `config`, `plugin`, `version`, `completion`, `options`, or `kuberc`.
- Do not read, merge, or write `~/.kube/config`.
- Do not add kubectl connection flags such as `--kubeconfig`, `--server`, or
  kubeconfig Context selection.
- Do not adapt kubectl's `config` behavior to ksctl's Config schema.
- Do not dispatch kubectl executable plugins from inside the `kube` command
  group. The existing top-level `ksctl` plugin model remains separate.
- Do not call an externally installed kubectl or generate a temporary
  kubeconfig.
- Do not add confirmations, retries, transaction wrappers, or KubeSphere
  lifecycle semantics around generic Kubernetes mutations.
- Do not reimplement tests already owned by the pinned kubectl packages.

## Command Surface

The root command contains:

```text
ksctl
  get
  kube
  auth
  config
  api
  extension
  plugin
  tenant
  version
```

The exact ordering in root help remains a presentation concern. The `kube`
group contains these kubectl v0.36.2 operation commands:

### Basic commands

- `create`
- `expose`
- `run`
- `set`
- `explain`
- `get`
- `edit`
- `delete`

### Deployment commands

- `rollout`
- `scale`
- `autoscale`

### Cluster management commands

- `certificate`
- `cluster-info`
- `top`
- `cordon`
- `uncordon`
- `drain`
- `taint`

### Troubleshooting and debugging commands

- `describe`
- `logs`
- `attach`
- `exec`
- `port-forward`
- `proxy`
- `cp`
- `auth`
- `debug`
- `events`

### Advanced commands

- `diff`
- `apply`
- `patch`
- `replace`
- `wait`
- `kustomize`

### Settings commands

- `label`
- `annotate`

### Discovery commands

- `api-versions`
- `api-resources`

The pinned kubectl version has no available `alpha` children, so `kube alpha`
is not registered.

The following transitions are intentionally breaking:

```text
ksctl describe ...  -> ksctl kube describe ...
ksctl logs ...      -> ksctl kube logs ...
ksctl top ...       -> ksctl kube top ...
```

The removed root paths return Cobra's normal unknown-command error. They do
not emit deprecation warnings and do not execute hidden forwarding commands.

## Command Assembly

Create a focused `pkg/cmd/kube.go` command assembler. It accepts:

- the active entry point display name;
- the shared kubectl `cmdutil.Factory`;
- the active generic IOStreams; and
- the pointers used by ksctl for Namespace and request-timeout selection.

The assembler constructs a `kube` parent and adds each upstream operation
through its exported `NewCmd...` constructor. It does not call
`kubectl.NewKubectlCommand`, because that constructor creates its own Factory
from kubeconfig-oriented `ConfigFlags` and therefore bypasses ksctl's
authentication and Cluster routing.

The root continues to build one KubeSphere-backed Kubernetes
`RESTClientGetter` and one kubectl Factory. That Factory is shared by the
root-level `get` command and all `kube` children:

```text
Cobra command
  -> upstream kubectl command/options
  -> shared kubectl Factory
  -> ksctl Kubernetes RESTClientGetter
  -> KubeSphere Endpoint or /clusters/<cluster>
  -> Kubernetes API
```

Top-level `get` and `kube get` are separate Cobra command instances created
from the same upstream constructor. Both delegate their resource parsing,
discovery, mapping, selection, watching, and printing behavior to kubectl.

The `kube` parent defines persistent `--namespace`/`-n` and
`--request-timeout` flags bound to the existing connection options. Its
children inherit those flags. Top-level `get` retains its command-local
Namespace flag so its current syntax and scope do not change, but it no
longer exposes `--request-timeout`.

Upstream examples are rewritten recursively from `kubectl ...` to the active
display path:

```text
ksctl kube ...
unictl ks kube ...
```

The rewrite applies to nested commands such as `rollout`, `set`, `create`,
`auth`, and `top`.

## Connection and Scope Semantics

The root retains ksctl's existing connection flags:

- `--endpoint`
- `--token`
- `--context`
- `--cluster`
- `--v`

`--request-timeout` moves from the root to the `kube` parent's persistent
flags. It limits one Kubernetes server request for any remote `kube` child,
and `0` retains the current unlimited/default behavior. Top-level KubeSphere
commands and top-level `get` no longer accept this flag.

For `kube` commands, `--context` selects a ksctl Context rather than a
kubeconfig Context. An explicit `--cluster` overrides the selected Context's
`defaultCluster`. With no effective Cluster, requests target the Fleet
Endpoint. With a Cluster, the Kubernetes REST Host is:

```text
<KubeSphere Endpoint>/clusters/<cluster>
```

The root-level persistent `--cluster` flag is inherited by the `kube` parent
and all of its children. It may be written after a nested command, for
example:

```text
ksctl kube get pods --cluster member-1
ksctl kube apply -f app.yaml --cluster member-1 --request-timeout 30s
ksctl kube exec -it pod/web --cluster member-1 -- sh
```

Purely local commands such as `kustomize` inherit `--cluster` but do not use
the connection.

Every command keeps the command-specific flags supplied by its upstream
constructor, including behavior such as server-side apply, interactive exec,
drain options, dry-run, wait, and output formatting.

## Top Discovery Adapter

Retain the current minimal `top pod` and `top node` assembly adapter.

Upstream kubectl's `TopPodOptions.Complete` and `TopNodeOptions.Complete`
obtain discovery from `Factory.KubernetesClientSet().DiscoveryClient`. That
path is created from `ToRESTConfig` and bypasses ksctl's
`RESTClientGetter.ToDiscoveryClient`.

KubeSphere member-Cluster routing may return an HTML response or incomplete
data from the aggregate `/api` or `/apis` discovery paths while individual
group-version paths remain accessible. ksctl's discovery getter handles that
case by probing core resources, CRDs, and APIService group versions. `top`
needs this discovery result to determine whether `metrics.k8s.io/v1beta1` is
available.

The adapter therefore:

1. constructs upstream Top options and commands;
2. runs upstream `Complete`;
3. replaces only the options' DiscoveryClient with
   `Factory.ToDiscoveryClient`;
4. runs upstream validation and execution.

Metrics clients, Core clients, calculations, errors, and printers remain
upstream implementations. The adapter can be removed in a future change only
when every supported KubeSphere Cluster route guarantees standard aggregate
discovery responses.

## Mutation and Security Boundary

The `kube` command group is a general Kubernetes management surface and is
not read-only.

Commands such as `apply`, `create`, `edit`, `delete`, `patch`, `replace`,
`rollout`, `scale`, `autoscale`, `cordon`, `drain`, `taint`, `label`,
`annotate`, `set`, and `debug` may change the selected Cluster. ksctl does not
add confirmation prompts or Extension lifecycle guards. Kubernetes RBAC
applied to the resolved KubeSphere credential authorizes or rejects each
operation.

The user-facing boundaries become:

- top-level `get` and tenant inspection are read-only;
- `kube` exposes generic Kubernetes reads and mutations;
- `extension` owns purpose-built KubeSphere Extension lifecycle workflows;
- `api` remains a lower-level authenticated request escape hatch; and
- executable plugins remain independent programs running under the user's
  authority.

Documentation must not describe every built-in resource command as read-only
after this change.

## Streaming, Upgrade, and Local Proxy Commands

`exec`, `attach`, `cp`, `port-forward`, and `proxy` receive the same resolved
REST configuration as other `kube` commands. Their SPDY, WebSocket, HTTP
Upgrade, streaming, and local proxy behavior remains owned by upstream
kubectl and client-go.

ksctl does not bypass KubeSphere for these operations. If a KubeSphere proxy
or selected Cluster route cannot carry the required upgraded connection, the
command reports the resulting upstream/client-go error. It does not retry
against the host Cluster and does not invoke external kubectl.

`cp` retains kubectl's dependency on `tar` in the target container.

## Errors and Output

Commands use the IOStreams injected by the shared root builder. Upstream
argument validation, usage messages, server Status errors, partial-result
behavior, and exit handling are retained wherever the command constructors
provide them.

Remote failures remain scoped to the selected effective Cluster. No command
silently retries a failed member-Cluster request against the Fleet Endpoint.

The `kube` parent shows help when run without a child. Unsupported excluded
commands return the normal unknown-command error.

## Dependencies

Continue pinning Kubernetes modules together at v0.36.2. Importing the
complete operation command set activates additional transitive dependencies
for apply, remote command streaming, port forwarding, debug, and kustomize.
`go mod tidy` records the required module graph; dependencies are not added
manually unless tidy cannot express the required direct import.

Every intentional Kubernetes dependency upgrade must compare the local
`kube` command inventory with the upgraded upstream `pkg/cmd/cmd.go`, because
ksctl owns the assembly list while kubectl owns the command implementations.

## Testing

### Command tree

Tests prove that:

- root `get` remains registered;
- root `describe`, `logs`, and `top` are absent;
- every listed Kubernetes operation is registered under `kube`;
- excluded self-management commands are absent;
- `kube alpha` is absent while the pinned upstream command has no children;
- `kube get` and root `get` expose representative get flags;
- the root does not expose `--request-timeout`;
- `kube --namespace` is inherited by namespaced children;
- `kube --request-timeout` is inherited by remote children; and
- root and nested help contain the active `ksctl` or `unictl ks` display path,
  not `kubectl ks`.

### Behavior

Integration-style HTTP tests prove that:

- root `get` and `kube get` produce the same request path, authentication, and
  output for the same inputs;
- existing describe, logs, and top behavior works through their new `kube`
  paths;
- explicit and Context-default Cluster routing remains effective;
- a representative mutation sends its write request only to the selected
  `/clusters/<cluster>` path; and
- `top` retains its member-Cluster discovery fallback.

Tests for the command assembly verify representative flags across the major
command groups without copying kubectl's own exhaustive command tests.

The existing REST configuration tests continue to prove the Host used by
streaming and upgrade-capable commands. Real KubeSphere integration testing
must cover `exec`, `attach`, `cp`, and `port-forward` against both host and
member Clusters; unit tests do not emulate kubectl's internal SPDY or
WebSocket implementations.

### Verification

The implementation is complete only after:

- focused `pkg/cmd` tests pass;
- `go mod tidy -diff` reports no changes;
- `make verify` passes;
- both `cmd/ksctl` and `cmd/unictl-ks` build;
- `kube --help`, `kube get --help`, and `kube apply --help` smoke tests pass
  through both display names; and
- generated binaries are not committed.

## Documentation and Release Notes

Update:

- `README.md`
- `docs/cli.md`
- `docs/cli_zh.md`
- `docs/design.md`
- `docs/design_zh.md`
- `CHANGELOG.md` under `[Unreleased]` only

The documentation:

- introduces `kube` as the full Kubernetes operation namespace;
- keeps root `get` examples where concise inspection is intended;
- changes describe, logs, and top examples to `ksctl kube ...`;
- documents the complete operation command categories;
- explains `--cluster` as the ksctl extension to kubectl behavior;
- documents `--request-timeout` only in the `kube` scope;
- distinguishes `ksctl` from the `unictl ks` release companion;
- removes stale claims that the complete generic resource surface is
  read-only; and
- calls out the immediate removal of root describe, logs, and top as a
  breaking change.

The implementation must not edit the already published `[0.2.0]` changelog
entry. The breaking-change notice remains under `[Unreleased]` until the next
release moves it into that new version. The same notice belongs in the next
release notes and migration documentation; removed commands do not gain
runtime forwarding or compatibility warnings.
