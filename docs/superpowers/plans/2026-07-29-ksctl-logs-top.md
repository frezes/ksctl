# ksctl Logs and Top Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add kubectl-compatible `logs`, `top pod`, and `top node` commands to both ksctl entrypoints through the existing KubeSphere connection and Cluster-routing pipeline.

**Architecture:** Move native Kubernetes command assembly out of `root.go` into one focused `resource_commands.go` file. Register kubectl's logs command directly; construct kubectl's top commands from exported options and replace only their discovery client after `Complete` so they use ksctl's cached KubeSphere fallback. Keep all flags, validation, clients, printing, streaming, and errors owned by kubectl v0.36.2.

**Tech Stack:** Go 1.26+, Cobra, Kubernetes `cli-runtime`, `client-go`, `kubectl` and Metrics API v0.36.2, Go `testing`/`httptest`.

## Global Constraints

- Go 1.26 or later is required.
- KubeSphere 4.x is the supported server generation.
- Keep `k8s.io/apimachinery`, `k8s.io/cli-runtime`, `k8s.io/client-go`, and `k8s.io/kubectl` aligned at v0.36.2.
- Do not add a KubeSphere logging, monitoring, Prometheus, or historical-query API.
- Do not add cross-Cluster log streaming or metrics aggregation.
- Do not add a public client interface, persistent configuration field, or dependency module.
- Keep the built-in commands read-only and identical through `ksctl` and `kubectl ks`.
- Do not modify `staging/`.
- Preserve kubectl's command syntax, flags, validation, output, and errors.
- Put Go tests beside the package they exercise and format all Go changes with `gofmt`.

## File Structure

- Create `pkg/cmd/resource_commands.go`: own kubectl resource-command assembly, recursive example rewriting, direct logs registration, and the private top discovery adapter.
- Modify `pkg/cmd/root.go`: retain dependency construction and delegate native resource-command creation.
- Modify `pkg/cmd/root_test.go`: verify registration, representative flags, both entrypoint display names, and plugin conflicts.
- Modify `pkg/cmd/resource_commands_test.go`: verify logs, top, Namespace, Cluster, discovery fallback, Metrics/Core paths, output, and streaming seams against fake KubeSphere endpoints.
- Modify `README.md`: advertise logs/top and add quick-start examples.
- Modify `docs/cli.md`: document syntax, prerequisites, command contract, examples, scope, and troubleshooting.
- Modify `docs/design.md`: update the current architecture, discovery path, routing, plugin ownership, security, and validation boundaries.

---

### Task 1: Add the kubectl-compatible logs command

**Files:**
- Create: `pkg/cmd/resource_commands.go`
- Modify: `pkg/cmd/root.go`
- Modify: `pkg/cmd/root_test.go`
- Modify: `pkg/cmd/resource_commands_test.go`

**Interfaces:**
- Consumes: `cmdutil.Factory`, `genericiooptions.IOStreams`, root `cmd.DisplayName()`, and the existing `clientkubernetes.RESTClientGetter`.
- Produces: `newResourceCommands(displayName string, factory cmdutil.Factory, streams genericiooptions.IOStreams) []*cobra.Command`.
- Produces: `rewriteKubectlExamples(command *cobra.Command, displayName string)`.
- Leaves `pkg/client/kubernetes` unchanged.

- [ ] **Step 1: Add failing command registration and display-name tests**

In `pkg/cmd/root_test.go`, extend `TestRootRegistersNativeResourceCommands` with the logs assertions:

```go
	logsCommand := findSubcommand(cmd, "logs")
	if logsCommand == nil {
		t.Fatal("logs command is not registered")
	}
	for _, name := range []string{"follow", "previous", "container", "tail"} {
		if logsCommand.Flags().Lookup(name) == nil {
			t.Errorf("logs flag --%s is not registered", name)
		}
	}
```

Add this test beside `TestKubectlPluginHelpUsesDisplayName`:

```go
func TestKubectlPluginLogsHelpUsesDisplayName(t *testing.T) {
	out := new(bytes.Buffer)
	cmd := NewKubectlPluginCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "dev"},
	)
	cmd.SetArgs([]string{"logs", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := out.String()
	if !strings.Contains(help, "Usage:\n  kubectl ks logs") ||
		strings.Contains(help, "Usage:\n  kubectl logs") {
		t.Fatalf("plugin logs usage = %q", help)
	}
	if !strings.Contains(help, "kubectl ks logs nginx") ||
		strings.Contains(help, "kubectl logs nginx") {
		t.Fatalf("plugin logs examples should use kubectl ks: %q", help)
	}
}
```

- [ ] **Step 2: Add failing direct-Pod and Cluster-routing integration tests**

Add these tests and helper to `pkg/cmd/resource_commands_test.go`:

