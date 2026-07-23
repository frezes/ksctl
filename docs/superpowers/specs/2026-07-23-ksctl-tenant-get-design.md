# ksctl Tenant Get Design

Date: 2026-07-23

## Summary

Add a read-only `tenant get` command group to both `ksctl` and `kubectl ks`.
The commands call KubeSphere Enterprise 4.2.1's native
`tenant.kubesphere.io/v1beta1` `/kapis` endpoints rather than the Kubernetes
`/api` and `/apis` endpoints used by the existing kubectl-derived `get`
command.

The initial resource surface lists WorkspaceTemplates, Namespaces, and
Clusters, presenting WorkspaceTemplates to users as Workspaces, and can
retrieve one named WorkspaceTemplate. Namespace and Cluster lists optionally
use a Workspace scope. Namespace requests follow the effective KubeSphere
Cluster selected by `--cluster` or the current Context's `defaultCluster`;
WorkspaceTemplate and Cluster requests always remain at Fleet scope.

## Goals

- Add the same `tenant get` commands to the standalone and kubectl-plugin
  entrypoints.
- Use the KSE 4.2.1 APIs documented by
  `staging/src/kubesphere.io/kubesphere-4.2.1/api/ks-openapi-spec/swagger.yaml`.
- Reuse existing Endpoint, Context, TLS, credential precedence, token-cache
  refresh, user-agent, request-timeout, and effective-Cluster resolution.
- Support default table output plus `-o json` and `-o yaml`.
- Keep the server response structure unchanged in JSON and YAML output.
- Validate every Workspace and Cluster value before using it as a request path
  segment.

## Non-Goals

- Do not add tenant create, update, patch, or delete commands.
- Do not add single-Namespace or single-Cluster retrieval.
- Do not expose `ws` as a resource alias.
- Do not add client-side sorting, filtering, pagination, watching, selection,
  custom columns, templates, or name-only output.
- Do not route these native `/kapis` resources through kubectl discovery or its
  Kubernetes REST mapper.
- Do not change the behavior of the existing top-level `get` and `describe`
  commands.

## CLI Behavior

The command syntax is:

```text
ksctl tenant get workspace [NAME] [-o table|json|yaml]
ksctl tenant get namespace [--workspace WORKSPACE] [-o table|json|yaml]
ksctl tenant get cluster [--workspace WORKSPACE] [-o table|json|yaml]
```

The same commands are available under `kubectl ks tenant get`.

Accepted resource names are:

- `workspace` and `workspaces`;
- `namespace`, `namespaces`, and `ns`; and
- `cluster` and `clusters`.

`ws` is not a resource alias. `--workspace` is defined only for Namespace and
Cluster commands and has no short form. Workspace accepts zero or one
positional name. Namespace and Cluster accept no positional names.

The `-o`/`--output` value defaults to `table`. The only accepted values are
`table`, `json`, and `yaml`.

## Request Routing

The base KSE API path is:

```text
/kapis/tenant.kubesphere.io/v1beta1
```

The effective Cluster is resolved using existing connection semantics:
an explicit root `--cluster` overrides the selected Context's
`defaultCluster`. If neither supplies a Cluster, the effective Cluster is
empty.

Namespace requests apply the effective Cluster as a KubeSphere proxy prefix.
Workspace requests use the Fleet-scoped `workspacetemplates` resource and
Cluster resource requests deliberately ignore the effective Cluster.

