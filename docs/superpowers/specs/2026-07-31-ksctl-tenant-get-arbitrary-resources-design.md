# ksctl Tenant Get Arbitrary Resources Design

Date: 2026-07-31

## Summary

Extend `tenant get` so it can query any Kubernetes resource discoverable by
kubectl while preserving the existing native KubeSphere tenant resources.
Cluster administrators retain kubectl's direct all-Namespaces request.
When a tenant user's direct all-Namespaces request is forbidden, ksctl resolves
the Namespaces visible to that user through the KubeSphere tenant API, queries
the resource in each Namespace, and merges the successful responses before
kubectl prints them.

The same aggregation is available explicitly through `--workspace WORKSPACE`.
That form resolves only the Namespaces visible in the selected Workspace and
never attempts a Cluster-wide resource request.

## Goals

- Preserve the existing `tenant get workspace|namespace|cluster` commands,
  aliases, KubeSphere API paths, flags, and output.
- Support every other Kubernetes resource type accepted by the pinned kubectl
  v0.36.2 `get` command, including discovered CRDs and qualified group/version
  forms.
- Preserve kubectl resource discovery, argument parsing, selectors, sorting,
  server printing, and output formats.
- Keep Cluster administrators on the native single-request `-A` path.
- Allow tenant users to query namespaced resources across all of their
  accessible Namespaces without requiring permission to list Kubernetes
  Namespace objects.
- Allow a caller to restrict an arbitrary namespaced resource query to the
  accessible Namespaces in one KubeSphere Workspace.
- Produce one deterministic kubectl-style result and never expose a partial
  aggregate when any Namespace query fails.
- Reuse the existing Endpoint, Context, Cluster, credential, TLS, user-agent,
  timeout, and cancellation behavior.

## Non-Goals

- Do not change top-level `ksctl get` or `ksctl kube get`.
- Do not change the three existing KubeSphere tenant resource implementations
  or route them through Kubernetes discovery.
- Do not modify the pinned KubeSphere or kubectl source under `staging/`.
- Do not add create, update, patch, delete, describe, logs, exec, or other
  multi-Namespace tenant operations.
- Do not implement a server-side `namespace-a|namespace-b` convention.
  Kubernetes treats that value as one Namespace; it is not a supported
  multi-Namespace syntax.
- Do not implement client-side multi-Namespace watch multiplexing.
- Do not search multiple Namespaces for a resource requested by name.
- Do not define Workspace scope for raw URLs or manifest filenames.

## CLI Behavior

The existing native forms remain:

```text
ksctl tenant get workspace [NAME] [-o table|json|yaml]
ksctl tenant get namespace [--workspace WORKSPACE] [-o table|json|yaml]
ksctl tenant get cluster [--workspace WORKSPACE] [-o table|json|yaml]
```

Their existing aliases continue to take precedence:

- `workspace`, `workspaces`;
- `namespace`, `namespaces`, `ns`; and
- `cluster`, `clusters`.

All other resource arguments use kubectl `get` syntax:

```text
ksctl tenant get pods
ksctl tenant get pods -n demo
ksctl tenant get pods -A
ksctl tenant get deployments.apps --workspace platform
ksctl tenant get widgets.example.io -A -o yaml
```

The generic command exposes the flags provided by kubectl v0.36.2 `get`, plus
an explicit kubectl-compatible Namespace flag and the ksctl-specific
long-form flag:

```text
-n, --namespace NAMESPACE
--workspace WORKSPACE
```

`--workspace` is mutually exclusive with `-n`/`--namespace` and
`-A`/`--all-namespaces`. It is valid only for namespaced collection queries.
It is rejected for:

- Cluster-scoped resources;
- named cross-Namespace resource queries;
- `--raw`; and
- `-f`/`--filename`.

Without `-A` or `--workspace`, kubectl retains its normal current/default or
explicit Namespace behavior.

Cluster-scoped resources retain normal kubectl behavior. `-A` has no
aggregation effect on them. `--workspace` with a Cluster-scoped resource is an
error because a Workspace is a collection of Namespaces, not a scope for
Cluster-scoped Kubernetes objects.

## Command Composition

The `tenant get` parent becomes a kubectl-derived generic get command and keeps
the three existing native commands as explicit Cobra children. Cobra resolves
the known child names before running the parent:

```text
tenant get workspace     -> existing KubeSphere tenant client
tenant get ns            -> existing KubeSphere tenant client
tenant get cluster       -> existing KubeSphere tenant client
tenant get pods          -> kubectl get engine
tenant get a-crd         -> kubectl get engine
```