```go
func TestNativeLogsThroughSpecifiedCluster(t *testing.T) {
	server := newClusterScopedLogsAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetArgs([]string{
		"logs", "demo-pod",
		"--namespace", "default",
		"--container", "demo",
		"--tail", "2",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "member",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := out.String(), "line one\nline two\n"; got != want {
		t.Fatalf("logs output = %q, want %q", got, want)
	}
}

func TestNativeLogsUsesContextDefaultCluster(t *testing.T) {
	server := newClusterScopedLogsAPIServer(t, "context-member")
	defer server.Close()
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{
		Host:  server.URL,
		Users: map[string]config.User{"admin": {BearerToken: "secret"}},
	}
	cfg.Contexts["local"] = config.Context{
		Fleet:          "local",
		User:           "admin",
		DefaultCluster: "context-member",
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	out := new(bytes.Buffer)
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetArgs([]string{
		"logs", "demo-pod",
		"--namespace", "default",
		"--container", "demo",
		"--tail", "2",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := out.String(), "line one\nline two\n"; got != want {
		t.Fatalf("logs output = %q, want %q", got, want)
	}
}

func newClusterScopedLogsAPIServer(t *testing.T, cluster string) *httptest.Server {
	t.Helper()
	prefix := "/clusters/" + cluster

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("path = %q, want prefix %q", r.URL.Path, prefix+"/")
			http.NotFound(w, r)
			return
		}

		switch strings.TrimPrefix(r.URL.Path, prefix) {
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
				Versions: []string{"v1"},
			})
		case "/apis":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
			})
		case "/api/v1":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name:         "pods",
					SingularName: "pod",
					Namespaced:   true,
					Kind:         "Pod",
					Verbs:        metav1.Verbs{"get", "list"},
					ShortNames:   []string{"po"},
				}},
			})
		case "/api/v1/namespaces/default/pods/demo-pod":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, podObject())
		case "/api/v1/namespaces/default/pods/demo-pod/log":
			if got := r.URL.Query().Get("container"); got != "demo" {
				t.Errorf("container = %q, want demo", got)
			}
			if got := r.URL.Query().Get("tailLines"); got != "2" {
				t.Errorf("tailLines = %q, want 2", got)
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "line one\nline two\n")
		default:
			http.NotFound(w, r)
		}
	}))
}
```

Add `io` to the test imports.

- [ ] **Step 3: Add a failing discovery-fallback workload logs test**

Add this test to `pkg/cmd/resource_commands_test.go`:

```go
func TestNativeLogsResolvesWorkloadWithDiscoveryFallback(t *testing.T) {
	server := newFallbackDiscoveryKSApiServer(t)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetArgs([]string{
		"logs", "deployment/demo-deployment",
		"--namespace", "default",
		"--all-pods",
		"--all-containers",
		"--endpoint", server.URL,
		"--token", "secret",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "deployment log\n") {
		t.Fatalf("logs output = %q, want deployment log", out.String())
	}
}
```

Extend the switch in `newFallbackDiscoveryKSApiServer` with the exact named
workload, selected Pod list, and log endpoints:

```go
		case "/apis/apps/v1/namespaces/default/deployments/demo-deployment":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]any{
					"name":      "demo-deployment",
					"namespace": "default",
				},
				"spec": map[string]any{
					"selector": map[string]any{
						"matchLabels": map[string]any{"app": "demo"},
					},
					"template": map[string]any{
						"metadata": map[string]any{
							"labels": map[string]any{"app": "demo"},
						},
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "demo", "image": "nginx:latest"},
							},
						},
					},
				},
			})
		case "/api/v1/namespaces/default/pods":
			pod := podObject()
			pod["metadata"].(map[string]any)["labels"] = map[string]any{"app": "demo"}
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "PodList",
				"items":      []any{pod},
			})
		case "/api/v1/namespaces/default/pods/demo-pod/log":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "deployment log\n")
```

- [ ] **Step 4: Add a failing followed-log cancellation test**

Add this test and server helper to `pkg/cmd/resource_commands_test.go`. The
test uses upstream `--ignore-errors` so cancellation remains in-process and
does not invoke kubectl's fatal exit handler:

```go
func TestNativeLogsFollowStopsWhenContextIsCancelled(t *testing.T) {
	started := make(chan struct{})
	server := newStreamingLogsAPIServer(t, started)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{
		"logs", "demo-pod",
		"--namespace", "default",
		"--follow",
		"--ignore-errors",
		"--endpoint", server.URL,
		"--token", "secret",
	})

	result := make(chan error, 1)
	go func() {
		result <- cmd.Execute()
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for followed log stream")
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("followed logs did not stop after cancellation")
	}
	if !strings.Contains(out.String(), "streamed line\n") {
		t.Fatalf("logs output = %q, want streamed line", out.String())
	}
}

func newStreamingLogsAPIServer(t *testing.T, started chan<- struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		switch r.URL.Path {
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
				Versions: []string{"v1"},
			})
		case "/apis":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
			})
		case "/api/v1":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name:         "pods",
					SingularName: "pod",
					Namespaced:   true,
					Kind:         "Pod",
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			})
		case "/api/v1/namespaces/default/pods/demo-pod":
			w.Header().Set("Content-Type", "application/json")
			writeAPIJSON(t, w, podObject())
		case "/api/v1/namespaces/default/pods/demo-pod/log":
			w.Header().Set("Content-Type", "text/plain")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("response writer does not support flushing")
				return
			}
			_, _ = io.WriteString(w, "streamed line\n")
			flusher.Flush()
			close(started)
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
}
```

Add `context` to the test imports.

- [ ] **Step 5: Run the new tests and verify they fail for the missing command**

Run:

```bash
go test ./pkg/cmd -run 'TestRootRegistersNativeResourceCommands|TestKubectlPluginLogsHelpUsesDisplayName|TestNativeLogs' -count=1
```

Expected: FAIL because `logs` is not registered; the integration tests report
an unknown command or missing subcommand.

- [ ] **Step 6: Create the resource-command assembly and register logs**

Create `pkg/cmd/resource_commands.go`:

```go
package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newResourceCommands(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) []*cobra.Command {
	commands := []*cobra.Command{
		getcmd.NewCmdGet(displayName, factory, streams),
		describecmd.NewCmdDescribe(displayName, factory, streams),
		logscmd.NewCmdLogs(factory, streams),
	}
	for _, command := range commands {
		rewriteKubectlExamples(command, displayName)
	}
	return commands
}

func rewriteKubectlExamples(command *cobra.Command, displayName string) {
	command.Example = strings.ReplaceAll(
		command.Example,
		"kubectl ",
		displayName+" ",
	)
	for _, child := range command.Commands() {
		rewriteKubectlExamples(child, displayName)
	}
}
```

In `pkg/cmd/root.go`, remove the `strings`,
`k8s.io/kubectl/pkg/cmd/describe`, and `k8s.io/kubectl/pkg/cmd/get` imports.
Replace:

```go
	getCommand := get.NewCmdGet(cmd.DisplayName(), factory, kubeStreams)
	getCommand.Example = strings.ReplaceAll(getCommand.Example, "kubectl ", cmd.DisplayName()+" ")
	describeCommand := describecmd.NewCmdDescribe(cmd.DisplayName(), factory, kubeStreams)
	describeCommand.Example = strings.ReplaceAll(describeCommand.Example, "kubectl ", cmd.DisplayName()+" ")
	cmd.AddCommand(getCommand, describeCommand)
```

with:

```go
	cmd.AddCommand(newResourceCommands(cmd.DisplayName(), factory, kubeStreams)...)
```

- [ ] **Step 7: Format and run focused tests**

Run:

```bash
gofmt -w pkg/cmd/root.go pkg/cmd/resource_commands.go pkg/cmd/root_test.go pkg/cmd/resource_commands_test.go
go test ./pkg/cmd -run 'TestRootRegistersNativeResourceCommands|TestKubectlPlugin.*HelpUsesDisplayName|TestNative(Get|Describe|Logs)' -count=1
git diff --check
```

Expected: all selected tests PASS and `git diff --check` prints nothing.

- [ ] **Step 8: Run the complete command-package tests**

Run:

```bash
go test ./pkg/cmd/... -count=1
```

Expected: PASS for `pkg/cmd` and its subpackages.

- [ ] **Step 9: Commit the logs command**

```bash
git add pkg/cmd/root.go pkg/cmd/resource_commands.go pkg/cmd/root_test.go pkg/cmd/resource_commands_test.go
git commit -m "add kubectl-compatible logs command"
```

---

### Task 2: Add top pod/node with KubeSphere discovery fallback

**Files:**
- Modify: `pkg/cmd/resource_commands.go`
- Modify: `pkg/cmd/root_test.go`
- Modify: `pkg/cmd/resource_commands_test.go`

**Interfaces:**
- Consumes: `newResourceCommands`, `rewriteKubectlExamples`, `cmdutil.Factory.ToDiscoveryClient`, and kubectl's exported `TopPodOptions`/`TopNodeOptions`.
- Produces: `newTopCommand(factory cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command`.
- Produces: `newTopPodCommand(factory cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command`.
- Produces: `newTopNodeCommand(factory cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command`.
- Does not change the existing `RESTClientGetter` contract.

- [ ] **Step 1: Add failing top registration and recursive display-name tests**

