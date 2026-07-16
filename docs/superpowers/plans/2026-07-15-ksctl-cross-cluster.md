# ksctl Cross-Cluster Requests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route every resource-bearing ksctl command through `/clusters/<cluster>` when `--cluster` or the current context's `defaultCluster` selects a target cluster.

**Architecture:** Resolve the target cluster once in `RESTClientGetter`, then join it onto the final REST host after authentication completes. Kubectl-backed commands inherit that host; `version` uses the same scoped host and stops adding a second cluster prefix through the KubeSphere request builder.

**Tech Stack:** Go 1.25, Cobra, Kubernetes client-go and kubectl, KubeSphere client-go, standard-library `net/url`, Go `testing` and `httptest`.

## Global Constraints

- `contexts.<name>.cluster` continues to select the KubeSphere endpoint entry.
- `contexts.<name>.defaultCluster` remains optional and selects the default target cluster.
- An explicit `--cluster` overrides `defaultCluster`.
- Empty cluster selection preserves the existing unscoped request paths.
- OAuth login and refresh always use the original unscoped endpoint.
- Do not add flags, configuration fields, dependencies, or command-specific routing wrappers.

---

### Task 1: Route shared REST clients through the selected cluster

**Files:**
- Modify: `pkg/client/kubernetes/getter.go`
- Test: `pkg/client/kubernetes/getter_test.go`
- Test: `pkg/cmd/resource_commands_test.go`

**Interfaces:**
- Consumes: `auth.Resolve(...).Cluster`, selected by `--cluster` first and `context.defaultCluster` second.
- Produces: `RESTClientGetter.ToRESTConfig()` and `ToRawKubeConfigLoader().RawConfig()` with matching cluster-scoped server URLs.

- [ ] **Step 1: Add a failing getter test for context defaults and flag overrides**

Add after `TestRESTClientGetterReturnsResolvedKubeSphereCluster` in `pkg/client/kubernetes/getter_test.go`:

```go
func TestRESTClientGetterScopesClientConfigsToResolvedCluster(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	cfg.Clusters["prod"] = config.Cluster{Host: "https://ks.example.com/proxy/"}
	cfg.Users["admin"] = config.User{BearerToken: "secret"}
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin", DefaultCluster: "context-member"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, test := range []struct {
		name        string
		clusterFlag string
		want        string
	}{
		{name: "context default", want: "https://ks.example.com/proxy/clusters/context-member"},
		{name: "flag override", clusterFlag: "flag-member", want: "https://ks.example.com/proxy/clusters/flag-member"},
	} {
		t.Run(test.name, func(t *testing.T) {
			getter := NewRESTClientGetter(&Options{ConfigPath: configPath, Token: "secret", Cluster: test.clusterFlag, NoInteractive: true}, Dependencies{})
			restConfig, err := getter.ToRESTConfig()
			if err != nil {
				t.Fatalf("ToRESTConfig() error = %v", err)
			}
			if restConfig.Host != test.want {
				t.Fatalf("Host = %q, want %q", restConfig.Host, test.want)
			}
			rawConfig, err := getter.ToRawKubeConfigLoader().RawConfig()
			if err != nil {
				t.Fatalf("RawConfig() error = %v", err)
			}
			if got := rawConfig.Clusters[clientConfigName].Server; got != test.want {
				t.Fatalf("raw cluster server = %q, want %q", got, test.want)
			}
		})
	}
}
```

Add a path-segment regression test in the same file:

```go
func TestRESTClientGetterRejectsInvalidClusterPathSegment(t *testing.T) {
	for _, cluster := range []string{"..", "team/member", "team%2Fmember"} {
		t.Run(cluster, func(t *testing.T) {
			getter := NewRESTClientGetter(&Options{Endpoint: "https://ks.example.com/proxy", Token: "secret", Cluster: cluster, NoInteractive: true}, Dependencies{})
			_, err := getter.ToRESTConfig()
			if err == nil || !strings.Contains(err.Error(), "invalid cluster") {
				t.Fatalf("ToRESTConfig() error = %v, want invalid cluster error", err)
			}
		})
	}
}
```

- [ ] **Step 2: Add failing command tests for explicit and context-default routing**

Add before `TestNativeDescribeThroughKSApiServer` in `pkg/cmd/resource_commands_test.go`:

```go
func TestNativeGetThroughSpecifiedCluster(t *testing.T) {
	server := newClusterScopedCoreAPIServer(t, "host")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"get", "po", "--all-namespaces", "--endpoint", server.URL, "--token", "secret", "--cluster", "host", "--no-interactive", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "demo-pod"`) {
		t.Fatalf("get output missing cluster pod:\n%s", out.String())
	}
}

func TestNativeDescribeUsesContextDefaultCluster(t *testing.T) {
	server := newClusterScopedCoreAPIServer(t, "host")
	defer server.Close()
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Clusters["local"] = config.Cluster{Host: server.URL}
	cfg.Users["admin"] = config.User{BearerToken: "secret"}
	cfg.Contexts["local"] = config.Context{Cluster: "local", User: "admin", DefaultCluster: "host"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"describe", "pod/demo-pod", "--namespace", "default", "--token", "secret", "--show-events=false", "--no-interactive"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "demo-pod") {
		t.Fatalf("describe output missing cluster pod:\n%s", out.String())
	}
}
```

Add before `newFakeKSApiServer` in the same file:

```go
func newClusterScopedCoreAPIServer(t *testing.T, cluster string) *httptest.Server {
	t.Helper()
	prefix := "/clusters/" + cluster
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("request path = %q, want prefix %q", r.URL.Path, prefix+"/")
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, prefix) {
		case "/api":
			writeAPIJSON(t, w, metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{"v1"}})
		case "/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}})
		case "/api/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods", SingularName: "pod", Namespaced: true, Kind: "Pod", Verbs: metav1.Verbs{"get", "list"}, ShortNames: []string{"po"}}}})
		case "/api/v1/pods":
			writeAPIJSON(t, w, map[string]any{"apiVersion": "v1", "kind": "PodList", "items": []any{podObject()}})
		case "/api/v1/namespaces/default/pods/demo-pod":
			writeAPIJSON(t, w, podObject())
		default:
			http.NotFound(w, r)
		}
	}))
}

func podObject() map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":  map[string]any{"name": "demo-pod", "namespace": "default"},
		"spec": map[string]any{"containers": []any{map[string]any{
			"name": "demo", "image": "nginx:latest",
		}}},
		"status": map[string]any{"phase": "Running"},
	}
}
```

Strengthen `TestRootRefreshesExpiredCacheBeforeResourceRequest` by setting
`DefaultCluster: "host"`, keeping the refresh handler at `/oauth/token`, and
changing only its Kubernetes handler cases to these scoped paths:

```go
case "/clusters/host/api":
case "/clusters/host/apis":
case "/clusters/host/apis/tenant.kubesphere.io/v1beta1":
case "/clusters/host/apis/tenant.kubesphere.io/v1beta1/workspaces":
```

This proves token refresh remains unscoped while the resource request made
with the refreshed token is cluster scoped.

- [ ] **Step 3: Run the new tests and verify RED**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubernetes ./pkg/cmd -run 'TestRESTClientGetterScopesClientConfigsToResolvedCluster|TestNativeGetThroughSpecifiedCluster|TestNativeDescribeUsesContextDefaultCluster' -count=1
```

Expected: FAIL because the getter host remains unscoped, invalid path segments are accepted, and command requests hit `/api` or `/apis` instead of `/clusters/host/...`.

- [ ] **Step 4: Scope the final REST host after token resolution**

Add `net/url` to `pkg/client/kubernetes/getter.go`. Immediately after `tokenProvider.Token(...)` succeeds, add:

```go
		resourceEndpoint := resolved.Endpoint
		if resolved.Cluster != "" {
			if messages := rest.IsValidPathSegmentName(resolved.Cluster); len(messages) != 0 {
				g.configErr = fmt.Errorf("invalid cluster %q: %v", resolved.Cluster, messages)
				return
			}
			resourceEndpoint, err = url.JoinPath(resolved.Endpoint, "clusters", resolved.Cluster)
			if err != nil {
				g.configErr = fmt.Errorf("build endpoint for cluster %q: %w", resolved.Cluster, err)
				return
			}
		}
```

Use `resourceEndpoint` for `g.baseConfig.Host` and `raw.Clusters[clientConfigName].Server`. Keep token resolution before this block so OAuth refresh still consumes `resolved.Endpoint`.