The generic command uses `get.NewGetOptions` and its existing `Complete`,
`Validate`, and `Run` methods. ksctl owns only the small Cobra constructor
needed to:

- register the upstream option flags;
- register `-n`/`--namespace` against the shared connection Namespace option,
  because `tenant get` does not inherit it from a kubectl-style parent;
- add `--workspace`;
- validate ksctl scope combinations;
- select direct or aggregation-aware execution; and
- return errors through Cobra rather than copying kubectl's get engine.

This retains kubectl's implementation for discovery, resource builders,
selectors, sorting, printing, templates, and output formats.

`pkg/cmd/root.go` supplies both existing connection getters, the shared
connection Namespace option, and the kubectl factory inputs to the tenant
command. Both supported executable entrypoints continue to build the same
root command tree.

## Components

### Tenant Generic Get Command

The generic command owns CLI-only decisions:

- distinguishing an existing native tenant child from an arbitrary resource;
- registering and validating `--workspace`;
- rejecting incompatible `--workspace`, Namespace, all-Namespaces, raw,
  filename, named-resource, and watch combinations;
- configuring the aggregation-aware Kubernetes REST client getter;
- performing the watch permission preflight described below; and
- buffering output when aggregation is used so an aggregate failure cannot
  expose partial data.

The command does not decode or print Kubernetes resources itself.

### Aggregation-Aware RESTClientGetter

A focused wrapper around the existing Kubernetes `RESTClientGetter` delegates:

- `ToDiscoveryClient`;
- `ToRESTMapper`; and
- `ToRawKubeConfigLoader`.

Its `ToRESTConfig` returns a copy of the normal Kubernetes REST configuration
with an aggregation-aware `http.RoundTripper`. The wrapper receives a narrow
tenant Namespace resolver backed by the existing KubeSphere connection.

The original getter remains the source of truth for resource discovery and
REST mappings. The transport consults that mapper to classify each requested
group/version/resource as namespaced or root-scoped; it does not infer scope
solely from URL shape.

### Tenant Namespace Resolver

The resolver calls the existing KubeSphere tenant v1beta1 endpoints:

```text
/kapis/tenant.kubesphere.io/v1beta1/namespaces
/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/namespaces
```

The request follows the effective KubeSphere Cluster selected by the existing
connection rules. It validates and extracts `metadata.name` from every item,
rejects invalid Kubernetes Namespace names, removes duplicates, and preserves
the first server-provided occurrence.

Resolved Namespace lists are cached for one command execution, separately for
the all-accessible and Workspace-specific scopes. No cache survives the
command.

### Aggregating RoundTripper

The transport delegates discovery, direct Namespace queries, root-scoped
resources, non-GET requests, and unrelated requests unchanged.

For an eligible namespaced collection GET, it can:

1. send the original request unchanged;
2. use a forbidden result as the signal to resolve tenant Namespaces;
3. clone the original request once per Namespace;
4. follow each Namespace's pagination internally;
5. merge homogeneous list or Table responses; and
6. return one synthetic successful HTTP response to kubectl.

The transport never prints output or changes kubectl flags.

## Request Flows

### Current or Explicit Namespace

`tenant get RESOURCE` and `tenant get RESOURCE -n NAMESPACE` are direct
kubectl requests. The aggregation transport delegates them unchanged.

### Cluster Administrator `-A`

For a namespaced resource requested with `-A`, the transport first sends the
original all-Namespaces request.

If it succeeds, its response is returned without tenant API lookup or
fan-out. This preserves native kubectl performance, pagination, resource
version, Table behavior, and watch behavior for Cluster administrators.

Only HTTP `403 Forbidden` activates tenant fallback. Authentication failures,
rate limits, not-found responses, timeouts, cancellations, and server errors
are returned unchanged.

### Tenant User `-A`

After the all-Namespaces request returns `403 Forbidden`, the transport
discards that response body, resolves the current user's accessible
Namespaces through the tenant API, and performs one namespaced collection
query per Namespace.

The original `403` is an internal fallback signal and is not included in the
successful result. If fallback fails, the final error describes that failure
rather than presenting the original global denial as the only cause.

### Workspace

For `--workspace WORKSPACE`, the command configures aggregation before kubectl
runs. The transport does not attempt a global request. It resolves
`/workspaces/{workspace}/namespaces` and fans out only to that list.

An administrator using `--workspace` receives the same restricted result as a
tenant user. Administrator permission never expands the explicit Workspace
scope.

