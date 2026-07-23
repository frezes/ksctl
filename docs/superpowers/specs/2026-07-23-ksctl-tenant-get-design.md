# ksctl Tenant Get Design

Date: 2026-07-23

## Summary

Add a read-only `tenant get` command group to both `ksctl` and `kubectl ks`.
The commands call KubeSphere Enterprise 4.2.1's native
`tenant.kubesphere.io/v1beta1` `/kapis` endpoints rather than the Kubernetes
`/api` and `/apis` endpoints used by the existing kubectl-derived `get`
command.

The initial resource surface lists Workspaces, Namespaces, and Clusters, and
can retrieve one named Workspace. Namespace and Cluster lists optionally use a
Workspace scope. Workspace and Namespace requests follow the effective
KubeSphere Cluster selected by `--cluster` or the current Context's
`defaultCluster`; Cluster-list requests always remain at Fleet scope.

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
ksctl tenant get namespace [-w WORKSPACE] [-o table|json|yaml]
ksctl tenant get cluster [-w WORKSPACE] [-o table|json|yaml]
```

The same commands are available under `kubectl ks tenant get`.

Accepted resource names are:

- `workspace` and `workspaces`;
- `namespace`, `namespaces`, and `ns`; and
- `cluster` and `clusters`.

`ws` is not a resource alias. `-w` is the short form of the
`--workspace` scope flag and is defined only for Namespace and Cluster
commands. Workspace accepts zero or one positional name. Namespace and Cluster
accept no positional names.

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

Workspace and Namespace requests apply the effective Cluster as a KubeSphere
proxy prefix. Cluster resource requests deliberately ignore the effective
Cluster and remain Fleet-scoped.

| Command | Empty effective Cluster | Non-empty effective Cluster |
| --- | --- | --- |
| `tenant get workspace` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces` | `/clusters/{cluster}/kapis/tenant.kubesphere.io/v1beta1/workspaces` |
| `tenant get workspace NAME` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{name}` | `/clusters/{cluster}/kapis/tenant.kubesphere.io/v1beta1/workspaces/{name}` |
| `tenant get ns` | `/kapis/tenant.kubesphere.io/v1beta1/namespaces` | `/clusters/{cluster}/kapis/tenant.kubesphere.io/v1beta1/namespaces` |
| `tenant get ns -w WORKSPACE` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/namespaces` | `/clusters/{cluster}/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/namespaces` |
| `tenant get cluster` | `/kapis/tenant.kubesphere.io/v1beta1/clusters` | `/kapis/tenant.kubesphere.io/v1beta1/clusters` |
| `tenant get cluster -w WORKSPACE` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/clusters` | `/kapis/tenant.kubesphere.io/v1beta1/workspaces/{workspace}/clusters` |

All requests use HTTP GET and the bearer token resolved by the existing
KubeSphere connection stack.

## Response and Output

KSE does not use one list envelope consistently across these endpoints.
Workspace lists may use a pageable response with `items` and `total_count`,
while Namespace and Cluster lists use `items` and `totalItems`. Table rendering
therefore reads only the `items` array and does not depend on either count
field. A single Workspace response is treated as one table row.

JSON output parses the response to ensure it is valid JSON, then writes it with
stable indentation. YAML output converts the same complete JSON response to
YAML. Both modes preserve the original top-level envelope, count field,
resource fields, and item order. They do not replace list responses with a
client-defined schema.

Default table output preserves server item order and prints a header even when
the list is empty:

| Resource | Columns |
| --- | --- |
| Workspace | `NAME`, `MANAGER` |
| Namespace | `NAME`, `STATUS` |
| Cluster | `NAME`, `STATUS`, `KUBERNETES VERSION`, `KUBESPHERE VERSION`, `NODES` |

The Workspace name is `metadata.name`. Manager is read from `spec.manager`,
with `spec.template.spec.manager` as a compatibility fallback for a
WorkspaceTemplate response. Namespace status is `status.phase`.

Cluster status is derived from the `Ready` entry in `status.conditions`:
`True` displays `Ready`, `False` displays `NotReady`, and an unknown or absent
condition displays `Unknown`. The remaining Cluster columns use
`status.kubernetesVersion`, `status.kubeSphereVersion`, and
`status.nodeCount`. Missing optional values display `<none>`.

## Components

### Tenant KubeSphere Client

Add a focused package under `pkg/client/kubesphere/tenant`. It owns the
v1beta1 base path, Cluster-prefix application, Workspace-scope application,
GET requests, and response decoding needed to identify list items.

The package receives an existing unversioned KubeSphere REST client or REST
configuration. Its request methods distinguish between resources that follow
the effective Cluster and the Cluster resource that must ignore it. It returns
the complete raw response together with the decoded objects required for table
printing.

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
- a non-object item or single-Workspace response; and
- output write failures.

Errors add the resource and relevant Cluster or Workspace scope without
including credentials. The command prints no partial successful response when
requesting, decoding, validating, or formatting fails.

## Testing

Implementation follows test-driven development. Focused tests cover:

- registration under both `ksctl` and `kubectl ks`;
- all accepted resource names and rejection of the `ws` resource alias;
- Workspace list and named-Workspace paths;
- Namespace list paths with and without `-w`;
- Cluster list paths with and without `-w`;
- explicit `--cluster` and Context `defaultCluster` prefixes for Workspace and
  Namespace;
- explicit and default Clusters being ignored by both Cluster-list paths;
- authorization headers, user agent, and request timeout reuse;
- default Workspace, Namespace, and Cluster table columns and values;
- Workspace manager compatibility fallback;
- Cluster Ready, NotReady, and Unknown rendering;
- empty-list tables;
- JSON and YAML preservation of both supported list envelopes and single
  Workspace responses;
- invalid names, arguments, output formats, malformed responses, HTTP errors,
  and output failures; and
- no regression to the existing kubectl-derived `get` and `describe`
  commands.

Final verification includes focused package tests, the full normal and race
test suites, formatting, module consistency, vet, both binary builds, and
`git diff --check`.

## Documentation

Update `docs/cli.md` with the tenant command syntax, aliases, optional
Workspace scope, output modes, and Cluster-routing exception. Update
`docs/design.md` with the native `/kapis` tenant pipeline and its difference
from kubectl-derived resource commands. Add the new command surface to
`README.md` and record the feature in `CHANGELOG.md`.
