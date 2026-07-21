# ksctl Design

This document describes the current architecture of ksctl. For command syntax
and workflows, see the [CLI guide](cli.md). Historical specifications under
`docs/superpowers/` record individual decisions and implementation phases but
are not the current architecture reference.

## Goals

- Provide one CLI for inspecting KubeSphere 4.x resources and Kubernetes
  resources exposed through KubeSphere.
- Preserve familiar kubectl `get` and `describe` syntax, discovery, selection,
  printing, watching, describing, and error behavior.
- Keep the built-in resource surface read-only.
- Support interactive use and explicit, predictable automation.
- Use the KubeSphere API Endpoint and credentials without reading or changing
  the user's kubeconfig.
- Expose identical built-in behavior through the standalone `ksctl` binary and
  the `kubectl ks` plugin entrypoint.
- Keep configuration, authentication, API clients, and command wiring in
  focused packages with testable boundaries.

## Non-goals

- Built-in create, update, edit, patch, delete, apply, or other resource
  mutation commands
- A local reimplementation or fork of kubectl resource parsing, printers,
  watchers, or Describers
- Reading, merging, or writing `~/.kube/config`
- A static public registry of the resources a KubeSphere server may expose
- Auditing or sandboxing executable plugins
- KubeSphere 3.x compatibility

Executable plugins are outside the built-in command surface and may implement
operations that are not read-only. They run as independent programs under the
user's authority.

## Command architecture

There are two small executable entrypoints:

```text
cmd/ksctl/main.go       -> NewRootCommandWithArgs
cmd/kubectl-ks/main.go  -> NewKubectlPluginCommandWithArgs
```

Both constructors delegate to the same root-command builder in `pkg/cmd`.
`kubectl-ks` adds Cobra's display-name annotation so help, examples, and errors
show `kubectl ks`; command behavior and options otherwise remain shared.

The root owns KubeSphere connection flags and constructs these command groups:

- ksctl-owned `auth`, `config`, `plugin`, and `version` commands;
- kubectl-owned `get` and `describe` commands; and
- Cobra-provided help, completion, and shell-completion commands.

Commands use injected input, output, and error streams. The executable
entrypoints connect those streams to the process standard streams and print a
single returned error before exiting non-zero. The root enables
`SilenceUsage` and `SilenceErrors` so execution failures do not also print
usage or duplicate the error.

Before Cobra executes an unknown command, the `WithArgs` constructors give the
plugin dispatcher an opportunity to resolve it to a `ksctl-*` executable.

## Resource command pipeline

ksctl constructs kubectl v0.36.2's commands directly:

```text
Cobra command
  -> kubectl get or describe
  -> kubectl Factory
  -> ksctl Kubernetes RESTClientGetter
  -> discovery and RESTMapper
  -> Kubernetes REST client
  -> KubeSphere Endpoint
```

`get.NewCmdGet` and `describe.NewCmdDescribe` receive the root display name, a
shared `cmdutil.Factory`, and the process streams. ksctl changes their examples
to use the active entrypoint but does not wrap their execution pipeline or
post-process successful output.

This delegates resource arguments, selectors, filename inputs, pagination,
watching, table negotiation, printers, built-in Describers, generic describe
fallback, Events, and most error behavior to the pinned kubectl implementation.
It also means command-specific capabilities evolve only when the aligned
Kubernetes dependencies are intentionally upgraded.

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

## Client boundaries

### Shared options

`pkg/client.Options` is the command-to-client input model. It carries Endpoint,
Token, Context, Cluster, Namespace, request timeout, config path, user agent,
and internal connection switches. Cobra binds the public root flags to one
Options value shared by both client getters.

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

Missing files load as an initialized empty Config. Loading fills absent API
version, kind, Fleet map, and Context map defaults, but it does not migrate
legacy root-level Users or earlier Cluster models.

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

`auth login` is the explicit cache-creation path. It resolves interactive or
fully supplied input, performs the password grant, then saves non-secret Fleet,
User, and Context metadata plus the complete OAuth response. It never stores
the supplied Password. `auth logout` deletes the Fleet/User cache only and
preserves configuration.

`auth whoami` is server-backed. It resolves the selected Context's User name,
builds an authenticated Fleet-level KubeSphere REST client, and reads:

```text
/kapis/iam.kubesphere.io/v1beta1/users/<username>
```

The command prints the returned `metadata.name` and
`metadata.annotations["iam.kubesphere.io/globalrole"]`. The request verifies
that the resolved credential can access the selected User resource, but the
endpoint is not an OAuth token-subject introspection API. Member Cluster
routing does not apply. In contrast, `auth logout` remains local-only and does
not revoke the server-side token.

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

This boundary keeps command parsing independent of KubeSphere proxy topology:
the client layer selects the effective route, and the server remains
responsible for authenticating the KubeSphere Token and serving or proxying the
target API.

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
- Generated kubeconfig and raw config output can contain credentials and must
  be protected by the caller.
- Plugins are not inspected or sandboxed. Trust in a plugin is equivalent to
  trust in any other executable run by the user.

These properties reduce accidental persistence and disclosure; they do not
provide an encrypted credential store, operating-system keychain integration,
or a sandbox.

## Compatibility

- Go 1.26 or later is required by the module.
- KubeSphere 4.x is the supported server generation.
- `k8s.io/apimachinery`, `k8s.io/cli-runtime`, `k8s.io/client-go`, and
  `k8s.io/kubectl` are aligned at v0.36.2.
- The standalone and kubectl-plugin binaries are built from the same module and
  command packages.

Kubernetes modules must remain on one aligned minor version because kubectl's
commands, Factory, Builder, printers, discovery, RESTMapper, and client
interfaces evolve together.

## Validation boundaries

The architecture is protected at several levels:

- command tests verify both display names, registered commands and flags,
  version behavior, resource requests, and member-Cluster routing;
- config tests verify defaults, serialization, redaction, migration boundaries,
  and filesystem permissions;
- authentication and cache tests verify precedence, login and refresh behavior,
  error disclosure, Fleet/User cache identity, encoding, and permissions;
- Kubernetes client tests verify TLS and Token mapping, Cluster Endpoint
  construction, in-memory client config, discovery caching and fallback, API
  path preservation, and RESTMapper behavior;
- KubeSphere connection tests verify native configuration, username resolution,
  Cluster validation, and injected transport ownership;
- plugin tests verify longest matching, argument forwarding, dash conversion,
  built-in protection, PATH diagnostics, and both entrypoints; and
- the build compiles both `cmd/ksctl` and `cmd/kubectl-ks`.

User-visible command, configuration, authentication, or plugin changes must
update the [CLI guide](cli.md). Changes to package boundaries, routing,
dependency alignment, persistence, or security properties must update this
design document.
