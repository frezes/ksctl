# ksctl CLI Design for KubeSphere 4.x

Date: 2026-05-21

Revised: 2026-07-14

## Summary

`ksctl` is a read-only KubeSphere 4.x CLI for humans and automation. Its
resource command surface contains `get` and `describe`, exposed through both
the standalone `ksctl` binary and the `kubectl-ks` plugin binary.

The commands are not reimplemented by ksctl. They are constructed directly
from kubectl v0.36.2 and use kubectl's resource Builder, discovery, RESTMapper,
printers, watch support, Describers, argument validation, and error behavior.
ksctl owns only KubeSphere configuration, authentication, connection
resolution, and the adapter that presents those settings as a Kubernetes
`RESTClientGetter`.

The first phase has one core outcome: after authenticating through KubeSphere,
users can use kubectl's native `get` and `describe` commands with nearly the
same syntax, behavior, output, and errors as kubectl itself. KSApiServer
transparently proxies the standard Kubernetes `/api` and `/apis` requests to
KubeAPIServer; ksctl does not translate those paths.

## Decisions

- Directly reuse kubectl's `get.NewCmdGet`.
- Directly reuse kubectl's `describe.NewCmdDescribe`.
- Use a shared kubectl `cmdutil.Factory` for both commands.
- Implement a KubeSphere-backed `genericclioptions.RESTClientGetter`.
- Keep `~/.ksctl/config.yaml`; do not read or write kubeconfig.
- Store optional `username`, `bearerToken`, and `bearerTokenFile` fields in the
  ksctl config. When `username` is omitted, use the `users` map key as the
  KubeSphere username.
- Use passwords only for explicit `auth login`; never persist them.
- Resolve tokens through the authentication provider before constructing the
  Kubernetes REST client.
- Use ks-apiserver as the only remote API endpoint.
- Send standard Kubernetes `/api` and `/apis` requests to ks-apiserver without
  path rewriting.
- Resolve resources through server discovery and RESTMapper, not a static
  ksctl registry.
- Pin all Kubernetes modules to v0.36.2, matching the staged kubectl source.
- Remove the standalone `list` command; kubectl `get RESOURCE` is the list
  operation.

## Goals

- Preserve kubectl `get` syntax, flags, output formats, watch behavior, and
  server error semantics.
- Preserve kubectl `describe` syntax, built-in Describers, Generic Describer
  fallback, Events, and formatting.
- Make KubeSphere authentication the only required adaptation before entering
  kubectl's native resource command pipeline.
- Support KubeSphere and Kubernetes resources exposed by ks-apiserver through
  one discovery and request pipeline.
- Make `ksctl ...` and `kubectl ks ...` behavior equivalent.
- Support both interactive use and non-interactive scripts or agents.
- Keep all resource commands in this release read-only.

## Non-Goals

- No KubeSphere 3.x compatibility.
- No create, update, edit, delete, apply, or other mutation commands.
- No fork or local copy of kubectl `get` or `describe` source.
- No independent ksctl implementation of resource parsing, watching,
  printing, sorting, JSONPath, custom columns, or Describers.
- No private success envelope around Kubernetes or KubeSphere API objects.
- No kubeconfig persistence or mutation.
- No system keychain, certificate store, credential reference, browser login,
  or token encryption in the first phase.
- No promise to support an API that cannot provide the discovery, object, list,
  watch, or Events semantics required by the corresponding kubectl operation.

## Command Surface

The resource commands are kubectl's command surfaces:

```bash
ksctl get [kubectl get arguments and flags]
ksctl describe [kubectl describe arguments and flags]
```

Representative invocations include:

```bash
ksctl get workspaces
ksctl get workspace demo
ksctl get pods -n demo
ksctl get deployments,pods -A -l app=web
ksctl get pod/web -o json
ksctl get pods -o custom-columns=NAME:.metadata.name
ksctl get pods --watch
ksctl describe workspace demo
ksctl describe pod/web -n demo
ksctl describe pods -l app=web
```

The plugin entrypoint is equivalent:

```bash
kubectl ks get workspaces
kubectl ks describe workspace demo
```

Support commands such as `version` and `config` remain ksctl-owned. `get` and
`describe` are the complete resource command surface for this release.

## Dependency Policy

Direct dependencies use one Kubernetes minor version:

```text
k8s.io/apimachinery v0.36.2
k8s.io/cli-runtime v0.36.2
k8s.io/client-go    v0.36.2
k8s.io/kubectl      v0.36.2
```

All transitive Kubernetes module versions must remain aligned. Mixed minor
versions are unsupported because kubectl's command, Factory, Builder, printer,
and client interfaces evolve together.

The local staged source is the implementation reference:

```text
staging/src/k8s.io/kubectl-0.36.2/pkg/cmd/get/
staging/src/k8s.io/kubectl-0.36.2/pkg/cmd/describe/
staging/src/k8s.io/kubectl-0.36.2/pkg/describe/
staging/src/k8s.io/kubectl-0.36.2/pkg/cmd/util/factory_client_access.go
```

## Repository Structure

```text
cmd/
  ksctl/              # standalone entrypoint
  kubectl-ks/         # kubectl plugin entrypoint

pkg/
  cmd/                # root command and ksctl-owned support commands
  config/             # independent ksctl config model and file IO
  auth/               # connection resolution, OAuth, and token provider
  cache/token/        # token models, expiry, and local persistence
  kubeclient/         # RESTClientGetter, discovery, and RESTMapper adapter
```

kubectl and cli-runtime provide the resource command, Factory, Builder,
discovery, RESTMapper, printers, watch, and Describer packages.

The previous `ksclient`, `resource`, `output`, and `runtime` packages leave the
resource command path. They are deleted after callers and tests have migrated.

## Command Construction

The shared root command builds one KubeSphere REST client adapter and one
kubectl Factory:

```go
provider := auth.NewProvider(auth.ProviderOptions{})
getter := kubeclient.NewRESTClientGetter(options, provider)
factory := cmdutil.NewFactory(getter)

root.AddCommand(get.NewCmdGet(parent, factory, streams))
root.AddCommand(describecmd.NewCmdDescribe(parent, factory, streams))
```

`parent` reflects the visible entrypoint for help text. Both entrypoints use
the same options, getter, Factory, and command constructors.

The root command registers ksctl connection flags before subcommands run:

```text
--endpoint
--token
--context
--cluster
--workspace
--namespace / -n
--request-timeout
--insecure-skip-tls-verify
--no-interactive
```

kubectl's `get` and `describe` constructors register their own command-specific
flags. ksctl does not redefine those flags.

## KubeSphere Credentials

The ksctl config stores KubeSphere REST client-style credential fields:

```yaml
users:
  admin:
    username: admin
    bearerToken: ""
    bearerTokenFile: ""

contexts:
  local:
    cluster: local
    user: admin
```

`username` defaults to the `users` map key when omitted. Passwords are accepted
only by `ksctl auth login ENDPOINT` and are never persisted. Token resolution
is:

```text
--token > KS_TOKEN > valid cache > refreshed cache > selected user.bearerTokenFile > selected user.bearerToken
```

`pkg/auth` performs the KubeSphere password and refresh grants and owns token
selection. The complete OAuth response is stored under
`~/.ksctl/cache/tokens/<context>.json`; the config contains connection identity
only. The config and token files remain mode `0600`, and passwords or tokens
must never appear in command output, errors, or transport logs.

## RESTClientGetter

`pkg/kubeclient` implements
`k8s.io/cli-runtime/pkg/genericclioptions.RESTClientGetter`:

```go
type RESTClientGetter interface {
    ToRESTConfig() (*rest.Config, error)
    ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error)
    ToRESTMapper() (meta.RESTMapper, error)
    ToRawKubeConfigLoader() clientcmd.ClientConfig
}
```

The implementation resolves configuration lazily because Cobra parses flags
after the command tree is constructed. One invocation resolves and caches one
effective connection.

### ToRESTConfig

`ToRESTConfig` converts the resolved ksctl connection into a Kubernetes
`rest.Config`:

- `Host` is the effective ks-apiserver endpoint.
- `BearerToken` contains the resolved KubeSphere token.
- TLS verification follows the selected ksctl cluster configuration.
- `UserAgent` identifies ksctl and its version.
- Request timeout follows the kubectl command or ksctl default.
- Kubernetes API paths remain unchanged. `/api` and `/apis` are sent directly
  to the effective KSApiServer endpoint.

Endpoint and token resolution is delegated to `pkg/auth`:

```text
endpoint flag > KS_ENDPOINT > selected ksctl context
token flag > KS_TOKEN > cache/refresh > selected user token sources
```

When no usable token exists, authentication fails before creating a Kubernetes
client with `login required` for the selected context.

### ToDiscoveryClient

`ToDiscoveryClient` creates a cached discovery client from the same
`rest.Config`. The first release uses an in-memory cache so ksctl does not
depend on kubeconfig cache directories. Cache invalidation follows client-go's
standard discovery behavior.

### ToRESTMapper

`ToRESTMapper` returns a deferred discovery RESTMapper backed by the cached
discovery client. Resource plural names, singular names, short names,
categories, group/version selection, and scope come from ks-apiserver.

The static ksctl Resource Registry is not used as a fallback. Missing discovery
data is a server compatibility error rather than a reason to maintain a second
resource model.

### ToRawKubeConfigLoader

kubectl commands use `ToRawKubeConfigLoader` to obtain namespace and namespace
enforcement behavior. ksctl returns a `clientcmd.ClientConfig` backed by an
in-memory kubeconfig assembled from the resolved ksctl context.

The in-memory object includes only the effective server, ephemeral
authentication data, context, and namespace needed by cli-runtime. It is never
written to `~/.kube/config`.

KubeSphere `workspace` remains a separate connection or scope value. It is
never silently converted into a Kubernetes namespace.

## KSApiServer Proxy Boundary

kubectl's Builder generates standard Kubernetes request paths under `/api` and
`/apis`. ksctl sends those paths unchanged to the effective KSApiServer
endpoint. KSApiServer is responsible for authenticating the KubeSphere token
and transparently proxying the request to KubeAPIServer.

No ksctl transport maps Kubernetes paths to KubeSphere-specific paths. The
same unchanged path contract applies to:

- discovery requests;
- object and list requests;
- server-side Table requests;
- watch streams;
- Events requests used by describe;
- OpenAPI requests used by kubectl helpers.

The command layer must not inspect resource names to choose a route. Any
KubeSphere cluster or workspace selection is resolved as connection context
before kubectl runs and must not mutate the standard Kubernetes API path.

The adapter preserves the path, escaped resource names, query parameters,
headers, request body, and watch stream. A KSApiServer endpoint is compatible
when its discovery returns mappings that kubectl can use and the resulting
object, list, Table, watch, and Events requests reach the same logical
resource.

## Get

`get.NewCmdGet` is registered without a local wrapper around its execution
pipeline. ksctl therefore inherits kubectl v0.36.2 behavior, including:

- one or many resource types and names;
- `TYPE`, `TYPE NAME`, and `TYPE/NAME` forms;
- versioned and group-qualified resource names;
- label and field selectors;
- current namespace and all-namespaces behavior;
- filename and kustomize inputs;
- subresource and raw requests;
- pagination and chunk size;
- ignore-not-found behavior;
- watch, watch-only, and output-watch-events;
- server-side Table negotiation;
- default, wide, name, JSON, and YAML output;
- JSONPath, Go template, and custom-columns output;
- no-headers, show-kind, show-labels, label columns, and sorting;
- aggregation and formatting of multiple resource types;
- kubectl empty-result and error messages.

ksctl does not post-process successful output. stdout is the kubectl command's
output stream.

## Describe

`describe.NewCmdDescribe` is registered without replacing its Describer
selection. ksctl therefore inherits kubectl v0.36.2 behavior, including:

- `TYPE NAME_PREFIX`, `TYPE/NAME`, selector, and filename forms;
- one or many matching objects;
- current namespace and all-namespaces behavior;
- `--show-events` and `--chunk-size`;
- exact-name lookup followed by prefix matching when appropriate;
- built-in resource-specific Describers for Kubernetes resource kinds;
- Generic Describer fallback for other discovered resources;
- Events lookup and kubectl's single-versus-multiple-object Events defaults;
- kubectl tabular indentation and human-readable formatting;
- aggregation of partial errors and empty-result messages.

`describe` does not provide `-o`. Structured output remains a `get` concern.

## Error Handling

kubectl's command implementations own resource usage errors, server errors,
partial failures, empty results, and output-printer errors. ksctl must preserve
their messages and stdout/stderr placement.

ksctl owns only errors that occur before kubectl can run:

```text
error: KubeSphere endpoint is not configured
error: login required for context "prod"
error: no context exists with the name: prod
error: KSApiServer does not expose Kubernetes API discovery
```

Authentication material must never appear in errors or verbose transport logs.

The root command must integrate kubectl's `cmdutil.CheckErr` behavior so both
entrypoints return the same process exit semantics as kubectl.

## Existing Code Migration

The current branch contains a custom `get` implementation. Migration is a
replacement, not a parallel path:

1. Add aligned Kubernetes dependencies.
2. Add and test the KubeSphere authentication and `RESTClientGetter` adapter.
3. Construct a kubectl Factory from that getter.
4. Replace the custom `get` command with `get.NewCmdGet`.
5. Add `describe.NewCmdDescribe` with the same Factory.
6. Remove the `list` command.
7. Delete custom resource-command packages after they have no callers.
8. Update README examples and command help snapshots.

During migration, tests may temporarily exercise both paths, but the final
binary must contain only the kubectl resource command path.

## Testing Strategy

Upstream kubectl behavior is covered by kubectl's own tests. ksctl tests focus
on adapter correctness and integration boundaries rather than copying the full
kubectl suite.

### Unit Tests

```text
config/auth adapter
  - flag, environment, and context precedence
  - endpoint and token validation
  - explicit username and users map key fallback
  - valid and refreshed cache precedes legacy configured tokens
  - bearer token file takes precedence over configured bearer token
  - failed refresh requires a new login
  - cluster, workspace, and namespace defaults
  - TLS and no-interactive behavior

RESTClientGetter
  - rest.Config fields and redaction
  - in-memory ClientConfig namespace behavior
  - discovery client caching
  - deferred RESTMapper construction
  - concurrent calls return one effective connection

KSApiServer proxy adapter
  - /api discovery and resource paths remain unchanged
  - /apis discovery and resource paths remain unchanged
  - query strings and watch parameters are preserved unchanged
  - selected connection context does not rewrite Kubernetes API paths
  - Authorization is not duplicated or logged

command tree
  - get and describe are registered
  - list is not registered
  - ksctl and kubectl-ks expose equivalent flags
```

### Integration Tests

Use an `httptest.Server` that implements representative discovery and API
responses. Cover:

```text
get list and named object
multiple resource types
table, json, yaml, name, and custom-columns output
label and field selectors
watch stream startup and cancellation
describe built-in Describer
describe Generic fallback
describe Events request
NotFound, Forbidden, and malformed discovery errors
```

The test server must assert unchanged paths, query parameters, Accept headers,
Authorization headers, and selected connection context.

### Optional E2E Tests

```bash
ksctl get workspaces
ksctl get pods -A -o wide
ksctl get pods -l app=web -o json
ksctl get pods --watch-only
ksctl describe workspace demo
ksctl describe pod/web -n demo
kubectl ks get workspaces
kubectl ks describe workspace demo
```

E2E tests validate the exact KubeSphere authentication, transparent proxy, and
discovery assumptions that cannot be proven with a fake server.

## Validation Items

Before implementation, validate against a KubeSphere 4.x endpoint:

- Discovery endpoints and returned APIResourceList data for KubeSphere and
  Kubernetes resources.
- Standard `/api` and `/apis` proxy behavior through KSApiServer.
- Effective KSApiServer endpoint selection for host and member clusters.
- Server-side Table negotiation behavior.
- Watch support and resourceVersion semantics.
- Events lookup for KubeSphere resources.
- OpenAPI behavior required by kubectl helpers.
- Authentication header and TLS requirements.

Any authentication incompatibility should be solved in `pkg/auth`; Kubernetes
client adaptation belongs in `pkg/kubeclient`. The command implementations
must remain unmodified upstream kubectl commands, and ksctl must not compensate
by rewriting Kubernetes API paths.

## References

- Local kubectl v0.36.2 command source:
  `staging/src/k8s.io/kubectl-0.36.2/pkg/cmd/`
- Local cli-runtime v0.36.2 Builder source:
  `$GOPATH/pkg/mod/k8s.io/cli-runtime@v0.36.2/pkg/resource/`
- KubeSphere API concepts:
  https://dev-guide.kubesphere.io/extension-dev-guide/en/references/kubesphere-api-concepts/
- Kubernetes kubectl plugin guide:
  https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/