### Root-Scoped Resources

The RESTMapper classifies Cluster-scoped resources before aggregation. Such
requests always remain direct. If `--workspace` is present, the command
returns a scope error before issuing the resource request.

### Watch

`--workspace` with `--watch` or `--watch-only` is rejected before any request.

For `-A --watch` or `-A --watch-only`, the command uses the KubeSphere
v1beta1 self-subject access review endpoint to check cluster-scope
`list namespaces` permission before kubectl produces an initial list:

```text
/kapis/iam.kubesphere.io/v1beta1/selfsubjectaccessreviews
```

If allowed, kubectl's native all-Namespaces watch proceeds. If not allowed,
the command reports that tenant multi-Namespace watch is unsupported. This
preflight prevents a tenant list fallback from printing initial objects before
a later watch request fails.

## Fan-Out and Pagination

Namespace queries run with at most eight concurrent requests. The command
context cancels outstanding work after the first failure. All started
requests are joined before returning so bodies and connections are closed.

Each cloned request preserves:

- authorization and impersonation headers;
- user agent;
- TLS transport;
- timeout and cancellation;
- label and field selectors;
- subresource path;
- server-print content negotiation; and
- other query parameters that are independent for each Namespace.

Per-Namespace pagination is consumed inside the transport. Each Namespace's
`continue` token is sent only back to that same Namespace. The final aggregate
does not expose one Namespace's token as a token for the combined result.

Results are stored by Namespace index and merged in tenant API Namespace
order, regardless of request completion order. Items retain their
server-provided order within each Namespace.

## Response Merging

The transport accepts only successful JSON Kubernetes list responses.
Responses must be homogeneous for one request.

### Kubernetes Lists

For ordinary list responses:

- `apiVersion` and `kind` must match;
- every response must be a Kubernetes list;
- `items` are appended in deterministic Namespace order;
- list metadata other than `continue` and `resourceVersion` is retained only
  when it is valid for the aggregate; and
- aggregate `continue` and `resourceVersion` are empty.

There is no correct single resource version or continuation token for several
independently listed Namespace collections.

### Table Responses

For server-print responses:

- every response must be a `meta.k8s.io` Table;
- column definitions must be identical;
- rows are appended in deterministic Namespace order; and
- the row objects remain present when requested so kubectl can render the
  Namespace column and client-side behaviors correctly.

Different column definitions are an error rather than a silently malformed
table.

### Empty Namespace Scope

If the tenant API returns no accessible Namespaces, the transport creates a
valid empty response of the negotiated list or Table shape. kubectl then emits
its standard `No resources found` behavior.

## Atomic Output and Error Behavior

The generic tenant get command uses transactional output buffers. Direct
requests preserve kubectl's normal output behavior. Once tenant aggregation is
activated, stdout and informational stderr are committed only if the complete
kubectl run succeeds.

Any of the following fails the aggregate without printing partial results:

- tenant Namespace resolution failure;
- malformed tenant response;
- missing or invalid Namespace name;
- per-Namespace authentication or authorization failure;
- any other non-success per-Namespace response;
- pagination failure;
- malformed Kubernetes JSON;
- non-list response;
- mismatched list type or Table columns;
- context cancellation or timeout; or
- final output write failure.

Errors add the failing Namespace and resource where known, preserve the
underlying error, and never include bearer tokens or other credentials.

The direct all-Namespaces fallback decision is exact:

- `403` on an eligible namespaced collection GET: attempt tenant aggregation;
- every other response: return it unchanged.

For a command requesting multiple resource types, an aggregate failure
discards all buffered aggregate output, including successful results from an
earlier resource type.

## Validation

Validation occurs before resource requests whenever the required information
is available:

- `--workspace` with `--namespace`;
- `--workspace` with `--all-namespaces`;
- `--workspace` with watch or watch-only;
- `--workspace` with `--raw`;
- `--workspace` with filename input;
- invalid Workspace path segment; and
- Workspace scope on a root-scoped REST mapping.

Kubectl retains responsibility for its existing argument, output, selector,
filename, raw URL, and named cross-Namespace validation.

Server-returned Namespace names are validated with Kubernetes DNS Namespace
rules before they are inserted into a request path.

## Concurrency and Resource Limits

The fixed concurrency limit is eight Namespace requests per aggregated
resource page set. It prevents a large tenant from opening an unbounded number
of connections while allowing materially faster results than sequential
fan-out.

