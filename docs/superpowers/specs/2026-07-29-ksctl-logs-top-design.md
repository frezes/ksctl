# ksctl Logs and Top Design

## Summary

Add kubectl-compatible `logs` and `top` commands to both the standalone
`ksctl` entrypoint and the `kubectl ks` plugin entrypoint.

The commands remain Kubernetes-native:

- `logs` reads the Pod log subresource;
- `top pod` and `top node` read `metrics.k8s.io/v1beta1`; and
- both commands use ksctl's existing Endpoint, credential, Context, Cluster,
  Namespace, TLS, and request-timeout resolution.

This change does not integrate KubeSphere logging or monitoring extensions,
aggregate results across Clusters, or introduce a new logs or metrics client
abstraction.

## Goals

- Preserve the command syntax, flags, validation, output, and errors of the
  pinned kubectl v0.36.2 implementation.
- Support the same KubeSphere Endpoint and member-Cluster proxy routing as
  `get` and `describe`.
- Keep `ksctl` and `kubectl ks` behavior identical apart from their displayed
  command names.
- Reuse ksctl's KubeSphere discovery compatibility path for `top`.
- Keep the new ksctl-owned surface limited to command assembly and a deletable
  discovery adapter.

## Non-goals

- KubeSphere logging, monitoring, Prometheus, or historical-query APIs
- Cross-Cluster log streaming or metrics aggregation
- Automatic fallback to a KubeSphere observability extension
- Log persistence, search, reconnection, or resume support
- A new output format, error vocabulary, or configuration field
- Reimplementation of kubectl workload-to-Pod resolution, concurrent log
  consumption, resource calculations, sorting, or printing
- Changes to `staging/` or to the public `pkg/client/kubernetes` interfaces

## Approaches Considered

### 1. Reuse kubectl commands with one narrow `top` adapter (selected)

Register kubectl's logs command directly. Construct kubectl's top command and
subcommands from their exported command and option types, replacing only the
top subcommand execution sequence so the completed options use
`Factory.ToDiscoveryClient`.

This retains upstream CLI behavior while preserving the discovery
compatibility already required by KubeSphere deployments.

### 2. Wrap all kubectl option types in ksctl commands

Create ksctl-owned Cobra definitions for logs, top pod, and top node, then call
the exported kubectl option methods.

This would allow more customization, but it would duplicate upstream flags,
help, completion, and orchestration and create an unnecessary compatibility
surface.

### 3. Implement the APIs directly

Call the Pod log and Metrics APIs from ksctl-owned clients.

This would duplicate substantial kubectl behavior: workload resolution,
container selection, multi-Pod concurrency, streaming, resource-unit
calculation, capacity percentages, filtering, sorting, and printing. It is
inconsistent with the existing `get` and `describe` architecture.

## Architecture

The existing resource-command spine remains the only request path:

```text
Cobra command
  -> kubectl command and options
  -> shared cmdutil.Factory
  -> ksctl Kubernetes RESTClientGetter
  -> KubeSphere Endpoint or /clusters/{cluster} proxy
  -> Kubernetes logs, metrics, and core APIs
```

No new public interface, client package, state owner, or persistent
configuration is added.

### Resource command assembly

Add `pkg/cmd/resource_commands.go` to own assembly of the native Kubernetes
resource commands:

- `get`
- `describe`
- `logs`
- `top`

`pkg/cmd/root.go` creates the shared `cmdutil.Factory` and IO streams, then adds
the commands returned by this assembly code. This keeps the root responsible
for dependency construction while the resource-command file owns kubectl
command adaptation.

A private recursive helper replaces `kubectl ` in every command's `Example`
field with the active root display name. It runs over the complete command
subtree so `top pod` and `top node` examples are correct for both entrypoints:

```text
ksctl top pod
kubectl ks top pod
```

The helper affects examples only. Cobra continues to derive usage lines from
the parent command and its display-name annotation.

### Logs command

Create the command with:

```text
logs.NewCmdLogs(factory, streams)
```

No ksctl wrapper or logs options type is added. The command uses the shared
Factory for namespace loading, REST mapping, resource building, REST config,
and Pod log requests.

Consequently, the pinned kubectl implementation continues to own:

- Pod and `TYPE/NAME` arguments;
- Deployment, Job, and other workload-to-Pod resolution;
- container, init-container, and ephemeral-container selection;
- label selection;
- previous logs, timestamps, tail, byte limits, and time filters;
- single- and multi-Pod/container output;
- follow mode, prefixes, concurrency limits, and interruption; and
- validation, completion, output, and errors.

### Top command

