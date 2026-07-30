# ksctl Design

**English** | [简体中文](design_zh.md)

This document describes the current architecture of ksctl. For command syntax
and workflows, see the [CLI guide](cli.md). Historical specifications under
`docs/superpowers/` record individual decisions and implementation phases but
are not the current architecture reference.

## Goals

- Provide one CLI for inspecting KubeSphere 4.x resources and Kubernetes
  resources, logs, and current metrics exposed through KubeSphere.
- Preserve familiar kubectl `get`, `describe`, `logs`, and `top` syntax,
  discovery, selection, printing, streaming, and error behavior.
- Keep the kubectl-backed generic resource surface read-only. Purpose-built
  Extension lifecycle commands provide controlled writes, while `api` is an
  explicit low-level escape hatch that sends caller-selected KubeSphere API
  requests.
- Support interactive use and explicit, predictable automation.
- Use the KubeSphere API Endpoint and credentials without reading or changing
  the user's kubeconfig.
- Keep configuration, authentication, API clients, and command wiring in
  focused packages with testable boundaries.

## Non-goals

- Typed built-in create, update, edit, patch, delete, apply, or other generic
  resource mutation commands. The raw `api` transport escape hatch is not a
  typed resource-management workflow.
- A local reimplementation or fork of kubectl resource parsing, printers,
  watchers, or Describers
- Reading, merging, or writing `~/.kube/config`
- A static public registry of the resources a KubeSphere server may expose
- KubeSphere logging or monitoring extension APIs, historical observability
  queries, or cross-Cluster logs/metrics aggregation
- Auditing or sandboxing executable plugins
- KubeSphere 3.x compatibility

Executable plugins are outside the built-in command surface and may implement
operations that are not read-only. They run as independent programs under the
user's authority.

## Command architecture

The executable entry point is intentionally small:

```text
cmd/ksctl/main.go -> NewRootCommandWithArgs
```

The constructor delegates to the shared root-command builder in `pkg/cmd`.

The root owns KubeSphere connection flags and constructs these command groups:

- ksctl-owned `api`, `auth`, `config`, `extension`, `plugin`, `tenant`, and
  `version` commands;
- kubectl-owned `get`, `describe`, `logs`, and `top` commands; and
- Cobra-provided help, completion, and shell-completion commands.

Commands use injected input, output, and error streams. The executable entry
point connects those streams to the process standard streams and prints a
single returned error before exiting non-zero. The root enables
`SilenceUsage` and `SilenceErrors` so execution failures do not also print
usage or duplicate the error.

Before Cobra executes an unknown command, the `WithArgs` constructor gives the
plugin dispatcher an opportunity to resolve it to a `ksctl-*` executable.

## Resource command pipeline

ksctl constructs kubectl v0.36.2's commands in
`pkg/cmd/resource_commands.go`:

```text
Cobra command
  -> kubectl get, describe, logs, or top command/options
  -> kubectl Factory
  -> ksctl Kubernetes RESTClientGetter
  -> discovery and RESTMapper where required
  -> Kubernetes REST, Core, or Metrics client
  -> KubeSphere Endpoint
```

`get`, `describe`, and `logs` use their upstream constructors directly. `top`
uses upstream parent, subcommand, option, validation, client, and printer
types, with one private assembly adapter: after upstream `Complete`, ksctl
replaces top's raw Clientset DiscoveryClient with
`Factory.ToDiscoveryClient`. This preserves ksctl's KubeSphere discovery
fallback without forking top behavior.

A recursive example normalizer changes upstream `kubectl` examples to the
active command display name.

This delegates resource arguments, selectors, filename inputs, pagination,
watching, table negotiation, printers, built-in Describers, generic describe
fallback, Events, workload-to-Pod log resolution, log streaming, Metrics
clients, resource calculations, and most error behavior to the pinned kubectl
implementation. It also means command-specific capabilities evolve only when
the aligned Kubernetes dependencies are intentionally upgraded.

