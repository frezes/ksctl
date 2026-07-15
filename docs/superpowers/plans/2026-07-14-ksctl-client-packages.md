# ksctl Client Packages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split client construction into `pkg/client/kubernetes` and `pkg/client/kubesphere`, inject independent network dependencies, and keep OAuth behavior in `pkg/auth`.

**Architecture:** Add a KubeSphere REST factory that optionally consumes an injected `*http.Client`, then inject that factory into an `auth.OAuth` service. Move the kubectl adapter to `pkg/client/kubernetes` and inject an independent `http.RoundTripper` through explicit dependencies. Assemble both stacks in `pkg/cmd` without sharing a transport.

**Tech Stack:** Go 1.26, KubeSphere client-go REST, Kubernetes client-go/cli-runtime/kubectl v0.36.2, Cobra, `net/http`, `httptest`.

## Global Constraints

- `pkg/client/kubesphere` contains only generic REST client construction; OAuth remains in `pkg/auth/oauth.go`.
- Kubernetes and KubeSphere use the same resolved endpoint but do not share a transport or HTTP client.
- Preserve `ksctl auth login`, `ksctl auth logout`, token precedence, config/cache formats, secret-safe OAuth streaming, kubectl command behavior, and discovery fallback.
- Remove `pkg/kubeclient` without a compatibility layer.
- An injected Kubernetes transport owns TLS; do not combine it with client-go TLS fields.
- An injected KubeSphere HTTP client takes precedence over REST-config transport settings, matching KubeSphere client-go behavior.

---

### Task 1: Add The KubeSphere REST Client Factory

**Files:**
- Create: `pkg/client/kubesphere/client_test.go`
- Create: `pkg/client/kubesphere/client.go`

**Interfaces:**
- Produces: `NewRESTClientFactory(httpClient *http.Client) *RESTClientFactory`
- Produces: `(*RESTClientFactory).ForConfig(config *kubesphererest.Config) (kubesphererest.Interface, error)`

- [ ] **Step 1: Write failing factory tests**

Add tests that construct a factory with a recording `http.Client`, call
`ForConfig`, perform `client.Get().AbsPath("/readyz").DoRaw(ctx)`, and assert the
injected round tripper observed `/readyz`. Add tests asserting a nil config
returns an error and a caller-owned config retains a nil serializer.

- [ ] **Step 2: Verify the new package fails**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubesphere -count=1
```

Expected: FAIL because `NewRESTClientFactory` is undefined.

- [ ] **Step 3: Implement the minimum factory**

Implement `RESTClientFactory` with an optional `*http.Client`. Copy configs
with `kubesphererest.CopyConfig`. Use `UnversionedRESTClientFor` when no client
is injected; otherwise set the copied config's serializer from KubeSphere's
scheme and call `UnversionedRESTClientForConfigAndClient`.

- [ ] **Step 4: Verify the factory tests pass**

Run the Task 1 test command again. Expected: PASS.

### Task 2: Inject The REST Factory Into OAuth

**Files:**
- Modify: `pkg/auth/oauth_test.go`
- Modify: `pkg/auth/oauth.go`
- Modify: `pkg/auth/provider_test.go`
- Modify: `pkg/auth/provider.go`

**Interfaces:**
- Consumes: `RESTClientFactory.ForConfig(*kubesphererest.Config)`
- Produces: `NewOAuth(factory RESTClientFactory) *OAuth`
- Produces: `(*OAuth).Login(context.Context, TokenRequestOptions)`
- Produces: `(*OAuth).Refresh(context.Context, TokenRequestOptions)`
- Produces: `ProviderOptions.Refresher TokenRefresher`

- [ ] **Step 1: Rewrite OAuth tests against the desired injected API**

Create an OAuth service with a recording factory. Keep the existing password,
refresh, invalid-response, and secret-safety assertions. Assert the factory
receives the endpoint, timeout, user-agent, and TLS config, and assert the
request path is `/oauth/token` even though the REST config has no OAuth API
path.

- [ ] **Step 2: Verify OAuth tests fail**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/auth -run 'TestLogin|TestRefresh|TestTokenRequest|TestOAuth' -count=1
```

Expected: FAIL because `NewOAuth` and the injected methods are undefined.

- [ ] **Step 3: Implement the OAuth service**

Move direct REST construction behind the injected factory. Keep grant form
construction and token parsing in `oauth.go`, call
`Post().AbsPath("/oauth/token")`, and preserve `Body(io.Reader)` plus
`Stream(ctx)`.

- [ ] **Step 4: Add a failing provider refresh-dependency test**

Update the expired-cache test to pass a recording `TokenRefresher` through
`ProviderOptions.Refresher` and assert it receives the resolved endpoint and
cached refresh token. Add a test that a nil refresher returns login-required
without panicking.

- [ ] **Step 5: Implement provider injection and verify auth**

