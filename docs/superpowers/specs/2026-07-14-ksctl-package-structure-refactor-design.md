# ksctl Package Structure Refactor

Date: 2026-07-14

## Summary

Refactor the `pkg` tree so each package has one clear responsibility. Move
Cobra command construction from `pkg/cli` to `pkg/cmd`, expose authentication
only through `ksctl auth login` and `ksctl auth logout`, move token persistence
to `pkg/cache/token`, and remove OAuth and token-cache lifecycle code from
the client packages.

The refactor keeps the existing config and cache file formats, kubectl-native
`get` and `describe` behavior, connection flags, and TLS behavior. It does not
preserve the old top-level `ksctl login` and `ksctl logout` commands or the old
internal Go import paths.

## Goals

- Make the package name and package contents describe the same responsibility.
- Keep Cobra command construction and user-facing output in `pkg/cmd`.
- Keep connection and credential resolution, OAuth requests, and token
  acquisition policy in `pkg/auth`.
- Keep token models, expiry checks, paths, and persistence in
  `pkg/cache/token`.
- Keep `pkg/client/kubernetes` limited to adapting an authenticated connection
  to kubectl's `RESTClientGetter`, discovery client, and REST mapper interfaces.
- Keep `pkg/client/kubesphere` limited to constructing generic KubeSphere REST
  clients.
- Preserve the existing on-disk config and token cache formats and paths.
- Preserve kubectl-native `get` and `describe` behavior.

## Non-Goals

- No compatibility aliases for top-level `login` or `logout`.
- No compatibility packages at `pkg/cli` or `pkg/token`.
- No compatibility package at `pkg/kubeclient`.
- No config format migration.
- No change to the token cache path or payload.
- No server-side token revocation.
- No interactive login, browser login, keychain integration, or token
  encryption.
- No changes to kubectl resource command behavior or additional resource
  commands.

## Target Repository Structure

```text
cmd/
  ksctl/main.go
  kubectl-ks/main.go

pkg/
  cmd/
    root.go
    root_test.go
    auth.go
    auth_test.go
    config.go
    config_test.go
    version.go
    resource_commands_test.go

  auth/
    resolver.go
    resolver_test.go
    oauth.go
    oauth_test.go
    provider.go
    provider_test.go

  cache/
    token/
      cache.go
      cache_test.go

  config/
    types.go
    loader.go
    loader_test.go

  client/
    kubernetes/
      getter.go
      getter_test.go
      discovery.go
      tls.go
    kubesphere/
      client.go
      client_test.go
```

## Package Responsibilities

### `pkg/cmd`

`pkg/cmd` owns the Cobra command tree, flag binding, argument validation, and
user-facing output. It constructs the kubectl `get` and `describe` commands but
does not implement OAuth requests, token persistence, token refresh, or
Kubernetes REST clients.

Both binary entrypoints import this package and call `cmd.NewRootCommand`.

### `pkg/auth`

`pkg/auth/resolver.go` resolves endpoint, context, cluster, workspace, TLS
settings, and configured credential sources from flags, environment variables,
and config.

`pkg/auth/oauth.go` owns the KubeSphere `/oauth/token` password and refresh
grant requests.

`pkg/auth/provider.go` owns the policy for selecting a token, reading cached
credentials, refreshing expired credentials, falling back to configured
legacy credentials, and returning a final bearer token to
`pkg/client/kubernetes`.

OAuth requests continue to send form data through an `io.Reader` and consume
responses through `Stream(ctx)`. Passwords, access tokens, and refresh tokens
must not appear in output, errors, or verbose transport logs. A consumer-owned
transport wrapper replaces non-2xx OAuth response bodies before the REST client
can log them.

### `pkg/cache/token`

`pkg/cache/token` owns the OAuth token response and cache entry models, expiry
calculation, validity checks, filesystem-safe context names, default cache
paths, and cache load, save, and delete operations.

The package continues to use:

```text
~/.ksctl/cache/tokens/<context>.json
```

The cache directory remains mode `0700`, and token files remain mode `0600`.

### `pkg/config`

`pkg/config` remains responsible for the ksctl config model and config file
load and save operations. The config path and serialized field names do not
change.

### `pkg/client/kubernetes`

`pkg/client/kubernetes` owns only the kubectl client adapter:

- Kubernetes `rest.Config` construction from an authenticated connection.
- `ToRESTConfig`.
- `ToDiscoveryClient`.
- `ToRESTMapper`.
- `ToRawKubeConfigLoader`.
- KubeSphere discovery fallback behavior.
- Conversion from ksctl TLS settings to Kubernetes TLS settings.

It does not contain Login, Refresh, OAuth form construction, cache paths,
cache reads, cache writes, or expiry policy.

An optional injected `http.RoundTripper` owns Kubernetes transport and TLS
behavior. Without one, the adapter preserves the resolved TLS settings and
lets client-go construct its default transport.

### `pkg/client/kubesphere`

`pkg/client/kubesphere` constructs generic
`kubesphere.io/client-go/rest.Interface` values. It accepts an optional
`*http.Client`; when absent, KubeSphere client-go constructs the default HTTP
client from its REST config. When present, the factory clones the HTTP client
and applies config transport wrappers without mutating the caller's instance.

This package contains no OAuth path, grant form, login, refresh, token cache,
or authentication policy. Those remain in `pkg/auth`.