- [ ] **Step 5: Verify new routing and expose the existing version regression**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubernetes ./pkg/cmd -count=1
```

Expected: the new routing tests PASS, while `TestRootVersionPrintsClientAndTargetVersions` FAILS because `version` adds the cluster prefix twice. This failure is the RED evidence for Task 2.

---

### Task 2: Make version consume the scoped host exactly once

**Files:**
- Modify: `pkg/cmd/version.go`
- Test: `pkg/cmd/root_test.go`

**Interfaces:**
- Consumes: `versionRESTClientGetter.ToRESTConfig() (*rest.Config, error)` with an already scoped `Host`.
- Produces: one request to `<scoped-host>/kapis/version` without another `.Cluster(...)` prefix.

- [ ] **Step 1: Remove duplicate version cluster selection**

Narrow the getter interface:

```go
type versionRESTClientGetter interface {
	ToRESTConfig() (*rest.Config, error)
}
```

Remove the `KubeSphereCluster()` call at the start of `loadServerVersion`, begin directly with `getter.ToRESTConfig()`, and replace the request block with:

```go
	request := client.Get().AbsPath("/kapis/version")
	raw, err := request.Do(ctx).Raw()
```

Retain `RESTClientGetter.KubeSphereCluster` to avoid an unrelated exported API removal.

- [ ] **Step 2: Cover the context-default version path**

Add `github.com/kubesphere/ksctl/pkg/config` to the imports in
`pkg/cmd/root_test.go`, then add:

```go
func TestRootVersionUsesContextDefaultCluster(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/clusters/context-member/kapis/version" {
			t.Errorf("path = %q, want /clusters/context-member/kapis/version", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gitVersion":"v4.2.0","kubernetes":{"gitVersion":"v1.31.0"}}`))
	}))
	defer server.Close()
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Clusters["local"] = config.Cluster{Host: server.URL}
	cfg.Users["admin"] = config.User{BearerToken: "secret"}
	cfg.Contexts["local"] = config.Context{Cluster: "local", User: "admin", DefaultCluster: "context-member"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "v0.1.0"})
	cmd.SetArgs([]string{"version", "--token", "secret", "--no-interactive"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "ksctl Version: v0.1.0\nKubeSphere Version: v4.2.0\nKubernetes Version: v1.31.0\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}
```

- [ ] **Step 3: Run focused GREEN verification**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/client/kubernetes ./pkg/cmd -run 'TestRESTClientGetterScopesClientConfigsToResolvedCluster|TestRESTClientGetterRejectsInvalidClusterPathSegment|TestNativeGetThroughSpecifiedCluster|TestNativeDescribeUsesContextDefaultCluster|TestRootRefreshesExpiredCacheBeforeResourceRequest|TestRootVersion' -count=1
```

Expected: PASS. The existing exact `/clusters/member/kapis/version` assertion proves the path is added once.

- [ ] **Step 4: Format and verify the complete repository**

Run:

```bash
gofmt -w pkg/client/kubernetes/getter.go pkg/client/kubernetes/getter_test.go pkg/cmd/resource_commands_test.go pkg/cmd/root_test.go pkg/cmd/version.go
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./... -count=1
env GOCACHE=/private/tmp/ksctl-go-build-cache go build ./...
git diff --check
```

Expected: formatting completes without output; all tests PASS; all packages build; `git diff --check` succeeds without output.

- [ ] **Step 5: Review scope and commit**

Run:

```bash
git diff -- pkg/client/kubernetes/getter.go pkg/client/kubernetes/getter_test.go pkg/cmd/resource_commands_test.go pkg/cmd/root_test.go pkg/cmd/version.go docs/superpowers/plans/2026-07-15-ksctl-cross-cluster.md
git add pkg/client/kubernetes/getter.go pkg/client/kubernetes/getter_test.go pkg/cmd/resource_commands_test.go pkg/cmd/root_test.go pkg/cmd/version.go
git add -f docs/superpowers/plans/2026-07-15-ksctl-cross-cluster.md
git commit -m "feat: support cross-cluster resource requests"
```

Expected: the diff contains only shared resource endpoint scoping, focused tests, version de-duplication, and this implementation plan; the commit succeeds.