| Command | Empty effective Cluster | Non-empty effective Cluster |
| --- | --- | --- |
| `tenant get workspace` | `/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates` | `/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates` |
| `tenant get workspace NAME` | `/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates/{name}` | `/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates/{name}` |
| `tenant get ns` | `/kapis/tenant.kubesphere.io/v1beta1/namespaces` | `/clusters/{cluster}/kapis/tenant.kubesphere.io/v1beta1/namespaces` |
| `tenant get ns --workspace WORKSPACE` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/namespaces` | `/clusters/{cluster}/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/namespaces` |
| `tenant get cluster` | `/kapis/tenant.kubesphere.io/v1beta1/clusters` | `/kapis/tenant.kubesphere.io/v1beta1/clusters` |
| `tenant get cluster --workspace WORKSPACE` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/clusters` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/clusters` |

All requests use HTTP GET and the bearer token resolved by the existing
KubeSphere connection stack.

## Response and Output

KSE does not use one list envelope consistently across these endpoints.
WorkspaceTemplate lists use a pageable response with `items` and
`total_count`, while Namespace and Cluster lists use `items` and `totalItems`.
Table rendering therefore reads only the `items` array and does not depend on
either count field. A single WorkspaceTemplate response is treated as one
table row.

JSON output parses the response to ensure it is valid JSON, then writes it with
stable indentation. YAML output converts the same complete JSON response to
YAML. Both modes preserve the original top-level envelope, count field,
resource fields, and item order. They do not replace list responses with a
client-defined schema.

Default table output preserves server item order and follows the familiar
kubectl column layout for the corresponding resources:

| Resource | Columns |
| --- | --- |
| Workspace | `NAME`, `CLUSTERS`, `ADMINISTRATOR`, `AGE` |
| Namespace | `NAME`, `STATUS`, `AGE` |
| Cluster | `NAME`, `PROVIDER`, `VERSION` |

Workspace values are read from `metadata.name`,
`spec.placement.clusters[*].name`, `spec.template.spec.manager`, and
`metadata.creationTimestamp`. Multiple explicit Cluster names are joined with
commas in their response order. AGE uses the kubectl-style compact duration
since `metadata.creationTimestamp`, such as `8d`, and is `<unknown>` when the
timestamp is absent or invalid.

Namespace Status is `status.phase`. AGE is the kubectl-style compact duration
since `metadata.creationTimestamp`, such as `8d`, and is `<unknown>` when the
timestamp is absent or invalid.

Cluster columns match the KSE 4.2.1 Cluster CRD printer columns:
`metadata.name`, `spec.provider`, and `status.kubernetesVersion`. Missing
optional string values render as an empty table cell, matching kubectl's
table behavior.

When a list has no items, table output prints `No resources found` rather than
an empty header-only table.

## Components

### Tenant KubeSphere Client

Add a focused package under `pkg/client/kubesphere/tenant`. It owns the
v1beta1 base path, Cluster-prefix application, Workspace-scope application,
GET requests, and response decoding needed to identify list items.

The package receives an existing unversioned KubeSphere REST client or REST
configuration. Its request methods distinguish the Namespace resource, which
follows the effective Cluster, from WorkspaceTemplate and Cluster resources,
which ignore it. It returns the complete raw response together with the
decoded objects required for table printing.

### Tenant Commands

Add `pkg/cmd/tenant.go` for the Cobra command tree, resource aliases, argument
and output validation, invocation of the tenant client, and output printers.
The command depends on a narrow getter interface exposing:

```go
ToRESTConfig() (*rest.Config, error)
KubeSphereCluster() (string, error)
```

The existing `pkg/client/kubesphere/connection.RESTClientGetter` already
implements this interface. `pkg/cmd/root.go` injects that getter into the
tenant command, so both executable entrypoints share all behavior.

The table printer is local to the tenant command because KSE list envelopes
and `/kapis` paths are not Kubernetes API resources and should not be
artificially inserted into kubectl discovery.

## Validation and Error Behavior

The command validates output format, argument count, Workspace names, and the
resolved Cluster before making a request. Values used in URLs must pass
KubeSphere path-segment validation.

The command returns a non-zero result for:

- unsupported resources or output formats;
- extra or missing positional arguments;
- invalid Workspace or effective-Cluster path segments;
- connection, credential, TLS, timeout, or REST-client construction failures;
- authentication, authorization, not-found, transport, or non-success HTTP
  responses from KubeSphere;
- malformed JSON;
- a list response without an `items` array;
- a non-object item or single-WorkspaceTemplate response; and
- output write failures.

Errors add the resource and relevant Cluster or Workspace scope without
including credentials. The command prints no partial successful response when
requesting, decoding, validating, or formatting fails.

## Testing

Implementation follows test-driven development. Focused tests cover:

- registration under both `ksctl` and `kubectl ks`;
- all accepted resource names and rejection of the `ws` resource alias;
- Fleet-scoped WorkspaceTemplate list and named-Workspace paths;
- Namespace list paths with and without `--workspace`;
- Cluster list paths with and without `--workspace`;
- rejection of `-w` for Namespace and Cluster;
- explicit `--cluster` and Context `defaultCluster` prefixes for Namespace;
- explicit and default Clusters being ignored by WorkspaceTemplate and both
  Cluster-list paths;
- authorization headers, user agent, and request timeout reuse;
- default Workspace, Namespace, and Cluster kubectl-style columns and values;
- comma-joined Workspace placement Clusters, Administrator, and compact AGE
  rendering;
- Namespace compact AGE and unknown timestamp rendering;
- Cluster Provider and Kubernetes Version rendering, including empty Provider
  cells;
- empty-list `No resources found` output;
- JSON and YAML preservation of both supported list envelopes and single
  WorkspaceTemplate responses;
- invalid names, arguments, output formats, malformed responses, HTTP errors,
  and output failures; and
- no regression to the existing kubectl-derived `get` and `describe`
  commands.

Final verification includes focused package tests, the full normal and race
test suites, formatting, module consistency, vet, both binary builds, and
`git diff --check`.

## Documentation

Update `docs/cli.md` with the tenant command syntax, aliases, long-form
Workspace scope, output modes, and Cluster-routing exceptions. Update
`docs/design.md` with the native `/kapis` tenant pipeline and its difference
from kubectl-derived resource commands. Add the new command surface to
`README.md` and record the feature in `CHANGELOG.md`.