Kubectl's top options construct their Metrics and Core clients from the shared
Factory, but kubectl v0.36.2 obtains discovery from the newly created
Kubernetes Clientset rather than from `Factory.ToDiscoveryClient`. That raw
DiscoveryClient bypasses ksctl's fallback when a KubeSphere Endpoint cannot
serve the `/api` or `/apis` discovery roots.

The private top adapter corrects only that seam:

1. Build the parent command with `top.NewCmdTop` so upstream help and parent
   behavior remain authoritative.
2. Replace its upstream `pod` and `node` children with children built by
   `top.NewCmdTopPod` and `top.NewCmdTopNode` using explicit exported options.
   Preserve the upstream default `UseProtocolBuffers: true`.
3. Replace each child's execution callback with the same upstream sequence:
   `Complete`, `Validate`, and `RunTopPod` or `RunTopNode`.
4. Between `Complete` and `Validate`, obtain
   `factory.ToDiscoveryClient()` and assign it to the options'
   `DiscoveryClient`.
5. Use kubectl's normal error handler for every step.

All flags, aliases, completion, clients, printers, validation, and execution
remain upstream-owned. The adapter is an internal assembly detail tied to the
pinned kubectl version. If a future kubectl top command uses
`Factory.ToDiscoveryClient` itself, the adapter can be removed.

## Command Contract

### `logs`

The public command is kubectl v0.36.2's:

```text
ksctl logs [-f] [-p] (POD | TYPE/NAME) [-c CONTAINER]
```

Representative supported behavior includes:

- snapshot and followed logs;
- current or previous containers;
- a named container or all containers;
- one Pod, a workload, all workload Pods, or a label-selected Pod set;
- prefixing multi-source output;
- tail, byte, timestamp, and time-window limits; and
- kubectl's maximum concurrent follow requests.

Namespace selection comes from the root `--namespace` flag. When it is absent,
the in-memory Kubernetes client config uses the standard `default` Namespace;
ksctl Contexts do not persist a Namespace. Logs does not add cross-Namespace
or cross-Cluster aggregation beyond what the upstream command supports.

### `top`

The public subcommands are:

```text
ksctl top pod [NAME | -l label]
ksctl top node [NAME | -l label]
```

Upstream aliases remain available:

- `pod`, `pods`, and `po`;
- `node`, `nodes`, and `no`.

`top pod` uses the selected Namespace and supports `--all-namespaces`.
`top node` is Cluster-scoped and ignores Namespace. Upstream filtering,
sorting, container detail, sum, capacity/allocatable selection, header, swap,
and protobuf flags remain unchanged.

Top reports current Metrics Server data intended for Kubernetes autoscaling.
It is not a historical or high-accuracy monitoring interface.

### Built-in command ownership

Registering `logs` and `top` makes those names built-in command paths. Plugin
dispatch cannot override them, and plugin listing reports colliding
`ksctl-logs` or `ksctl-top` executables as built-in conflicts using the
existing conflict mechanism.

Future observability enhancements should use a distinct plugin command path
rather than widening the built-in `logs` or `top` contract.

## Request Routing

All connection state is resolved lazily after Cobra parses the root flags.
Existing precedence and validation remain unchanged:

- Endpoint and credentials come from explicit flags, environment variables,
  or the selected Context;
- an explicit `--cluster` overrides the Context's `defaultCluster`;
- Cluster path segments are validated before token resolution; and
- an explicit `--namespace` overrides the in-memory client's standard
  `default` Namespace.

Without a selected Cluster, a Pod log request is:

```text
/api/v1/namespaces/{namespace}/pods/{pod}/log
```

With a selected Cluster, the same request is:

```text
/clusters/{cluster}/api/v1/namespaces/{namespace}/pods/{pod}/log
```

Top requests use the same REST-config host:

```text
/apis/metrics.k8s.io/v1beta1/namespaces/{namespace}/pods
/apis/metrics.k8s.io/v1beta1/nodes
/api/v1/nodes
```

or, for a selected Cluster:

```text
/clusters/{cluster}/apis/metrics.k8s.io/v1beta1/namespaces/{namespace}/pods
/clusters/{cluster}/apis/metrics.k8s.io/v1beta1/nodes
/clusters/{cluster}/api/v1/nodes
```

Top uses ksctl's cached discovery client. When ordinary discovery succeeds,
behavior is unchanged. When the discovery roots fail, the existing fallback
may recover `metrics.k8s.io/v1beta1` from APIService registration and its
concrete group/version endpoint. Resource requests remain Cluster-scoped even
when an existing compatibility path uses unscoped core-v1 discovery metadata.

## Timeouts, Streaming, and Cancellation

The root `--request-timeout` remains the only request-timeout setting.