In `TestRootRegistersNativeResourceCommands` in `pkg/cmd/root_test.go`, add:

```go
	topCommand := findSubcommand(cmd, "top")
	if topCommand == nil {
		t.Fatal("top command is not registered")
	}
	topPod := findSubcommand(topCommand, "pod")
	if topPod == nil || topPod.Flags().Lookup("containers") == nil {
		t.Fatal("top pod --containers is not registered")
	}
	topNode := findSubcommand(topCommand, "node")
	if topNode == nil || topNode.Flags().Lookup("show-capacity") == nil {
		t.Fatal("top node --show-capacity is not registered")
	}
```

Add:

```go
func TestKubectlPluginTopHelpUsesDisplayNameRecursively(t *testing.T) {
	out := new(bytes.Buffer)
	cmd := NewKubectlPluginCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "dev"},
	)
	cmd.SetArgs([]string{"top", "pod", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := out.String()
	if !strings.Contains(help, "Usage:\n  kubectl ks top pod") ||
		strings.Contains(help, "Usage:\n  kubectl top pod") {
		t.Fatalf("plugin top pod usage = %q", help)
	}
	if !strings.Contains(help, "kubectl ks top pod") ||
		strings.Contains(help, "kubectl top pod -l") {
		t.Fatalf("plugin top pod examples should use kubectl ks: %q", help)
	}
}
```

- [ ] **Step 2: Add failing PodMetrics and NodeMetrics integration tests**

Add the tests and helper below to `pkg/cmd/resource_commands_test.go`:

```go
func TestNativeTopPodUsesFallbackDiscoveryThroughSpecifiedCluster(t *testing.T) {
	server := newClusterScopedMetricsAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetArgs([]string{
		"top", "pod",
		"--namespace", "demo",
		"--use-protocol-buffers=false",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "member",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"demo-pod", "125m", "64Mi"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("top pod output missing %q:\n%s", want, out.String())
		}
	}
}

func TestNativeTopPodAllNamespacesForwardsSelector(t *testing.T) {
	server := newClusterScopedMetricsAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetArgs([]string{
		"top", "pod",
		"--all-namespaces",
		"--selector", "app=demo",
		"--use-protocol-buffers=false",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "member",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"demo", "demo-pod", "125m", "64Mi"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("top pod -A output missing %q:\n%s", want, out.String())
		}
	}
}

func TestNativeTopNodeUsesFallbackDiscoveryThroughSpecifiedCluster(t *testing.T) {
	server := newClusterScopedMetricsAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	cmd.SetArgs([]string{
		"top", "node",
		"--use-protocol-buffers=false",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "member",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"member-node", "250m", "25%", "256Mi"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("top node output missing %q:\n%s", want, out.String())
		}
	}
}

func newClusterScopedMetricsAPIServer(t *testing.T, cluster string) *httptest.Server {
	t.Helper()
	prefix := "/clusters/" + cluster

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("path = %q, want prefix %q", r.URL.Path, prefix+"/")
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, prefix)
		w.Header().Set("Content-Type", "application/json")

		switch path {
		case "/api", "/apis":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<!doctype html><title>KubeSphere</title>")
		case "/api/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{
						Name:       "pods",
						Namespaced: true,
						Kind:       "Pod",
						Verbs:      metav1.Verbs{"get", "list"},
					},
					{
						Name:       "nodes",
						Namespaced: false,
						Kind:       "Node",
						Verbs:      metav1.Verbs{"get", "list"},
					},
				},
			})
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions",
			"/apis/extensions.kubesphere.io/v1alpha1/apiservices":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]any{},
				"items":      []any{},
			})
		case "/apis/apiregistration.k8s.io/v1/apiservices":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "apiregistration.k8s.io/v1",
				"kind":       "APIServiceList",
				"metadata":   map[string]any{},
				"items": []any{map[string]any{
					"spec": map[string]any{
						"group":   "metrics.k8s.io",
						"version": "v1beta1",
					},
				}},
			})
		case "/apis/metrics.k8s.io/v1beta1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "metrics.k8s.io/v1beta1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Namespaced: true, Kind: "PodMetrics", Verbs: metav1.Verbs{"get", "list"}},
					{Name: "nodes", Namespaced: false, Kind: "NodeMetrics", Verbs: metav1.Verbs{"get", "list"}},
				},
			})
		case "/apis/metrics.k8s.io/v1beta1/namespaces/demo/pods",
			"/apis/metrics.k8s.io/v1beta1/pods":
			if path == "/apis/metrics.k8s.io/v1beta1/pods" {
				if got := r.URL.Query().Get("labelSelector"); got != "app=demo" {
					t.Errorf("labelSelector = %q, want app=demo", got)
				}
			}
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "metrics.k8s.io/v1beta1",
				"kind":       "PodMetricsList",
				"items": []any{map[string]any{
					"metadata":  map[string]any{"name": "demo-pod", "namespace": "demo"},
					"timestamp": "2026-07-29T00:00:00Z",
					"window":    "30s",
					"containers": []any{map[string]any{
						"name": "demo",
						"usage": map[string]any{"cpu": "125m", "memory": "64Mi"},
					}},
				}},
			})
		case "/apis/metrics.k8s.io/v1beta1/nodes":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "metrics.k8s.io/v1beta1",
				"kind":       "NodeMetricsList",
				"items": []any{map[string]any{
					"metadata":  map[string]any{"name": "member-node"},
					"timestamp": "2026-07-29T00:00:00Z",
					"window":    "30s",
					"usage":     map[string]any{"cpu": "250m", "memory": "256Mi"},
				}},
			})
		case "/api/v1/nodes":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "NodeList",
				"items": []any{map[string]any{
					"apiVersion": "v1",
					"kind":       "Node",
					"metadata":   map[string]any{"name": "member-node"},
					"status": map[string]any{
						"allocatable": map[string]any{"cpu": "1", "memory": "1Gi"},
						"capacity":    map[string]any{"cpu": "2", "memory": "2Gi"},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}
```