### Native tenant pipeline

The `pkg/cmd/tenant` package implements `tenant get` separately from kubectl's
discovery-driven resource commands. It uses the KubeSphere REST client to call
the KSE tenant API at `/kapis/tenant.kubesphere.io/v1beta1`, decodes the
returned object or list envelope, and renders the default kubectl-style table
or preserves the response envelope for JSON and YAML output.

Workspace and tenant Cluster requests are Fleet-scoped and ignore the resolved
Cluster. Namespace requests apply the resolved explicit or Context-default
Cluster through the KubeSphere REST client's Cluster routing, which adds the
`/clusters/<cluster>` prefix. An optional Workspace adds
`/workspaces/<workspace>` to Namespace and tenant Cluster collection routes.

The Factory consumes `pkg/client/kubernetes.RESTClientGetter`, which implements
the four cli-runtime client interfaces:

```text
ToRESTConfig
ToDiscoveryClient
ToRESTMapper
ToRawKubeConfigLoader
```

Connection resolution is lazy because Cobra parses flags after constructing
the command tree. Each getter caches one resolved connection, discovery client,
and RESTMapper for the lifetime of one command invocation.

### Discovery compatibility

The normal path uses live KubeSphere discovery and an in-memory discovery
cache. A deferred RESTMapper and shortcut expander derive API mappings,
singular and plural names, short names, groups, versions, and scope.

Some KubeSphere deployments expose individual discovery endpoints but fail the
aggregate API-group request. The fallback discovery client handles that case
by probing candidate group versions derived from:

- the Kubernetes client scheme;
- CustomResourceDefinitions returned by the server; and
- Kubernetes and KubeSphere APIService registrations.

It constructs an aggregate discovery view only from group versions that
actually respond. When a Cluster-scoped core-v1 discovery request fails, the
client may query the unscoped KubeSphere Endpoint for that discovery data while
resource requests remain Cluster-scoped. This is a compatibility path, not a
hard-coded list of user-visible KubeSphere resources.

The private top adapter uses this same cached discovery surface. This matters
because kubectl v0.36.2 otherwise takes top discovery from a newly created
Clientset and bypasses `RESTClientGetter.ToDiscoveryClient`. Metrics API
discovery can therefore be recovered from APIService registration and the
concrete `metrics.k8s.io/v1beta1` endpoint when aggregate discovery roots are
unusable.

## Client boundaries

### Shared options

`pkg/client.Options` is the command-to-client input model. It carries Endpoint,
Token, Context, Cluster, Namespace, request timeout, config path, user agent,
and TLS verification settings. Cobra binds the public root flags to one Options
value shared by both client getters. Interactive input is not a client concern;
it is owned by the `auth login` command and its terminal prompter.

### Kubernetes client adapter

`pkg/client/kubernetes` adapts resolved ksctl state to Kubernetes
`client-go` and `cli-runtime` interfaces. It owns:

- token and TLS resolution for Kubernetes requests;
- member-Cluster Endpoint construction;
- request-timeout parsing;
- an in-memory client config for kubectl namespace handling;
- cached discovery with the compatibility fallback; and
- a deferred discovery RESTMapper.

The in-memory client config contains only the effective server, token, TLS
settings, Context name, and Namespace required by cli-runtime. It is never
written as kubeconfig.

### KubeSphere REST clients

`pkg/client/kubesphere` creates unversioned KubeSphere REST clients. It is used
for KubeSphere-native operations such as OAuth, version queries, and kubeconfig
retrieval, with either a generated HTTP client or an injected client for tests.

`pkg/client/kubesphere/connection` resolves the KubeSphere-native REST config,
selected Cluster, and selected username. Unlike the Kubernetes getter, its base
REST config keeps the Fleet Endpoint unscoped; a KubeSphere-native request adds
Cluster scope explicitly when its API supports it.

