# ksctl Client Packages Design

Date: 2026-07-14

## Summary

Replace the ambiguous `pkg/kubeclient` package with two explicit client
adapters under `pkg/client`: `kubernetes` for kubectl/client-go integration and
`kubesphere` for KubeSphere REST client construction. Keep OAuth protocol logic
in `pkg/auth` and inject the KubeSphere REST client factory into it.

Kubernetes and KubeSphere currently use the same endpoint, but they do not
share a transport. Each client stack owns its own HTTP behavior and can be
configured independently.

## Goals

- Make client packages describe the protocol/client they construct.
- Keep Kubernetes discovery, REST mapping, and `RESTClientGetter` behavior in
  `pkg/client/kubernetes`.
- Keep `pkg/client/kubesphere` limited to constructing
  `kubesphere.io/client-go/rest.Interface` values.
- Keep `/oauth/token`, grant forms, token response parsing, and secret-safe
  errors in `pkg/auth/oauth.go`.
- Allow a caller to inject an independent `http.Client` for KubeSphere REST
  calls.
- Allow a caller to inject an independent `http.RoundTripper` for Kubernetes
  REST calls.
- Preserve the command surface, config/cache formats, auth precedence,
  discovery fallback, and default network behavior.

## Non-Goals

- Do not move OAuth code into `pkg/client/kubesphere`.
- Do not add typed KubeSphere resource clients.
- Do not share an HTTP transport between the Kubernetes and KubeSphere stacks.
- Do not keep a compatibility package at `pkg/kubeclient`.
- Do not change the endpoint model or add a second endpoint.
- Do not change login, logout, token cache, or kubectl resource semantics.

## Target Structure

```text
pkg/
  auth/
    oauth.go
    provider.go
    resolver.go

  cache/token/

  client/
    kubernetes/
      getter.go
      discovery.go
      tls.go
    kubesphere/
      client.go

  cmd/
  config/
```

## Package Responsibilities

### `pkg/client/kubesphere`

This package owns a small REST client factory backed by
`kubesphere.io/client-go/rest`:

```go
type RESTClientFactory struct {
	httpClient *http.Client
}

func NewRESTClientFactory(httpClient *http.Client) *RESTClientFactory
func (f *RESTClientFactory) ForConfig(config *rest.Config) (rest.Interface, error)
```

`ForConfig` copies the supplied config before constructing a client so that
client-go defaulting cannot mutate caller-owned state.

When `httpClient` is nil, the factory calls
`rest.UnversionedRESTClientFor`, allowing KubeSphere client-go to construct its
normal TLS-aware HTTP client from the supplied config. When an HTTP client is
injected, it calls `rest.UnversionedRESTClientForConfigAndClient`; the injected
HTTP client owns its transport, timeout, redirect, and cookie behavior, as
defined by KubeSphere client-go. The factory clones the injected HTTP client
and applies `rest.Config.WrapTransport` to that clone, allowing consumer-owned
security middleware without mutating or sharing the caller's client state.

The package contains no OAuth path, form, grant, response, cache, login, or
logout logic.

### `pkg/auth`

`oauth.go` defines the narrow factory interface it consumes:

```go
type RESTClientFactory interface {
	ForConfig(*kubesphererest.Config) (kubesphererest.Interface, error)
}
```

An `OAuth` service receives that factory at construction and exposes password
and refresh grants:

```go
func NewOAuth(factory RESTClientFactory) *OAuth
func (o *OAuth) Login(context.Context, TokenRequestOptions) (token.Response, error)
func (o *OAuth) Refresh(context.Context, TokenRequestOptions) (token.Response, error)
```

The OAuth service builds a KubeSphere REST config from endpoint, user-agent,
timeout, and TLS options, asks the factory for a generic REST client, and sends
the form with:

```go
client.Post().
	AbsPath("/oauth/token").
	SetHeader("Content-Type", "application/x-www-form-urlencoded").
	Body(strings.NewReader(form.Encode())).
	Stream(ctx)
```