- Its default value `0` imposes no client timeout on a followed log stream.
- An explicitly configured nonzero value applies to logs and top requests in
  the same way as other Kubernetes requests.
- Logs passes the Cobra Context into kubectl's streaming implementation, so
  user cancellation terminates active streams.
- ksctl does not add automatic reconnect, resume, or retry behavior.

For multi-source logs, some output may already have reached stdout when a
later request fails. ksctl does not buffer or roll back streamed output.

## Errors and Negative Paths

### Logs

Kubectl remains authoritative for errors involving:

- missing Pods or workloads;
- workloads that resolve to no Pods;
- missing or ambiguous containers;
- invalid selectors or time filters;
- backend TLS verification;
- concurrent log request failures; and
- follow-mode interruption or stream failures.

The `--ignore-errors` flag retains its upstream meaning. ksctl does not convert
these failures into KubeSphere-specific errors.

### Top

- If discovery advertises no supported `metrics.k8s.io` API, return kubectl's
  `Metrics API not available`.
- If the API exists but has not produced metrics, retain kubectl's
  `metrics not available yet` or empty-resource behavior.
- Authentication, authorization, NotFound, transport, decoding, and server
  failures retain their Kubernetes errors.
- Missing metrics are never converted to zero-valued usage.
- No KubeSphere monitoring API is queried as a fallback.

Connection-resolution and request-timeout parse failures occur before a
resource request. Both commands are read-only and create no state requiring
idempotency, compensation, reconciliation, or replay handling.

Successful output and error text are deliberately inherited from the pinned
kubectl release. ksctl does not create a second compatibility promise for
their exact formatting.

## Testing

Tests cover ksctl's integration seams rather than duplicating upstream
kubectl unit tests.

### Command assembly and help

- Both entrypoints register `logs`, `top pod`, and `top node`.
- Representative upstream flags are present:
  - `logs --follow`;
  - `top pod --containers`; and
  - `top node --show-capacity`.
- Usage and recursive examples use `ksctl` for the standalone entrypoint.
- Usage and recursive examples use `kubectl ks` for the plugin entrypoint.
- Existing `get` and `describe` help remains correct.

### Logs integration

- A snapshot Pod log response is copied to stdout.
- Namespace, container, tail, and other representative options reach the Pod
  log subresource query.
- An explicit Cluster and a Context default Cluster both add the expected
  `/clusters/{cluster}` prefix.
- Workload-to-Pod resolution works when the KubeSphere discovery roots require
  the existing fallback.
- A streaming response can emit data and terminates when the command Context
  is cancelled.
- Authorization and missing-resource failures are returned.

### Top integration

- `top pod` reads PodMetrics and prints the expected CPU/memory table.
- `top node` reads NodeMetrics plus Node allocatable or capacity values.
- Namespace, all-Namespaces, selectors, and Cluster routing reach the expected
  endpoints.
- When aggregate discovery roots fail, APIService-based discovery of the
  concrete Metrics API still permits top to run.
- Missing Metrics API, forbidden requests, and unavailable metrics retain
  upstream failures.

Tests may disable protocol buffers where JSON fixtures make the routing seam
clearer. The default value and flag wiring remain covered through command
construction, while kubectl owns protocol encoding tests.

### Regression and verification

- Existing `get`, `describe`, authentication, and discovery tests pass.
- Plugin conflict tests cover the new built-in names.
- Focused package tests pass.
- The full normal and race test suites pass.
- Both binaries build.
- Formatting, module, vet, and `git diff --check` verification passes.

## Documentation

Update:

- `README.md` feature and quick-start examples;
- `docs/cli.md` overview, command syntax, command table, scope examples, logs
  usage, top usage, and Metrics Server prerequisite; and
- `docs/design.md` goals, command architecture, resource-command pipeline,
  discovery compatibility, routing, and read-only command surface.

Documentation will state that logs and top are kubectl-compatible Kubernetes
commands, not KubeSphere observability-extension clients.

## Acceptance Criteria

- `ksctl logs` and `kubectl ks logs` expose kubectl v0.36.2 logs behavior
  through the configured KubeSphere Endpoint.
- `ksctl top pod/node` and `kubectl ks top pod/node` expose kubectl v0.36.2 top
  behavior through the configured KubeSphere Endpoint.
- Namespace, explicit Cluster, and Context default Cluster routing are
  preserved.
- Top uses ksctl's existing discovery fallback instead of bypassing it.
- No new KubeSphere observability API, configuration model, public client
  interface, or cross-Cluster aggregation is introduced.
- Help and examples use the active entrypoint name.
- Documentation describes the public contract and limitations.
- Repository verification passes.