`internal/kubesphererest` is the sole adapter from ksctl's TLS configuration to
`kubesphere.io/client-go/rest.TLSClientConfig`. OAuth and the KubeSphere
connection getter both use it. The Kubernetes adapter remains separate because
it targets the `k8s.io/client-go/rest` type.

## Raw KubeSphere API requests

The `api` command is a low-level authenticated transport escape hatch:

```text
ksctl api API_PATH [-X METHOD] [-d DATA]
```

`API_PATH` must be server-relative and begin with `/`. It may contain a query
string, but absolute URLs and fragments are rejected. The command reuses
ksctl's KubeSphere connection, credential, TLS, user-agent, and timeout
resolution.

The selected Cluster and a Context's `defaultCluster` do not rewrite the
request path. A caller targeting a Cluster-scoped endpoint must include the
complete `/clusters/<cluster>` prefix in `API_PATH`.

`GET` is the default method. Supplying `--data` without an explicit method
selects `POST`; an explicit `--method` always wins. Supplied data is sent as raw
bytes with `Content-Type: application/json`.

The response body is copied unchanged to stdout. For an HTTP error response,
the command writes the received body and also returns an error. It performs no
resource typing, lifecycle validation, response formatting, redaction, or
write guard. The caller therefore owns the effects and disclosure risks of the
selected path, method, request body, and response destination.

## Extension management

Extension management is the deliberate, purpose-built write workflow alongside
the otherwise read-only kubectl-backed resource surface. It owns only
`kubesphere.io/v1alpha1` Extension, ExtensionVersion, and InstallPlan workflows;
it does not expose generic mutation verbs.

Responsibilities are split at a narrow boundary:

```text
pkg/cmd/extension
  Cobra flags, local file/stdin input, stable tables, lifecycle messages
  and command-specific scope rejection

internal/extension
  private wire models, host REST paths, catalog joins, dependency validation,
  guarded lifecycle writes, stale-safe waiting, and diagnosis
```

The wire models intentionally remain private and retain each complete raw JSON
document. Table output reads known fields, while JSON and YAML output preserve
unknown server fields for forward compatibility.

All extension catalog and InstallPlan requests use the host KubeSphere
Endpoint. A dedicated connection getter ignores a Context's `defaultCluster`;
the global `--cluster` flag is rejected before connection resolution.
Extension commands do not define the resource-command-local `--namespace`
flag. Multicluster placement is expressed in the InstallPlan with `--clusters`
and overrides. `diagnose --target-cluster` selects a member's status from the
host InstallPlan, but the referenced Namespace, Job, and Pods are still read
through host `/api/v1` and `/apis/batch/v1` paths.

Install creates an enabled InstallPlan with `upgradeStrategy: Manual`. Upgrade
and configure send minimal JSON Merge Patches guarded by the current
`metadata.resourceVersion`; conflicts are returned instead of retrying changed
intent. Exact ExtensionVersion values remain opaque, but controller-facing
operations directly require the resource identity `<extension>-<version>`.
Required dependencies are validated without automatic installation.

Install uses the Extension's current `status.recommendedVersion` when
`--version` is omitted; upgrade still requires an explicit exact version. For
a multicluster install, `--all-clusters` lists host `Clusters`, selects the
current ready and schedulable set, and writes that resolved eligible snapshot
as explicit placement rather than a dynamic selector. The snapshot includes
the host Cluster when it satisfies the same conditions; the KubeSphere
controller remains responsible for host handling.

Lifecycle writes are asynchronous unless `--wait` is explicit. The waiter
uses the accepted create or patch response as a target-local baseline, so a
stale pre-existing terminal status is not attributed to the new operation.
Host and member targets advance independently, effective configuration hashes
match the KubeSphere controller's merge semantics, and removed member failures
remain actionable. Clearing scheduling uses an explicit empty placement so the
controller performs member cleanup. Uninstall succeeds only when the
InstallPlan becomes NotFound. Accepted specs and object identities are fenced
so a dropped admission change, concurrent mutation, deletion, or same-name
recreation cannot be reported as the original operation's success.