The fake intentionally returns HTML for `/api` and `/apis`. A direct upstream
top command bypasses `Factory.ToDiscoveryClient`, so this test proves the
adapter rather than merely proving Metrics API decoding.

- [ ] **Step 3: Add a failing subprocess test for an unavailable Metrics API**

Add these subprocess tests to `pkg/cmd/resource_commands_test.go`:

```go
func TestNativeTopReportsUnavailableMetricsAPI(t *testing.T) {
	const helperEnv = "KSCTL_TEST_TOP_NO_METRICS"
	if os.Getenv(helperEnv) == "1" {
		cmd := NewRootCommand(
			IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
			VersionInfo{Version: "test"},
		)
		cmd.SetArgs([]string{
			"top", "pod",
			"--use-protocol-buffers=false",
			"--endpoint", os.Getenv("KSCTL_TEST_TOP_ENDPOINT"),
			"--token", "secret",
		})
		_ = cmd.Execute()
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
				Versions: []string{"v1"},
			})
		case "/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	helper := exec.Command(os.Args[0], "-test.run=^TestNativeTopReportsUnavailableMetricsAPI$")
	helper.Env = append(
		os.Environ(),
		helperEnv+"=1",
		"KSCTL_TEST_TOP_ENDPOINT="+server.URL,
		"KSCTL_CONFIG="+filepath.Join(t.TempDir(), "config.yaml"),
	)
	output, err := helper.CombinedOutput()
	if err == nil {
		t.Fatalf("top helper succeeded, output:\n%s", output)
	}
	if !strings.Contains(string(output), "Metrics API not available") {
		t.Fatalf("top helper output = %q, want Metrics API not available", output)
	}
}

func TestNativeTopPreservesForbiddenMetricsError(t *testing.T) {
	const helperEnv = "KSCTL_TEST_TOP_FORBIDDEN"
	if os.Getenv(helperEnv) == "1" {
		cmd := NewRootCommand(
			IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
			VersionInfo{Version: "test"},
		)
		cmd.SetArgs([]string{
			"top", "pod",
			"--use-protocol-buffers=false",
			"--endpoint", os.Getenv("KSCTL_TEST_TOP_ENDPOINT"),
			"--token", "secret",
		})
		_ = cmd.Execute()
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
				Versions: []string{"v1"},
			})
		case "/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
				Groups: []metav1.APIGroup{{
					Name: "metrics.k8s.io",
					Versions: []metav1.GroupVersionForDiscovery{{
						GroupVersion: "metrics.k8s.io/v1beta1",
						Version:      "v1beta1",
					}},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: "metrics.k8s.io/v1beta1",
						Version:      "v1beta1",
					},
				}},
			})
		case "/apis/metrics.k8s.io/v1beta1/namespaces/default/pods":
			w.WriteHeader(http.StatusForbidden)
			writeAPIJSON(t, w, metav1.Status{
				TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
				Status:   metav1.StatusFailure,
				Message:  "podmetrics.metrics.k8s.io is forbidden",
				Reason:   metav1.StatusReasonForbidden,
				Code:     http.StatusForbidden,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	helper := exec.Command(os.Args[0], "-test.run=^TestNativeTopPreservesForbiddenMetricsError$")
	helper.Env = append(
		os.Environ(),
		helperEnv+"=1",
		"KSCTL_TEST_TOP_ENDPOINT="+server.URL,
		"KSCTL_CONFIG="+filepath.Join(t.TempDir(), "config.yaml"),
	)
	output, err := helper.CombinedOutput()
	if err == nil {
		t.Fatalf("top helper succeeded, output:\n%s", output)
	}
	if !strings.Contains(string(output), "podmetrics.metrics.k8s.io is forbidden") {
		t.Fatalf("top helper output = %q, want forbidden Status error", output)
	}
}
```