The absolute OAuth path remains in `auth`, not in the KubeSphere client
factory. Request bodies remain `io.Reader` values and responses remain streams
so credentials are not exposed by verbose REST logging.

OAuth installs a transport wrapper that closes and replaces non-2xx response
bodies before KubeSphere client-go transforms or logs them. The command returns
an operation-only authentication error, so a server that echoes submitted
credentials cannot expose them through either the returned error or verbosity-8
REST logs.

`Provider` consumes only a refresh-capable interface. It does not construct an
HTTP client or KubeSphere REST client. If no refresher is supplied and a cached
token requires refresh, it returns the existing login-required error.

### `pkg/client/kubernetes`

This package is the renamed `pkg/kubeclient`. It retains:

- Kubernetes `rest.Config` construction.
- kubectl `RESTClientGetter` implementation.
- cached discovery and REST mapper construction.
- KubeSphere discovery fallback behavior.
- ksctl-to-Kubernetes TLS conversion.

Construction uses explicit dependencies:

```go
type Dependencies struct {
	TokenProvider auth.TokenProvider
	Transport     http.RoundTripper
}

func NewRESTClientGetter(options *Options, dependencies Dependencies) *RESTClientGetter
```

When no transport is injected, the generated Kubernetes `rest.Config` keeps
the resolved TLS settings and client-go builds its standard transport. When a
transport is injected, it is assigned to `rest.Config.Transport` and the REST
config's TLS fields are left empty because client-go forbids combining a
custom transport with CA, client-certificate, or insecure TLS options. The
injected transport therefore owns Kubernetes TLS behavior. The raw kubeconfig
view still represents the user's resolved cluster metadata.

The Kubernetes transport and KubeSphere HTTP client are separate dependencies,
even when both target the same endpoint.

## Construction And Data Flow

`pkg/cmd.NewRootCommand` is the composition root:

1. Create `client/kubesphere.RESTClientFactory` with its own optional HTTP
   client.
2. Create `auth.OAuth` from that factory.
3. Create `auth.Provider` with the OAuth service as token refresher.
4. Pass the OAuth service to `auth login`.
5. Create `client/kubernetes.RESTClientGetter` with the provider and its own
   optional transport.
6. Pass the getter to kubectl's command factory.

Endpoint, TLS, and timeout values remain lazy because Cobra parses flags after
the root command is constructed. The OAuth service creates a REST config per
login or refresh call; no endpoint-bound REST client is created eagerly.

## Error Handling

- Nil KubeSphere configs and missing OAuth factories return explicit errors.
- REST client construction errors retain their cause and operation context.
- OAuth request failures continue to hide usernames, passwords, token values,
  and server response bodies; non-2xx bodies are removed at the transport
  boundary before REST response logging.
- Provider refresh failures continue to become `login required` errors.
- A custom Kubernetes transport is never combined with client-go TLS fields.

## Testing

- `pkg/client/kubesphere` tests prove injected HTTP clients are used, default
  HTTP construction still works, config input is not mutated, and nil config
  is rejected.
- `pkg/auth` tests prove the factory is called with the expected config,
  `/oauth/token` is selected with `AbsPath`, both grants work, and credentials
  remain absent from logs/errors.
- `pkg/client/kubernetes` tests preserve all previous getter/discovery cases
  and add an injected-transport case proving the transport is used and TLS
  fields are cleared from the REST config.
- `pkg/cmd` tests prove root composition still supports login, refresh-backed
  resource commands, logout, get, and describe.
- Whole-repository tests and both command builds provide final verification.

## Migration

- Move `pkg/kubeclient/{getter,discovery,tls}.go` and their tests to
  `pkg/client/kubernetes` and change the package name to `kubernetes`.
- Replace imports of `github.com/kubesphere/ksctl/pkg/kubeclient` with
  `github.com/kubesphere/ksctl/pkg/client/kubernetes`.
- Add `pkg/client/kubesphere` and inject its factory through the composition
  root.
- Remove the old `pkg/kubeclient` directory without an alias or forwarding
  layer.