Diagnosis validates the controller's exact target ExtensionVersion and reports
controller state, conditions, dependencies, Namespace, Job, Pod terminations,
member statuses, and limited timestamp evidence. Completed retrying Jobs and
recovered container history are distinguished from current failures. A complete
healthy default result is a concise health line; other default results show
only warning and error checks plus status counts, while `--verbose` shows every
completed check. Interrupted diagnosis reports its completed checks and an
incomplete marker before returning the service error. It suggests follow-up
`kubectl logs` commands but does not retrieve logs, Secrets, or Helm values.

Human-readable extension output escapes terminal control data. Extension
commands also reject REST debug verbosity `--v=8` and higher because that
client level may log InstallPlan configuration bodies.

Because `extension` is built in, plugin executables named `ksctl-extension` or
nested beneath that path cannot override or extend it. Plugin listing reports
those executables as built-in command conflicts.

## Configuration model

ksctl stores its state in `~/.ksctl/config.yaml`, or the path selected by
`KSCTL_CONFIG`. It does not use kubeconfig as its persistent model.

```text
Config
  currentContext -> Context name
  Fleets
    Fleet
      host
      TLS client settings
      Users
        User
          username
          bearerTokenFile | bearerToken | password
  Contexts
    Context
      Fleet reference
      User reference
      defaultCluster
```

A Fleet owns an Endpoint, TLS client configuration, and its Users. Users are
Fleet-scoped, so the same User map key may exist in multiple Fleets. If a
User's `username` is empty, the User map key becomes the KubeSphere username.

A Context selects one Fleet and User and may set a default Cluster. The current
Context is the default selection. An explicit `--context` chooses another
Context for one invocation; an explicit `--cluster` overrides its
`defaultCluster`.

Missing and empty files load as an initialized empty Config. Loading fills
absent API version, kind, Fleet map, and Context map defaults. Non-empty files
are decoded strictly: unknown fields, duplicate fields, type mismatches, legacy
root-level Users or Cluster models, unsupported API versions, and unsupported
kinds are errors. Saving uses the same type-contract validation and refuses to
rewrite a Config version or kind the binary does not understand.

## Authentication model

Authentication is divided into three responsibilities:

1. `pkg/auth.Resolve` combines flags, environment variables, current Context,
   Fleet, User, default Cluster, and TLS settings into a connection identity.
2. `pkg/auth.Provider` selects or obtains a bearer Token for that identity.
3. `pkg/auth.OAuth` performs password and refresh grants against
   `/oauth/token`.

Endpoint selection is:

```text
--endpoint > KS_ENDPOINT > selected Fleet host
```

An Endpoint selected by `--endpoint` or `KS_ENDPOINT` must be paired with a
Token selected by `--token` or `KS_TOKEN`. Resolution fails before consulting
Context credentials when the pair is incomplete. Supplying only `KS_TOKEN`
remains valid: the selected Fleet supplies its Endpoint and TLS settings.

Credential selection is:

```text
--token > KS_TOKEN > bearerTokenFile > bearerToken > token cache > password
```

An explicit flag or environment Token returns without reading configured or
cached credentials. A configured Token File or Token is also authoritative; a
file read error or empty file returns an error instead of falling through.

The token cache is keyed by Fleet and User under
`~/.ksctl/cache/tokens/<fleet>/<user>.json`. Valid Access Tokens are reused. An
expired entry with a Refresh Token attempts a refresh and atomically replaces
the cache on success. If refresh is unavailable or fails, a configured Password
may obtain an Access Token for the current command, but that response is not
cached. Malformed or otherwise unreadable cache data is an error rather than a
reason to ignore the file.

`auth login` is the explicit cache-creation path. A Fleet name owns one
normalized Endpoint. Before authentication, login loads the Config and rejects
an existing Fleet whose non-empty Host differs from the login Endpoint; this
prevents one Fleet/User cache coordinate from holding credentials for different
servers. Same-Endpoint login merges the Fleet's existing TLS settings, Users,
and manually configured credentials.