Aggregation necessarily buffers decoded list or Table data until all
Namespaces succeed. This is limited to the explicitly read-only `tenant get`
operation. The implementation avoids duplicate full-object conversions and
releases per-page bodies promptly, but it does not add a separate configurable
memory limit in this change.

## Testing

Implementation follows test-driven development.

### Command Compatibility

- Existing Workspace, Namespace, and Cluster names and aliases still resolve
  to the native tenant handlers.
- Their paths, Cluster routing, flags, table output, JSON, and YAML do not
  change.
- An arbitrary built-in resource and a discovered CRD resolve through
  kubectl.
- Active entrypoint names appear in usage and examples.
- Top-level `get` and `kube get` do not gain `--workspace` or tenant fallback.

### Direct Routing

- Current/default and explicit Namespace requests are unchanged.
- An administrator's successful namespaced `-A` request issues no tenant API
  or namespaced fan-out requests.
- A root-scoped resource is direct even with `-A`.
- Non-`403` all-Namespaces errors are returned without fallback.

### Tenant and Workspace Routing

- A namespaced all-Namespaces `403` resolves the accessible Namespace list and
  sends one namespaced resource request per unique Namespace.
- `--workspace` calls only the Workspace Namespace endpoint and never sends a
  global resource request.
- Explicit Cluster and Context default Cluster prefixes apply to tenant API
  and Kubernetes requests consistently.
- Empty and duplicate Namespace lists behave deterministically.
- Invalid Namespace data fails before an invalid resource URL is sent.

### Response Behavior

- Ordinary unstructured lists merge correctly.
- Server-print Tables merge rows and preserve Namespace rendering.
- Empty scope produces kubectl's no-resources output.
- Label selectors, field selectors, subresources, sorting, and relevant query
  parameters reach every Namespace request.
- JSON, YAML, wide, custom-columns, templates, and default table output remain
  valid single outputs.
- Multiple resource types remain ordered and atomic.
- Per-Namespace pagination follows independent continue tokens and returns all
  items.

### Failure and Cancellation

- A single Namespace `403`, `404`, `429`, `5xx`, malformed response, or
  pagination error fails the whole aggregate and names the Namespace.
- Mismatched kinds or Table columns fail the whole aggregate.
- Context cancellation stops pending work and closes response bodies.
- Concurrency never exceeds eight requests.
- Completion order does not change output order.
- Output writer failure is returned with context.
- No aggregate failure commits buffered stdout or informational stderr.

### Watch and Flag Validation

- Workspace/Namespace/all-Namespaces conflicts fail before requests.
- Workspace with raw, filename, named, root-scoped, or watch queries fails with
  an actionable error.
- A tenant `-A --watch` denial fails before initial resource output.
- An administrator `-A --watch` uses native kubectl behavior.

Final verification runs:

```text
go test ./pkg/cmd/tenant ./pkg/cmd
go test -race ./pkg/cmd/tenant ./pkg/cmd
make verify
./bin/ksctl tenant get --help
./bin/ksctl version
git diff --check
```

## Documentation

Update:

- `docs/cli.md` and `docs/cli_zh.md` with syntax, examples, administrator and
  tenant behavior, Workspace scope, incompatible flags, atomic errors, and
  watch limitations;
- `docs/design.md` and `docs/design_zh.md` with the aggregation-aware transport
  boundary and request flow;
- `README.md` and `README_zh.md` with the expanded tenant capability and one
  representative example; and
- `CHANGELOG.md` with the user-visible arbitrary-resource tenant query
  capability.

## Security

- The feature is read-only and never broadens server authorization.
- Workspace scope is resolved by the KubeSphere tenant API for the current
  authenticated user.
- A forbidden global request is retried only as independently authorized
  per-Namespace requests.
- Every per-Namespace response must succeed; ksctl never treats a forbidden
  Namespace as an empty result.
- Request construction validates server-provided Namespace names.
- Errors and diagnostics omit credentials and raw authorization headers.

## Success Criteria

- Existing native tenant resource commands pass unchanged.
- Cluster administrators can use `tenant get` for arbitrary Kubernetes
  resources with native kubectl behavior.
- Tenant users can use `tenant get RESOURCE -A` for namespaced resources even
  when they cannot list Kubernetes Namespace objects.
- `tenant get RESOURCE --workspace WORKSPACE` returns only resources from
  accessible Namespaces in that Workspace.
- All supported outputs are one valid kubectl-style result.
- Any Namespace failure produces a non-zero result without partial aggregate
  output.
- Tenant aggregation never attempts multi-Namespace watch.
- Full repository verification passes without changes under `staging/`.
