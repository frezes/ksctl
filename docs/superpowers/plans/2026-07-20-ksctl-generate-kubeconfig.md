# ksctl Generate Kubeconfig Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ksctl config generate kubeconfig` to fetch the current logged-in user's kubeconfig from the KubeSphere API and write it unchanged to stdout.

**Architecture:** Share connection flag values through `pkg/client.Options`, while keeping protocol construction separate. Kubernetes commands use the Kubernetes getter; kubeconfig generation uses a native KubeSphere getter and KubeSphere request builder, including its `Cluster` scope method.

**Tech Stack:** Go 1.26, Cobra, KubeSphere client-go REST, Kubernetes client-go for existing kubectl commands, `httptest`, Go `testing`.

## Global Constraints

- Write successful response bytes to stdout unchanged; never write or merge a local kubeconfig.
- Resolve username from current or explicit context, using `user.username` then the user map key.
- Resolve cluster as `--cluster > context.defaultCluster`; empty means the unscoped host.
- Use native KubeSphere `rest.Config`, client construction, and request cluster scope for this API.
- Do not derive this request's transport from Kubernetes `rest.Config`.
- Validate cluster and username before authentication or an API request.
- Preserve token refresh, TLS, timeout, user-agent, precedence, and redaction behavior.

---

### Task 1: Add Shared Options and a Native KubeSphere Getter

**Files:**
- Create: `pkg/client/options.go`
- Create: `pkg/client/kubesphere/connection/getter.go`
- Test: `pkg/client/kubesphere/connection/getter_test.go`
- Modify: `pkg/client/kubernetes/getter.go`

**Interfaces:**
- Produces: `client.Options` shared by both protocol getters.
- Produces: `kubesphere.RESTClientGetter.ToRESTConfig() (*kubesphererest.Config, error)`.
- Produces: `KubeSphereCluster() (string, error)` and `KubeSphereUsername() (string, error)`.

- [x] **Step 1: Write failing native-config tests**

Test native KubeSphere host, bearer token, TLS, timeout, user agent, current and
explicit context username resolution, user-key fallback, cluster resolution,
broken references, and rejection of invalid clusters before token resolution.

- [x] **Step 2: Verify RED**

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubesphere/connection -run TestRESTClientGetter -count=1
```

Expected: failure because the shared options package and getter do not exist.

- [x] **Step 3: Implement the native KubeSphere getter**

Load ksctl config, call `auth.Resolve`, validate cluster, resolve the token, and
populate `kubesphere.io/client-go/rest.Config` directly. Keep the endpoint
unscoped and expose cluster separately for the request builder.

- [x] **Step 4: Verify GREEN**

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubesphere/connection -run TestRESTClientGetter -count=1
```

---

### Task 2: Route the Command Through the KubeSphere Client

**Files:**
- Modify: `pkg/cmd/config.go`
- Modify: `pkg/cmd/root.go`
- Test: `pkg/cmd/config_test.go`

**Interfaces:**
- Consumes: the native KubeSphere getter interface from Task 1.
- Produces: `ksctl config generate kubeconfig`.

- [x] **Step 1: Preserve command behavior tests**

Cover explicit cluster override, context default cluster, unscoped host,
username fallback, byte-exact stdout, invalid input, server/output errors, and
refresh-backed authentication.

- [x] **Step 2: Construct the native KubeSphere request**

```go
client, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(restConfig)
request := client.Get()
if cluster != "" {
	request.Cluster(cluster)
}
raw, err := request.AbsPath(
	"/kapis/resources.kubesphere.io/v1alpha2/users", username, "kubeconfig",
).Do(ctx).Raw()
```

The command must not import Kubernetes REST or call Kubernetes
`rest.HTTPClientFor`.

- [x] **Step 3: Wire protocol-specific getters at the root**

Create one Kubernetes getter for version/kubectl commands and one KubeSphere
getter for kubeconfig generation. Give both the same `client.Options` pointer
and token provider.

- [x] **Step 4: Run command and client package tests**

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubesphere/connection ./pkg/client/kubernetes ./pkg/cmd -count=1
```

---

### Task 3: Document and Verify

**Files:**
- Modify: `README.md`
- Modify: `README_zh.md`
- Modify: `docs/superpowers/specs/2026-07-20-ksctl-generate-kubeconfig-design.md`

- [x] **Step 1: Document stdout, cluster precedence, and file permissions**

Include `umask 077` before redirect examples because kubeconfig contains
credentials.

- [x] **Step 2: Run final verification**

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./... -count=1
env GOCACHE=/private/tmp/ksctl-go-build-cache go test -race ./pkg/client/kubesphere/connection ./pkg/client/kubernetes ./pkg/cmd -count=1
env GOCACHE=/private/tmp/ksctl-go-build-cache go build ./cmd/ksctl ./cmd/kubectl-ks
git diff --check
```