Store the refresher on `Provider`, call it only for expired cached entries with
a refresh token, and preserve current fallback/error behavior.

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/auth -count=1
```

Expected: PASS.

### Task 3: Move The Kubernetes Adapter And Inject Its Transport

**Files:**
- Move: `pkg/kubeclient/getter_test.go` to `pkg/client/kubernetes/getter_test.go`
- Move: `pkg/kubeclient/getter.go` to `pkg/client/kubernetes/getter.go`
- Move: `pkg/kubeclient/discovery.go` to `pkg/client/kubernetes/discovery.go`
- Move: `pkg/kubeclient/tls.go` to `pkg/client/kubernetes/tls.go`
- Delete: `pkg/kubeclient`

**Interfaces:**
- Produces: `kubernetes.Dependencies{TokenProvider auth.TokenProvider, Transport http.RoundTripper}`
- Produces: `kubernetes.NewRESTClientGetter(options *Options, dependencies Dependencies) *RESTClientGetter`

- [ ] **Step 1: Move the getter tests first and define injected transport behavior**

Change the test package to `kubernetes`, update constructor calls to use a
`Dependencies` value, and add a recording transport test. The test asserts
`ToRESTConfig()` contains the injected transport, contains no TLS config, and
an actual discovery request passes through the injected transport.

- [ ] **Step 2: Verify the moved tests fail**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubernetes -count=1
```

Expected: FAIL because the moved implementation and `Dependencies` are absent.

- [ ] **Step 3: Move the implementation and add dependencies**

Move the three production files, change their package to `kubernetes`, add the
`Dependencies` type, and store the optional transport on the getter. Preserve
the current default token provider when none is supplied. During REST config
construction, use resolved TLS only when transport is nil; otherwise set the
custom transport and leave REST TLS fields empty.

- [ ] **Step 4: Verify Kubernetes behavior**

Run the Task 3 test command again. Expected: PASS, including all existing
discovery fallback cases and the new transport case.

- [ ] **Step 5: Verify the legacy package is gone**

Run:

```bash
test ! -d pkg/kubeclient
rg 'pkg/kubeclient|package kubeclient' --glob '*.go' .
```

Expected: the directory assertion succeeds and `rg` returns no matches after
Task 4 updates callers.

### Task 4: Assemble Both Client Stacks In The Root Command

**Files:**
- Modify: `pkg/cmd/auth.go`
- Modify: `pkg/cmd/root.go`
- Modify: `pkg/cmd/auth_test.go`
- Modify: `pkg/cmd/root_test.go`
- Modify: `pkg/cmd/resource_commands_test.go`

**Interfaces:**
- Consumes: `auth.NewOAuth`, `auth.ProviderOptions.Refresher`
- Consumes: `kubesphere.NewRESTClientFactory`
- Consumes: `kubernetes.NewRESTClientGetter`

- [ ] **Step 1: Update command tests for injected OAuth composition**

Keep the existing end-to-end login test and resource-command tests. Add a root
test that obtains a token through an expired-cache refresh and then performs a
resource request, proving both client stacks are composed without moving OAuth
into either command or Kubernetes packages.

- [ ] **Step 2: Verify command compilation fails against the new APIs**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/cmd -count=1
```

Expected: FAIL until imports, constructors, and login method calls are updated.

- [ ] **Step 3: Implement root dependency assembly**

Construct a KubeSphere REST factory, construct `auth.OAuth`, pass it to the
login command and provider, then construct the Kubernetes getter with a
`Dependencies` value. Both network dependencies default to nil and remain
independent.

- [ ] **Step 4: Verify command behavior**

Run the Task 4 test command again. Expected: PASS.

### Task 5: Update Documentation And Verify The Repository

**Files:**
- Modify: `README.md`
- Modify: `README_zh.md`
- Modify: `docs/superpowers/specs/2026-07-14-ksctl-package-structure-refactor-design.md`

**Interfaces:**
- Documents: final `pkg/client/kubernetes` and `pkg/client/kubesphere` layout.

- [ ] **Step 1: Update package layout references**

Replace `pkg/kubeclient` documentation with the two client packages. State
that OAuth remains in `pkg/auth` and both client stacks use independent network
dependencies.

- [ ] **Step 2: Format and run focused tests**

Run:

```bash
gofmt -w pkg/auth pkg/client pkg/cmd
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/auth ./pkg/client/kubesphere ./pkg/client/kubernetes ./pkg/cmd -count=1
```

Expected: formatting succeeds and all focused packages pass.

- [ ] **Step 3: Run full verification**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./... -count=1
go build ./cmd/...
git diff --check
rg 'pkg/kubeclient|package kubeclient' --glob '*.go' .
rg 'oauth/token|func Login|func Refresh' pkg/client --glob '*.go'
```

Expected: tests and builds pass, diff check is empty, and both searches return
no matches.

- [ ] **Step 4: Review the final diff and commit**

Review package boundaries, secret handling, injected-client semantics, and
unrelated changes. Commit the implementation after fresh verification.