## Dependency Direction

The dependency direction remains acyclic:

```text
pkg/cmd
  |-- pkg/auth
  |-- pkg/cache/token
  |-- pkg/config
  |-- pkg/client/kubesphere
  `-- pkg/client/kubernetes
          `-- pkg/auth
                  |-- pkg/config
                  `-- pkg/cache/token
```

`pkg/cache/token` and `pkg/config` do not depend on higher-level command,
authentication, or client packages.

## Command Surface

Authentication is exposed only through:

```bash
ksctl auth login ENDPOINT --username USERNAME --password PASSWORD
ksctl auth logout [CONTEXT]
```

The shared root command also makes the equivalent kubectl plugin forms
available:

```bash
kubectl ks auth login ENDPOINT --username USERNAME --password PASSWORD
kubectl ks auth logout [CONTEXT]
```

The old top-level commands are removed:

```text
ksctl login
ksctl logout
```

No hidden aliases are registered.

## Authentication Flows

### Login

1. `pkg/cmd` validates the endpoint, username, password, and optional context.
2. `auth.OAuth.Login` obtains a generic KubeSphere REST client from the
   injected factory and sends the password grant to `ENDPOINT/oauth/token`.
3. The command loads and updates the config in memory.
4. It saves the cluster, user, context, and `currentContext` metadata without a
   password or bearer token.
5. It saves the complete OAuth response in the context token cache.
6. It prints a success message only after the required writes succeed.

The default context name continues to be derived from the endpoint host and
port with filesystem-unsafe characters replaced.

### Authenticated Resource Commands

The Kubernetes client adapter asks the authentication layer for an
authenticated connection. Token precedence is:

```text
--token
KS_TOKEN
valid cached access token
refreshed cached token
selected user.bearerTokenFile
selected user.bearerToken
login required
```

Explicit flag and environment tokens bypass the cache. A missing cache may
fall back to configured legacy token sources. An expired cache with a refresh
token is refreshed and overwritten. If that refresh fails, the provider
returns `login required` and does not silently use a legacy configured token.

This precedence corrects the current implementation so it matches the
existing token-cache design.

### Logout

1. Resolve the requested context, or use `currentContext` when omitted.
2. Delete the corresponding token cache file. A missing file is successful.
3. Clear legacy `bearerToken` and `bearerTokenFile` fields for the selected
   context's user.
4. Preserve cluster, user, context, and `currentContext` entries.
5. Save the updated config and print the success message.

Logout does not call a server-side revocation endpoint.

## Error Handling

- `pkg/cmd` returns specific errors for missing endpoint, username, password,
  or context input.
- OAuth failures do not include submitted credentials or response bodies.
- A missing token cache continues to the configured legacy credential
  fallback.
- Invalid cache JSON, permission failures, and other cache read errors return
  the real cache error rather than being presented as an unauthenticated
  state.
- An expired cache whose refresh fails returns
  `error: login required for context "<context>"`.
- Config and cache write failures are returned and suppress the success
  message.
- Logout treats a missing cache file as success but returns other filesystem
  errors.

## On-Disk Compatibility

The refactor does not migrate or rewrite existing files merely because the
binary starts. It preserves:

- `~/.ksctl/config.yaml`.
- Existing config API version, kind, and field names.
- `~/.ksctl/cache/tokens/<context>.json`.
- Existing token cache JSON fields.
- Config and cache permissions.

Tests that exercise default paths use a temporary home directory and do not
touch a developer's real ksctl files.

## Implementation Strategy

The implementation uses test-driven migration:

1. Add failing command tests for the `auth` parent command and removal of
   top-level `login` and `logout`.
2. Move the command package and update both binary entrypoints.
3. Move token models and persistence into `pkg/cache/token` and update tests.
4. Move OAuth requests into `pkg/auth/oauth.go` with their focused tests.
5. Add provider tests for token precedence, cache validity, refresh, legacy
   fallback, invalid cache handling, and refresh failure.
6. Move the kubectl adapter from `pkg/kubeclient` to
   `pkg/client/kubernetes` and add independent Transport injection.
7. Add `pkg/client/kubesphere`, inject its REST factory into OAuth, and keep
   OAuth paths and grants in `pkg/auth`.
8. Update documentation and remove the old packages.

## Verification

Focused tests cover:

- `auth login` and `auth logout` command registration and behavior.
- Absence of top-level `login` and `logout`.
- Config metadata and token cache writes after login.
- Password and bearer token absence from config files and output.
- Logout cleanup and preservation of context metadata.
- OAuth password and refresh grant request shapes.
- Secret-safe request and response handling.
- Token precedence and explicit-token cache bypass.
- Valid cache reuse, expired cache refresh, cache overwrite, legacy fallback,
  invalid cache errors, and refresh failure.
- Token cache payload, expiry calculation, path safety, and permissions.
- Kubernetes REST config mapping and discovery behavior.
- Existing kubectl-native `get` and `describe` behavior.

Final verification commands are:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./... -count=1
git diff --check
rg 'pkg/(cli|token)|package cli' --glob '*.go'
rg 'pkg/kubeclient|package kubeclient' --glob '*.go'
rg 'oauth/token|func Login|func Refresh' pkg/client --glob '*.go'
```

All final `rg` commands must return no Go source matches.