Add `os` and `os/exec` to `pkg/cmd/resource_commands_test.go` imports.

- [ ] **Step 4: Add a failing built-in plugin-conflict test**

Add `runtime` to `pkg/cmd/root_test.go` imports, then add:

```go
func TestPluginListReportsLogsAndTopAsBuiltInConflicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test plugin fixtures use Unix executable mode bits")
	}
	directory := t.TempDir()
	for _, name := range []string{"ksctl-logs", "ksctl-top"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("plugin"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	root := NewRootCommand(
		IOStreams{Out: out, ErrOut: errOut},
		VersionInfo{Version: "test"},
	)
	root.SetArgs([]string{"plugin", "list", "--name-only"})
	t.Setenv("PATH", directory)

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "2 plugin warnings") {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{
		`ksctl-logs overwrites existing command: "ksctl logs"`,
		`ksctl-top overwrites existing command: "ksctl top"`,
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want %q", errOut.String(), want)
		}
	}
}
```

- [ ] **Step 5: Run the new tests and verify they fail for the missing command**

Run:

```bash
go test ./pkg/cmd -run 'TestRootRegistersNativeResourceCommands|TestKubectlPluginTopHelpUsesDisplayNameRecursively|TestNativeTop|TestPluginListReportsLogsAndTopAsBuiltInConflicts' -count=1
```

Expected: FAIL because `top` is not registered. The plugin test sees only one
built-in conflict, and the metrics tests report an unknown command.

- [ ] **Step 6: Add the narrow top discovery adapter**

Add `topcmd "k8s.io/kubectl/pkg/cmd/top"` to
`pkg/cmd/resource_commands.go`. Append `newTopCommand(factory, streams)` to the
commands slice:

```go
	commands := []*cobra.Command{
		getcmd.NewCmdGet(displayName, factory, streams),
		describecmd.NewCmdDescribe(displayName, factory, streams),
		logscmd.NewCmdLogs(factory, streams),
		newTopCommand(factory, streams),
	}
```

Add these functions:

```go
func newTopCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) *cobra.Command {
	command := topcmd.NewCmdTop(factory, streams)
	for _, child := range command.Commands() {
		switch child.Name() {
		case "pod", "node":
			command.RemoveCommand(child)
		}
	}
	command.AddCommand(
		newTopPodCommand(factory, streams),
		newTopNodeCommand(factory, streams),
	)
	return command
}

func newTopPodCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) *cobra.Command {
	options := &topcmd.TopPodOptions{
		IOStreams:          streams,
		UseProtocolBuffers: true,
	}
	command := topcmd.NewCmdTopPod(factory, options, streams)
	command.Run = func(command *cobra.Command, args []string) {
		cmdutil.CheckErr(options.Complete(factory, command, args))
		discoveryClient, err := factory.ToDiscoveryClient()
		cmdutil.CheckErr(err)
		options.DiscoveryClient = discoveryClient
		cmdutil.CheckErr(options.Validate())
		cmdutil.CheckErr(options.RunTopPod())
	}
	return command
}

func newTopNodeCommand(
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
) *cobra.Command {
	options := &topcmd.TopNodeOptions{
		IOStreams:          streams,
		UseProtocolBuffers: true,
	}
	command := topcmd.NewCmdTopNode(factory, options, streams)
	command.Run = func(command *cobra.Command, args []string) {
		cmdutil.CheckErr(options.Complete(factory, command, args))
		discoveryClient, err := factory.ToDiscoveryClient()
		cmdutil.CheckErr(err)
		options.DiscoveryClient = discoveryClient
		cmdutil.CheckErr(options.Validate())
		cmdutil.CheckErr(options.RunTopNode())
	}
	return command
}
```

Do not move this behavior into `pkg/client/kubernetes`: the mismatch is local
to kubectl top's assembly and the adapter should remain deletable.

- [ ] **Step 7: Format and run focused tests**

Run:

```bash
gofmt -w pkg/cmd/resource_commands.go pkg/cmd/root_test.go pkg/cmd/resource_commands_test.go
go test ./pkg/cmd -run 'TestRootRegistersNativeResourceCommands|TestKubectlPluginTopHelpUsesDisplayNameRecursively|TestNativeTop|TestPluginListReportsLogsAndTopAsBuiltInConflicts' -count=1
git diff --check
```

Expected: all selected tests PASS. The top Pod and Node tests must pass while
the aggregate discovery roots return HTML.