After the password grant, login builds the Config update in memory, captures
the exact previous Fleet/User cache bytes, atomically saves the new cache, then
atomically saves the Config. If Config saving fails, an idempotent rollback
restores the previous cache bytes (including malformed data) or removes a cache
entry created by this login. The original save error is joined with any
rollback error. Success is printed only after both writes complete. The
supplied Password is never persisted.

`auth logout` asks KubeSphere to revoke the cached Access Token, deletes the
Fleet/User cache regardless of the remote result, and preserves configuration.

`auth whoami` is server-backed. It resolves the selected Context's User name,
builds an authenticated Fleet-level KubeSphere REST client, and reads:

```text
/kapis/iam.kubesphere.io/v1beta1/users/<username>
```

The command prints the returned `metadata.name` and
`metadata.annotations["iam.kubesphere.io/globalrole"]`. The request verifies
that the resolved credential can access the selected User resource, but the
endpoint is not an OAuth token-subject introspection API. `auth logout` reads
the cached Fleet/User Access Token and makes a best-effort, unscoped request to
`<fleet-endpoint>/oauth/logout`. It ignores remote errors and always attempts
to delete the local Fleet/User cache. It does not resolve or revoke configured
static credentials, perform a refresh, or apply Member Cluster routing.

## Cross-cluster routing

The selected Cluster changes request routing without changing resource command
syntax.

For standard Kubernetes discovery and resource requests, the Kubernetes getter
constructs the effective server as:

```text
<fleet-endpoint>/clusters/<cluster>
```

When no Cluster is selected, it uses the Fleet Endpoint directly. kubectl then
sends its standard `/api` and `/apis` paths to that effective server. Cobra
commands do not inspect resource types or rewrite individual resource paths.

KubeSphere-native APIs continue to use `/kapis/...` paths. A native operation
that accepts Cluster scope adds it through the KubeSphere request builder. For
example, kubeconfig retrieval uses the base KubeSphere Endpoint and applies the
selected Cluster to the request before calling the user kubeconfig API.

The raw `api` command is excluded from automatic Cluster routing. It uses the
caller-provided path unchanged, so a Cluster-scoped request must contain its
own `/clusters/<cluster>` prefix.

This boundary keeps command parsing independent of KubeSphere proxy topology:
the client layer selects the effective route, and the server remains
responsible for authenticating the KubeSphere Token and serving or proxying the
target API.

Pod log subresource, Metrics API, and supporting Core API requests use the same
effective server. For example:

```text
/clusters/<cluster>/api/v1/namespaces/<namespace>/pods/<pod>/log
/clusters/<cluster>/apis/metrics.k8s.io/v1beta1/namespaces/<namespace>/pods
/clusters/<cluster>/apis/metrics.k8s.io/v1beta1/nodes
```

The commands query one selected Cluster. They do not aggregate across Fleet
members.

## Generated kubeconfig

`config generate kubeconfig` is a KubeSphere-native request to:

```text
/kapis/resources.kubesphere.io/v1alpha2/users/<username>/kubeconfig
```

The username always comes from the selected Context, falling back to its User
map key. This identity requirement remains even when Endpoint or Token flags
override connection values. The explicit Cluster flag wins over the Context's
default Cluster.

The response body is copied unchanged to stdout. ksctl does not parse, merge,
store, or write it to `~/.kube/config`. The caller owns secure redirection and
file lifecycle.

## Plugin model

Before normal Cobra execution, the root asks the plugin dispatcher to handle an
unknown command path. The dispatcher:

1. ignores built-in commands, help, and shell-completion requests;
2. collects command words until the first flag;
3. maps dashes in words to underscores for executable lookup;
4. tries the longest `ksctl-<words-joined-with-dashes>` name first;
5. passes unmatched words, remaining arguments, and the inherited environment
   to the executable; and
6. replaces the current process with the plugin on Unix, or starts a child
   process on Windows.