- [ ] **Step 8: Run all tests for the affected client and command packages**

Run:

```bash
go test ./pkg/client/kubernetes ./pkg/cmd/... -count=1
```

Expected: PASS for the Kubernetes client adapter and every command package.

- [ ] **Step 9: Commit the top command**

```bash
git add pkg/cmd/resource_commands.go pkg/cmd/root_test.go pkg/cmd/resource_commands_test.go
git commit -m "add kubectl-compatible top command"
```

---

### Task 3: Document the commands and verify the repository

**Files:**
- Modify: `README.md`
- Modify: `docs/cli.md`
- Modify: `docs/design.md`

**Interfaces:**
- Consumes: the built-in command contract from Tasks 1 and 2.
- Produces: current user-facing and architecture documentation for logs/top.
- Does not change Go behavior.

- [ ] **Step 1: Update README features and quick start**

Replace the first feature bullet in `README.md` with:

```markdown
- Inspect KubeSphere and Kubernetes resources with kubectl-compatible `get`
  and `describe`, stream container logs with `logs`, and view Metrics Server
  CPU/memory usage with `top`.
```

Extend the Quick start code block to:

```bash
ksctl auth login
ksctl get workspaces
ksctl get pods -A
ksctl logs deployment/web -n demo --all-pods
ksctl top pod -n demo
```

- [ ] **Step 2: Update the CLI overview, prerequisites, syntax, and command table**

In `docs/cli.md`, change the Overview's generic built-in description to:

```markdown
Its generic built-in resource commands are read-only: `get` displays
resources, `describe` displays their detailed state, `logs` reads container
logs, and `top` displays current Metrics Server CPU/memory usage. The
purpose-built `extension` group is the controlled exception for KubeSphere
extension lifecycle management.
```

Add this prerequisite:

```markdown
- Metrics Server with `metrics.k8s.io/v1beta1` for `top` commands
```

Replace the resource-command syntax block and explanation with:

````markdown
```text
ksctl get TYPE [NAME] [flags]
ksctl describe TYPE [NAME_PREFIX] [flags]
ksctl logs (POD | TYPE/NAME) [flags]
ksctl top (pod | node) [NAME] [flags]
```

Resource names are resolved from server discovery. Singular, plural, short,
versioned, and group-qualified names are accepted where the selected kubectl
command supports them. Use command-specific help for exact arguments and
flags.
````

Add these rows to the command table after `describe`:

```markdown
| `ksctl logs (POD \| TYPE/NAME)` | Print or follow logs from one or more Pod containers. |
| `ksctl top (pod \| node) [NAME]` | Display current Metrics Server CPU/memory usage. |
```

- [ ] **Step 3: Add logs and top sections to the resource workflow**

After `### Describe resources` in `docs/cli.md`, add:

````markdown
### Read container logs

`logs` uses kubectl v0.36.2's Pod Logs behavior. It can read a Pod directly or
resolve a workload to its Pods:

```bash
ksctl logs pod/web-0 -n demo
ksctl logs deployment/web -n demo --all-pods
ksctl logs deployment/web -n demo --all-pods --all-containers --prefix
ksctl logs pod/web-0 -n demo -c app --previous
ksctl logs pod/web-0 -n demo --tail=100 --since=1h
ksctl logs pod/web-0 -n demo --follow
```

Logs are read from the selected Kubernetes Cluster through the KubeSphere
Endpoint. The built-in command does not search a logging extension, retain
history, reconnect a failed stream, or combine results from multiple Clusters.
Application logs can contain credentials or other sensitive data; protect
terminal capture and redirected files accordingly.

### View current resource usage

`top` reads the Kubernetes Metrics API. Metrics Server must expose
`metrics.k8s.io/v1beta1` in the selected Cluster:

```bash
ksctl top pod -n demo
ksctl top pod -A --sort-by=cpu
ksctl top pod web-0 -n demo --containers
ksctl top node
ksctl top node worker-0 --show-capacity
```

`top pod` is namespaced and supports `-A`; `top node` is Cluster-scoped.
The reported values are the recent signals used by Kubernetes autoscaling,
not historical monitoring data. ksctl does not fall back to a KubeSphere
monitoring extension.
````

Extend `### Select scope` with:

```bash
ksctl logs deployment/web -n demo --all-pods --cluster member-1
ksctl top pod -n demo --cluster member-1
ksctl top node --cluster member-1
```

Change the help sentence under filtering to:

```markdown
Run `ksctl get --help`, `ksctl describe --help`, `ksctl logs --help`, and
`ksctl top --help` for the complete command-specific flags.
```

Add these troubleshooting entries:

```markdown
### `Metrics API not available`

`top` requires Metrics Server and a discoverable `metrics.k8s.io/v1beta1`
APIService in the selected Cluster. Verify the Metrics Server deployment,
APIService availability, and the selected `--cluster`.

### Logs stop during `--follow`

A followed log stream ends when the server closes it, the user cancels it, an
unignored request error occurs, or an explicit nonzero `--request-timeout`
expires. ksctl does not reconnect or resume the stream.
```

- [ ] **Step 4: Update the current architecture document**

Make these exact conceptual updates in `docs/design.md`:

1. In Goals, replace the first two bullets with:

```markdown
- Provide one CLI for inspecting KubeSphere 4.x resources and Kubernetes
  resources, logs, and current metrics exposed through KubeSphere.
- Preserve familiar kubectl `get`, `describe`, `logs`, and `top` syntax,
  discovery, selection, printing, streaming, and error behavior.
```

2. In Non-goals, add:

```markdown
- KubeSphere logging or monitoring extension APIs, historical observability
  queries, or cross-Cluster logs/metrics aggregation
```

3. In Command architecture, replace the resource-command bullet with:

```markdown
- kubectl-owned `get`, `describe`, `logs`, and `top` commands; and
```

4. At the start of Resource command pipeline, replace the construction
paragraph and diagram with:

````markdown
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

`get`, `describe`, and `logs` use their upstream constructors directly.
`top` uses upstream parent, subcommand, option, validation, client, and printer
types, with one private assembly adapter: after upstream `Complete`, ksctl
replaces top's raw Clientset DiscoveryClient with
`Factory.ToDiscoveryClient`. This preserves ksctl's KubeSphere discovery
fallback without forking top behavior.

A recursive example normalizer changes upstream `kubectl` examples to the
active `ksctl` or `kubectl ks` display name.
````

5. Extend Discovery compatibility with:

```markdown
The private top adapter uses this same cached discovery surface. This matters
because kubectl v0.36.2 otherwise takes top discovery from a newly created
Clientset and bypasses `RESTClientGetter.ToDiscoveryClient`. Metrics API
discovery can therefore be recovered from APIService registration and the
concrete `metrics.k8s.io/v1beta1` endpoint when aggregate discovery roots are
unusable.
```

6. Extend Cross-cluster routing with:

````markdown
Pod log subresource, Metrics API, and supporting Core API requests use the same
effective server. For example:

```text
/clusters/<cluster>/api/v1/namespaces/<namespace>/pods/<pod>/log
/clusters/<cluster>/apis/metrics.k8s.io/v1beta1/namespaces/<namespace>/pods
/clusters/<cluster>/apis/metrics.k8s.io/v1beta1/nodes
```

The commands query one selected Cluster. They do not aggregate across Fleet
members.
````

7. In Plugin model, add:

```markdown
The built-in `logs` and `top` paths likewise cannot be replaced by
`ksctl-logs` or `ksctl-top` executables.
```

8. In Security properties, add:

```markdown
- Container logs are written to stdout and may contain application secrets;
  ksctl does not inspect or redact their content.
```

9. In Validation boundaries, replace the command-test bullet with:

```markdown
- command tests verify both display names, registered commands and flags,
  version behavior, resource requests, member-Cluster routing, recursive
  logs/top examples, log subresource streaming, Metrics/Core requests, and
  Metrics API discovery fallback;
```

- [ ] **Step 5: Check documentation coverage and formatting**

Run:

```bash
rg -n 'logs|top|metrics.k8s.io|Metrics Server' README.md docs/cli.md docs/design.md
git diff --check
```

Expected: each document names both commands; the CLI guide includes user
examples and limitations; the design includes assembly, discovery, routing,
security, and validation; `git diff --check` prints nothing.

- [ ] **Step 6: Run focused and complete verification**

Run:

```bash
go test ./pkg/client/kubernetes ./pkg/cmd/... -count=1
make verify
./bin/ksctl logs --help
./bin/ksctl top pod --help
./bin/kubectl-ks logs --help
./bin/kubectl-ks top node --help
git diff --check
git status --short
```

Expected:

- focused tests PASS;
- `make verify` passes formatting, modules, vet, normal tests, race tests, and
  both builds;
- each help command exits zero and shows its active entrypoint;
- `git diff --check` prints nothing; and
- `git status --short` lists only the three documentation files before the
  documentation commit.

- [ ] **Step 7: Commit documentation**

```bash
git add README.md docs/cli.md docs/design.md
git commit -m "document logs and top commands"
```

- [ ] **Step 8: Confirm the final commit range and clean worktree**

Run:

```bash
git log --oneline -3
git status --short
```

Expected: the three newest implementation commits are:

```text
document logs and top commands
add kubectl-compatible top command
add kubectl-compatible logs command
```

`git status --short` prints nothing.