Because lookup begins only after Cobra fails to find a built-in command,
plugins cannot replace or extend built-in command paths. Flags before a plugin
name are rejected so persistent flags cannot be mistaken for plugin input.
The built-in `logs` and `top` paths likewise cannot be replaced by
`ksctl-logs` or `ksctl-top` executables.

`plugin list` scans unique PATH directories in order and diagnoses candidates
for executable permissions, PATH shadowing, and conflicts with the built-in
command tree. Diagnostics return a non-zero status so scripts can treat
warnings as invalid plugin configuration.

Plugin executables are not clients injected into the ksctl process. They are
arbitrary external programs with the user's privileges, inherited environment,
and their own flag and connection handling.

## Security properties

- Interactive passwords are read without echo.
- `auth login` uses a Password only for its request and never persists it.
- A Password manually placed in the Config remains plaintext and is the user's
  responsibility.
- Config and token cache writes create parent directories with mode `0700`,
  write temporary files with mode `0600`, sync them, and atomically rename
  them over the destination.
- `config view` redacts Passwords, bearer Tokens, and TLS private key data by
  default; `--raw` is an explicit sensitive-output escape hatch.
- OAuth errors are constructed without embedding request credentials.
- Explicit Endpoint overrides cannot borrow credentials from a selected
  Context.
- A Fleet name cannot be rebound to another Endpoint by login.
- A returned login persistence failure leaves the Config unchanged and
  compensates the Token cache write.
- Generated kubeconfig and raw config output can contain credentials and must
  be protected by the caller.
- Raw API requests may mutate server state, and their request or response
  bodies may contain sensitive data. ksctl does not inspect, redact, or apply
  Extension lifecycle safeguards to them.
- Container logs are written to stdout and may contain application secrets;
  ksctl does not inspect or redact their content.
- Plugins are not inspected or sandboxed. Trust in a plugin is equivalent to
  trust in any other executable run by the user.

These properties reduce accidental persistence and disclosure; they do not
provide an encrypted credential store, operating-system keychain integration,
or a sandbox.

## Compatibility

- Go 1.26 or later is required by the module.
- KubeSphere 4.x is the supported server generation.
- `k8s.io/apimachinery`, `k8s.io/cli-runtime`, `k8s.io/client-go`,
  `k8s.io/kubectl`, and the indirect `k8s.io/metrics` dependency are aligned at
  v0.36.2.
- The standalone and kubectl-plugin binaries are built from the same module and
  command packages.

Kubernetes modules must remain on one aligned minor version because kubectl's
commands, Factory, Builder, printers, discovery, RESTMapper, and client
interfaces evolve together.

## Validation boundaries

The architecture is protected at several levels:

- command tests verify display-name propagation, registered commands and flags,
  version behavior, resource requests, member-Cluster routing, recursive
  logs/top examples, log subresource streaming, Metrics/Core requests, and
  Metrics API discovery fallback;
- config tests verify defaults, strict schema and type-contract rejection,
  serialization, redaction, migration boundaries, and filesystem permissions;
- authentication and cache tests verify precedence, login and refresh behavior,
  error disclosure, Fleet/User cache identity, encoding, and permissions;
- Kubernetes client tests verify TLS and Token mapping, Cluster Endpoint
  construction, in-memory client config, discovery caching and fallback, API
  path preservation, and RESTMapper behavior;
- KubeSphere connection tests verify native configuration, username resolution,
  Cluster validation, and injected transport ownership;
- raw API command tests verify path and method validation, body construction,
  unchanged query and response bytes, the absence of automatic Cluster
  routing, and HTTP-error body propagation;
- plugin tests verify longest matching, argument forwarding, dash conversion,
  built-in protection, and PATH diagnostics; and
- the normal build compiles `cmd/ksctl`.

User-visible command, configuration, authentication, or plugin changes must
update the [CLI guide](cli.md). Changes to package boundaries, routing,
dependency alignment, persistence, or security properties must update this
design document.
