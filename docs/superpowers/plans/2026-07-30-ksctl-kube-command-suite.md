# ksctl Kube Command Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `kube` namespace containing kubectl v0.36.2's complete Kubernetes operation command set, routed through ksctl authentication and `--cluster`, while preserving top-level `get` and removing top-level `describe`, `logs`, and `top`.

**Architecture:** The root continues to create one KubeSphere-backed Kubernetes `RESTClientGetter` and one kubectl `Factory`. A new `pkg/cmd/kube.go` assembles exported upstream kubectl constructors under `kube`; `pkg/cmd/kube_top.go` retains only the existing discovery override required by KubeSphere member-Cluster routing. Top-level `get` remains a separate upstream command instance, and `--namespace` plus `--request-timeout` become persistent flags on `kube`.

**Tech Stack:** Go 1.26, Cobra v1.10.2, Kubernetes/kubectl/cli-runtime/client-go v0.36.2, Go `testing`, `httptest`, Make

## Global Constraints

- Keep all Kubernetes modules aligned at exactly v0.36.2.
- Reuse exported upstream kubectl command constructors; do not copy or fork their operation implementations.
- Do not call `kubectl.NewKubectlCommand`; it would create a kubeconfig-backed Factory.
- Do not read or write `~/.kube/config`.
- Do not add `kube config`, `kube plugin`, `kube version`, `kube completion`, `kube options`, `kube kuberc`, or empty `kube alpha`.
- Keep top-level `get`; remove top-level `describe`, `logs`, and `top` immediately without aliases or forwarding.
- Expose the shared command tree as `ksctl ...` and `unictl ks ...`; never describe an active `kubectl ks` entry point.
- Keep `--cluster` at the ksctl root and inherit it into every `kube` child.
- Remove root `--request-timeout`; expose it only as a persistent `kube` flag.
- Expose `--namespace`/`-n` persistently on `kube`; keep a command-local Namespace flag on top-level `get`.
- Retain the custom `top` DiscoveryClient replacement and no other top behavior fork.
- Generic `kube` commands may mutate the selected Cluster and must preserve upstream kubectl errors and write semantics.
- Never retry a failed member-Cluster operation against the Fleet Endpoint.
- Do not edit the published `[0.2.0]` changelog entry; add the breaking-change notice under `[Unreleased]` only.
- Do not modify `staging/` while implementing this feature.
- Follow TDD: add each behavior test, observe the expected failure, then write the minimum production change.

## File Structure

- Create `pkg/cmd/kube.go`: assemble the `kube` parent, command groups, inherited flags, display-name rewriting, and all upstream constructors.
- Create `pkg/cmd/kube_top.go`: own the narrow `top pod/node` discovery adapter.
- Create `pkg/cmd/kube_test.go`: verify command inventory, exclusions, inherited flags, help paths, get parity, timeout behavior, and representative mutation routing.
- Modify `pkg/cmd/resource_commands.go`: reduce the root resource surface to top-level `get` plus small shared flag/example helpers.
- Modify `pkg/cmd/root.go`: remove the root timeout flag and register top-level `get` plus `kube`.
- Modify `pkg/cmd/root_test.go`: update root-tree, plugin-conflict, display-name, and flag-scope expectations.
- Modify `pkg/cmd/resource_commands_test.go`: execute describe/logs/top integration cases through `kube`.
- Modify `go.mod` and `go.sum`: record transitive modules activated by the complete command suite.
- Modify `README.md`, `docs/cli.md`, and `docs/cli_zh.md`: document the user-facing command tree, migration, mutations, and `--cluster`.
- Modify `docs/design.md` and `docs/design_zh.md`: document the new assembly and security boundaries.
- Modify `CHANGELOG.md`: add only `[Unreleased]` entries.

---

### Task 1: Introduce the `kube` Namespace and Migrate Existing Resource Commands

**Files:**
- Create: `pkg/cmd/kube.go`
- Create: `pkg/cmd/kube_top.go`
- Create: `pkg/cmd/kube_test.go`
- Modify: `pkg/cmd/root.go:69-118`
- Modify: `pkg/cmd/resource_commands.go:1-120`
- Modify: `pkg/cmd/root_test.go:178-350,607-665`
- Modify: `pkg/cmd/resource_commands_test.go:24-650`

**Interfaces:**
- Consumes: `cmdutil.Factory`, `genericiooptions.IOStreams`, `client.Options.Namespace`, `client.Options.RequestTimeout`, and the existing `rewriteKubectlExamples`.
- Produces: `newKubeCommand(displayName string, factory cmdutil.Factory, streams genericiooptions.IOStreams, namespace, requestTimeout *string) *cobra.Command`.
- Produces: `newRootGetCommand(displayName string, factory cmdutil.Factory, streams genericiooptions.IOStreams, namespace *string) *cobra.Command`.
- Produces: `newKubeTopCommand(factory cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command`.

- [ ] **Step 1: Replace the root resource-tree test with the desired migration**

In `pkg/cmd/root_test.go`, replace `TestRootRegistersNativeResourceCommands` with:

```go
func TestRootRegistersKubeResourceCommands(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})

	rootGet := findSubcommand(root, "get")
	if rootGet == nil {
		t.Fatal("root get command is not registered")
	}
	for _, name := range []string{"describe", "logs", "top"} {
		if findSubcommand(root, name) != nil {
			t.Errorf("root %s command is registered", name)
		}
	}

	kube := findSubcommand(root, "kube")
	if kube == nil {
		t.Fatal("kube command is not registered")
	}
	kubeGet := findSubcommand(kube, "get")
	if kubeGet == nil {
		t.Fatal("kube get command is not registered")
	}
	for _, name := range []string{"output", "watch", "watch-only", "selector"} {
		if rootGet.Flags().Lookup(name) == nil {
			t.Errorf("root get flag --%s is not registered", name)
		}
		if kubeGet.Flags().Lookup(name) == nil {
			t.Errorf("kube get flag --%s is not registered", name)
		}
	}

	describe := findSubcommand(kube, "describe")
	if describe == nil || describe.Flags().Lookup("show-events") == nil {
		t.Fatal("kube describe --show-events is not registered")
	}
	logs := findSubcommand(kube, "logs")
	if logs == nil {
		t.Fatal("kube logs command is not registered")
	}
	for _, name := range []string{"follow", "previous", "container", "tail"} {
		if logs.Flags().Lookup(name) == nil {
			t.Errorf("kube logs flag --%s is not registered", name)
		}
	}
	top := findSubcommand(kube, "top")
	if top == nil {
		t.Fatal("kube top command is not registered")
	}
	topPod := findSubcommand(top, "pod")
	if topPod == nil || topPod.Flags().Lookup("containers") == nil {
		t.Fatal("kube top pod --containers is not registered")
	}
	topNode := findSubcommand(top, "node")
	if topNode == nil || topNode.Flags().Lookup("show-capacity") == nil {
		t.Fatal("kube top node --show-capacity is not registered")
	}
}
```

- [ ] **Step 2: Add failing flag-scope tests**

Replace `TestRootConnectionFlags` and `TestResourceCommandNamespaceFlags` with:

```go
func TestRootAndKubeConnectionFlagScopes(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	for _, name := range []string{"endpoint", "token", "context", "cluster", "v"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("root persistent flag --%s is not registered", name)
		}
	}
	for _, name := range []string{
		"insecure-skip-tls-verify",
		"namespace",
		"no-interactive",
		"request-timeout",
		"workspace",
	} {
		if root.PersistentFlags().Lookup(name) != nil {
			t.Errorf("root persistent flag --%s is registered", name)
		}
	}

	kube := findSubcommand(root, "kube")
	if kube == nil {
		t.Fatal("kube command is not registered")
	}
	namespace := kube.PersistentFlags().Lookup("namespace")
	if namespace == nil || namespace.Shorthand != "n" {
		t.Fatalf("kube --namespace = %#v, want persistent -n flag", namespace)
	}
	if kube.PersistentFlags().Lookup("request-timeout") == nil {
		t.Fatal("kube persistent --request-timeout is not registered")
	}

	rootGet := findSubcommand(root, "get")
	namespace = rootGet.LocalNonPersistentFlags().Lookup("namespace")
	if namespace == nil || namespace.Shorthand != "n" {
		t.Fatalf("root get --namespace = %#v, want command-local -n flag", namespace)
	}
	if rootGet.Flags().Lookup("request-timeout") != nil {
		t.Fatal("root get exposes --request-timeout")
	}

	for _, path := range [][]string{
		{"get"},
		{"describe"},
		{"logs"},
		{"top", "pod"},
	} {
		command := kube
		for _, name := range path {
			command = findSubcommand(command, name)
			if command == nil {
				t.Fatalf("kube %s is not registered", strings.Join(path, " "))
			}
		}
		if command.InheritedFlags().Lookup("namespace") == nil {
			t.Errorf("kube %s does not inherit --namespace", strings.Join(path, " "))
		}
		if command.InheritedFlags().Lookup("request-timeout") == nil {
			t.Errorf("kube %s does not inherit --request-timeout", strings.Join(path, " "))
		}
	}
}
```

- [ ] **Step 3: Add failing display-name tests for both shared entry-point forms**

Replace the old entry-point logs/top tests with:

```go
func TestKubeResourceHelpUsesEntrypointDisplayName(t *testing.T) {
	for _, test := range []struct {
		name    string
		newRoot func() *cobra.Command
		want    string
	}{
		{
			name: "standalone",
			newRoot: func() *cobra.Command {
				return NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: "ksctl kube",
		},
		{
			name: "alternate entrypoint",
			newRoot: func() *cobra.Command {
				return newTestEntrypointCommand(t, IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: "fixture entrypoint kube",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, path := range [][]string{
				{"get", "--help"},
				{"logs", "--help"},
				{"top", "pod", "--help"},
			} {
				out := new(bytes.Buffer)
				root := test.newRoot()
				root.SetOut(out)
				root.SetErr(out)
				root.SetArgs(append([]string{"kube"}, path...))
				if err := root.Execute(); err != nil {
					t.Fatalf("Execute(%v) error = %v", path, err)
				}
				if !strings.Contains(out.String(), test.want+" "+strings.Join(path[:len(path)-1], " ")) {
					t.Fatalf("help = %q, want display path %q", out.String(), test.want)
				}
			}
		})
	}
}
```

- [ ] **Step 4: Add a failing parity test for root `get` and `kube get`**

Create `pkg/cmd/kube_test.go` with:

```go
package cmd

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestRootGetAndKubeGetProduceEquivalentResults(t *testing.T) {
	server := newClusterScopedCoreAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	var outputs []string
	for _, prefix := range [][]string{{"get"}, {"kube", "get"}} {
		out := new(bytes.Buffer)
		command := NewRootCommand(
			IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
			VersionInfo{Version: "test"},
		)
		args := append([]string{}, prefix...)
		args = append(args,
			"pods",
			"--all-namespaces",
			"--endpoint", server.URL,
			"--token", "secret",
			"--cluster", "member",
			"-o", "json",
		)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != outputs[1] {
		t.Fatalf("root get output differs from kube get:\nroot:\n%s\nkube:\n%s", outputs[0], outputs[1])
	}
}
```

- [ ] **Step 5: Add a failing kube-only request-timeout behavior test**

Append to `pkg/cmd/kube_test.go` and add `net/http`, `net/http/httptest`,
`strings`, and `time` imports:

```go
func TestKubeRequestTimeoutLimitsRawGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	command := NewRootCommand(
		IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	command.SetArgs([]string{
		"kube", "get",
		"--raw=/slow",
		"--request-timeout=20ms",
		"--endpoint", server.URL,
		"--token", "secret",
	})

	start := time.Now()
	err := command.Execute()
	if err == nil {
		t.Fatal("Execute() succeeded, want request timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request timeout took %s, want less than one second", elapsed)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") &&
		!strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		t.Fatalf("Execute() error = %v, want timeout error", err)
	}
}
```

- [ ] **Step 6: Move all existing describe/logs/top integration invocations under `kube`**

In `pkg/cmd/resource_commands_test.go`, prepend `"kube"` to the command args
in these tests and their subprocess branches:

```text
TestNativeTopPodUsesFallbackDiscoveryThroughSpecifiedCluster
TestNativeTopPodAllNamespacesForwardsSelector
TestNativeTopNodeUsesFallbackDiscoveryThroughSpecifiedCluster
TestNativeTopReportsUnavailableMetricsAPI
TestNativeTopPreservesForbiddenMetricsError
TestNativeLogsThroughSpecifiedCluster
TestNativeLogsUsesContextDefaultCluster
TestNativeLogsResolvesWorkloadWithDiscoveryFallback
TestNativeLogsFollowStopsWhenContextIsCancelled
TestNativeDescribeUsesContextDefaultCluster
TestNativeDescribeThroughKSApiServer
```

Use this exact argument transformation:

```go
// Before:
[]string{"logs", "demo-pod", ...}

// After:
[]string{"kube", "logs", "demo-pod", ...}
```

Keep every top-level `get` test unchanged.

- [ ] **Step 7: Update the plugin conflict test to reflect the new root**

Replace `TestPluginListReportsLogsAndTopAsBuiltInConflicts` with a test that
creates `ksctl-get`, `ksctl-kube`, `ksctl-logs`, and `ksctl-top`. Expect only
the first two to conflict:

```go
func TestPluginListReportsRootGetAndKubeAsBuiltInConflicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test plugin fixtures use Unix executable mode bits")
	}
	directory := t.TempDir()
	for _, name := range []string{"ksctl-get", "ksctl-kube", "ksctl-logs", "ksctl-top"} {
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
		`ksctl-get overwrites existing command: "ksctl get"`,
		`ksctl-kube overwrites existing command: "ksctl kube"`,
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want %q", errOut.String(), want)
		}
	}
	for _, unwanted := range []string{"ksctl-logs overwrites", "ksctl-top overwrites"} {
		if strings.Contains(errOut.String(), unwanted) {
			t.Fatalf("stderr = %q, does not want %q", errOut.String(), unwanted)
		}
	}
}
```

- [ ] **Step 8: Run the focused tests and verify the expected RED state**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go test ./pkg/cmd \
  -run 'Test(RootRegistersKubeResourceCommands|RootAndKubeConnectionFlagScopes|KubeResourceHelpUsesEntrypointDisplayName|RootGetAndKubeGetProduceEquivalentResults|KubeRequestTimeoutLimitsRawGet|NativeTop.*|NativeLogs.*|NativeDescribe.*|PluginListReportsRootGetAndKubeAsBuiltInConflicts)$' \
  -count=1
```

Expected: FAIL because `kube` is not registered, root describe/logs/top still
exist, and `--request-timeout` is still a root flag.

- [ ] **Step 9: Reduce `resource_commands.go` to top-level get and shared helpers**

Replace its command assembly with:

```go
package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newRootGetCommand(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) *cobra.Command {
	command := getcmd.NewCmdGet(displayName, factory, streams)
	addNamespaceFlag(command, namespace)
	rewriteKubectlExamples(command, displayName)
	return command
}

func addNamespaceFlag(command *cobra.Command, namespace *string) {
	command.Flags().StringVarP(
		namespace,
		"namespace",
		"n",
		"",
		"Kubernetes namespace or KubeSphere project",
	)
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

- [ ] **Step 10: Move the top adapter into `kube_top.go`**

Create `pkg/cmd/kube_top.go` with the existing adapter but remove the
command-local Namespace flag:

```go
package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	topcmd "k8s.io/kubectl/pkg/cmd/top"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newKubeTopCommand(
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
		newKubeTopPodCommand(factory, streams),
		newKubeTopNodeCommand(factory, streams),
	)
	return command
}

func newKubeTopPodCommand(
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

func newKubeTopNodeCommand(
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

- [ ] **Step 11: Add the initial `kube` assembler for migrated commands**

Create `pkg/cmd/kube.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func newKubeCommand(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace, requestTimeout *string,
) *cobra.Command {
	kubeDisplayName := displayName + " kube"
	command := &cobra.Command{
		Use:   "kube",
		Short: "Manage Kubernetes resources through KubeSphere",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.PersistentFlags().StringVarP(
		namespace,
		"namespace",
		"n",
		"",
		"Kubernetes namespace or KubeSphere project",
	)
	command.PersistentFlags().StringVar(
		requestTimeout,
		"request-timeout",
		"0",
		"The length of time to wait before giving up on a single server request",
	)
	command.AddCommand(
		getcmd.NewCmdGet(kubeDisplayName, factory, streams),
		describecmd.NewCmdDescribe(kubeDisplayName, factory, streams),
		logscmd.NewCmdLogs(factory, streams),
		newKubeTopCommand(factory, streams),
	)
	rewriteKubectlExamples(command, kubeDisplayName)
	return command
}
```

- [ ] **Step 12: Rewire the root and remove its timeout flag**

In `pkg/cmd/root.go`, delete:

```go
cmd.PersistentFlags().StringVar(&connection.RequestTimeout, "request-timeout", "0", "The length of time to wait before giving up on a single server request")
```

Replace the `newResourceCommands` registration with:

```go
cmd.AddCommand(newRootGetCommand(
	cmd.DisplayName(),
	factory,
	kubeStreams,
	&connection.Namespace,
))
cmd.AddCommand(newKubeCommand(
	cmd.DisplayName(),
	factory,
	kubeStreams,
	&connection.Namespace,
	&connection.RequestTimeout,
))
```

- [ ] **Step 13: Format and run the focused GREEN tests**

Run:

```bash
gofmt -w pkg/cmd/kube.go pkg/cmd/kube_top.go pkg/cmd/kube_test.go \
  pkg/cmd/root.go pkg/cmd/resource_commands.go \
  pkg/cmd/root_test.go pkg/cmd/resource_commands_test.go

env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go test ./pkg/cmd \
  -run 'Test(RootRegistersKubeResourceCommands|RootAndKubeConnectionFlagScopes|KubeResourceHelpUsesEntrypointDisplayName|RootGetAndKubeGetProduceEquivalentResults|KubeRequestTimeoutLimitsRawGet|NativeTop.*|NativeLogs.*|NativeDescribe.*|PluginListReportsRootGetAndKubeAsBuiltInConflicts)$' \
  -count=1
```

Expected: PASS.

- [ ] **Step 14: Run the complete command-package regression suite**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/cmd -count=1
```

Expected: PASS with no stale root describe/logs/top expectations.

- [ ] **Step 15: Commit the namespace migration**

```bash
git add pkg/cmd/kube.go pkg/cmd/kube_top.go pkg/cmd/kube_test.go \
  pkg/cmd/root.go pkg/cmd/resource_commands.go \
  pkg/cmd/root_test.go pkg/cmd/resource_commands_test.go
git commit -m "add kube resource command namespace"
```

---

### Task 2: Assemble the Complete Kubernetes Operation Command Set

**Files:**
- Modify: `pkg/cmd/kube.go`
- Modify: `pkg/cmd/kube_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `newKubeCommand`, `newKubeTopCommand`, the shared Factory, and kubectl v0.36.2 exported constructors.
- Produces: the exact operation command inventory specified by `2026-07-30-ksctl-kube-command-suite-design.md`.
- Produces: grouped `kube --help` output and recursive `ksctl kube`/`unictl ks kube` examples.

- [ ] **Step 1: Add the failing exact-inventory and exclusion test**

Append to `pkg/cmd/kube_test.go`:

```go
func TestKubeRegistersCompleteOperationCommandSet(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	kube := findSubcommand(root, "kube")
	if kube == nil {
		t.Fatal("kube command is not registered")
	}

	want := []string{
		"annotate",
		"api-resources",
		"api-versions",
		"apply",
		"attach",
		"auth",
		"autoscale",
		"certificate",
		"cluster-info",
		"cordon",
		"cp",
		"create",
		"debug",
		"delete",
		"describe",
		"diff",
		"drain",
		"edit",
		"events",
		"exec",
		"explain",
		"expose",
		"get",
		"kustomize",
		"label",
		"logs",
		"patch",
		"port-forward",
		"proxy",
		"replace",
		"rollout",
		"run",
		"scale",
		"set",
		"taint",
		"top",
		"uncordon",
		"wait",
	}
	for _, name := range want {
		if findSubcommand(kube, name) == nil {
			t.Errorf("kube %s is not registered", name)
		}
	}
	for _, name := range []string{
		"alpha",
		"completion",
		"config",
		"kuberc",
		"options",
		"plugin",
		"version",
	} {
		if findSubcommand(kube, name) != nil {
			t.Errorf("excluded kube %s is registered", name)
		}
	}
}
```

- [ ] **Step 2: Add failing nested-command and representative-flag tests**

Append:

```go
func TestKubePreservesRepresentativeKubectlSubcommandsAndFlags(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	kube := findSubcommand(root, "kube")

	for parent, children := range map[string][]string{
		"auth":    {"can-i", "reconcile", "whoami"},
		"rollout": {"history", "pause", "restart", "resume", "status", "undo"},
		"set":     {"env", "image", "resources", "selector", "serviceaccount", "subject"},
	} {
		command := findSubcommand(kube, parent)
		if command == nil {
			t.Fatalf("kube %s is not registered", parent)
		}
		for _, child := range children {
			if findSubcommand(command, child) == nil {
				t.Errorf("kube %s %s is not registered", parent, child)
			}
		}
	}

	for path, flags := range map[string][]string{
		"apply": {"server-side"},
		"drain": {"ignore-daemonsets"},
		"exec":  {"stdin", "tty"},
	} {
		command := findSubcommand(kube, path)
		if command == nil {
			t.Fatalf("kube %s is not registered", path)
		}
		for _, flag := range flags {
			if command.Flags().Lookup(flag) == nil {
				t.Errorf("kube %s --%s is not registered", path, flag)
			}
		}
	}
}
```

- [ ] **Step 3: Add failing grouped-help and recursive example tests**

Append:

```go
func TestKubeHelpGroupsOperationCommands(t *testing.T) {
	out := new(bytes.Buffer)
	root := NewRootCommand(
		IOStreams{Out: out, ErrOut: out},
		VersionInfo{Version: "dev"},
	)
	root.SetArgs([]string{"kube", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{
		"Basic Commands (Beginner):",
		"Deploy Commands:",
		"Cluster Management Commands:",
		"Troubleshooting and Debugging Commands:",
		"Advanced Commands:",
		"Settings Commands:",
		"Discovery Commands:",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("kube help missing %q:\n%s", want, out.String())
		}
	}
}

func TestKubeOperationHelpUsesEntrypointDisplayName(t *testing.T) {
	for _, test := range []struct {
		name    string
		newRoot func() *cobra.Command
		want    string
	}{
		{
			name: "standalone",
			newRoot: func() *cobra.Command {
				return NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: "ksctl kube",
		},
		{
			name: "alternate entrypoint",
			newRoot: func() *cobra.Command {
				return newTestEntrypointCommand(t, IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: "fixture entrypoint kube",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, path := range [][]string{
				{"apply", "--help"},
				{"rollout", "status", "--help"},
			} {
				out := new(bytes.Buffer)
				root := test.newRoot()
				root.SetOut(out)
				root.SetErr(out)
				root.SetArgs(append([]string{"kube"}, path...))
				if err := root.Execute(); err != nil {
					t.Fatalf("Execute(%v) error = %v", path, err)
				}
				if !strings.Contains(out.String(), test.want) {
					t.Fatalf("help = %q, want %q", out.String(), test.want)
				}
				if strings.Contains(out.String(), "kubectl apply") ||
					strings.Contains(out.String(), "kubectl rollout") {
					t.Fatalf("help contains stale kubectl invocation: %s", out.String())
				}
			}
		})
	}
}
```

Add `strings` and `github.com/spf13/cobra` to `kube_test.go` imports if Task 1
did not already require them.

- [ ] **Step 4: Add a failing representative mutation-routing test**

Append to `pkg/cmd/kube_test.go`, adding `net/http/httptest`,
`sync`, and Kubernetes `metav1` imports as needed:

```go
func TestKubeDeleteRoutesMutationThroughSpecifiedCluster(t *testing.T) {
	const prefix = "/clusters/member"
	var lock sync.Mutex
	var deletePaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("request path %q is not scoped to %q", r.URL.Path, prefix)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, prefix)
		switch path {
		case "/api":
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
				Versions: []string{"v1"},
			})
		case "/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
			})
		case "/api/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name:         "pods",
					SingularName: "pod",
					Namespaced:   true,
					Kind:         "Pod",
					Verbs:        metav1.Verbs{"delete", "get", "list"},
					ShortNames:   []string{"po"},
				}},
			})
		case "/api/v1/namespaces/default/pods/demo-pod":
			switch r.Method {
			case http.MethodGet:
				writeAPIJSON(t, w, podObject())
			case http.MethodDelete:
				lock.Lock()
				deletePaths = append(deletePaths, r.URL.Path)
				lock.Unlock()
				writeAPIJSON(t, w, podObject())
			default:
				t.Errorf("method = %s, want GET or DELETE", r.Method)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	command := NewRootCommand(
		IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	command.SetArgs([]string{
		"kube", "delete", "pod/demo-pod",
		"--namespace", "default",
		"--wait=false",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "member",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	lock.Lock()
	defer lock.Unlock()
	want := prefix + "/api/v1/namespaces/default/pods/demo-pod"
	if len(deletePaths) != 1 || deletePaths[0] != want {
		t.Fatalf("DELETE paths = %v, want [%s]", deletePaths, want)
	}
}
```

- [ ] **Step 5: Run the new tests and verify the expected RED state**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go test ./pkg/cmd \
  -run 'TestKube(RegistersCompleteOperationCommandSet|PreservesRepresentativeKubectlSubcommandsAndFlags|HelpGroupsOperationCommands|OperationHelpUsesEntrypointDisplayName|DeleteRoutesMutationThroughSpecifiedCluster)$' \
  -count=1
```

Expected: FAIL because only get/describe/logs/top are registered.

- [ ] **Step 6: Replace `kube.go` with the complete upstream assembly**

Use the following imports and implementation:

```go
package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/kubectl/pkg/cmd/annotate"
	"k8s.io/kubectl/pkg/cmd/apiresources"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/attach"
	kubectlauth "k8s.io/kubectl/pkg/cmd/auth"
	"k8s.io/kubectl/pkg/cmd/autoscale"
	"k8s.io/kubectl/pkg/cmd/certificates"
	"k8s.io/kubectl/pkg/cmd/clusterinfo"
	"k8s.io/kubectl/pkg/cmd/cp"
	"k8s.io/kubectl/pkg/cmd/create"
	"k8s.io/kubectl/pkg/cmd/debug"
	deletecmd "k8s.io/kubectl/pkg/cmd/delete"
	describecmd "k8s.io/kubectl/pkg/cmd/describe"
	"k8s.io/kubectl/pkg/cmd/diff"
	"k8s.io/kubectl/pkg/cmd/drain"
	"k8s.io/kubectl/pkg/cmd/edit"
	"k8s.io/kubectl/pkg/cmd/events"
	cmdexec "k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/explain"
	"k8s.io/kubectl/pkg/cmd/expose"
	getcmd "k8s.io/kubectl/pkg/cmd/get"
	"k8s.io/kubectl/pkg/cmd/kustomize"
	"k8s.io/kubectl/pkg/cmd/label"
	logscmd "k8s.io/kubectl/pkg/cmd/logs"
	"k8s.io/kubectl/pkg/cmd/patch"
	"k8s.io/kubectl/pkg/cmd/portforward"
	"k8s.io/kubectl/pkg/cmd/proxy"
	"k8s.io/kubectl/pkg/cmd/replace"
	"k8s.io/kubectl/pkg/cmd/rollout"
	"k8s.io/kubectl/pkg/cmd/run"
	"k8s.io/kubectl/pkg/cmd/scale"
	"k8s.io/kubectl/pkg/cmd/set"
	"k8s.io/kubectl/pkg/cmd/taint"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/cmd/wait"
	utilcomp "k8s.io/kubectl/pkg/util/completion"
	"k8s.io/kubectl/pkg/util/templates"
)

func newKubeCommand(
	displayName string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace, requestTimeout *string,
) *cobra.Command {
	kubeDisplayName := displayName + " kube"
	command := &cobra.Command{
		Use:   "kube",
		Short: "Manage Kubernetes resources through KubeSphere",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.PersistentFlags().StringVarP(
		namespace,
		"namespace",
		"n",
		"",
		"Kubernetes namespace or KubeSphere project",
	)
	command.PersistentFlags().StringVar(
		requestTimeout,
		"request-timeout",
		"0",
		"The length of time to wait before giving up on a single server request",
	)

	getCommand := getcmd.NewCmdGet(kubeDisplayName, factory, streams)
	getCommand.ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(factory)
	debugCommand := debug.NewCmdDebug(factory, streams)
	debugCommand.ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(factory)

	groups := templates.CommandGroups{
		{
			Message: "Basic Commands (Beginner):",
			Commands: []*cobra.Command{
				create.NewCmdCreate(factory, streams),
				expose.NewCmdExposeService(factory, streams),
				run.NewCmdRun(factory, streams),
				set.NewCmdSet(factory, streams),
			},
		},
		{
			Message: "Basic Commands (Intermediate):",
			Commands: []*cobra.Command{
				explain.NewCmdExplain(kubeDisplayName, factory, streams),
				getCommand,
				edit.NewCmdEdit(factory, streams),
				deletecmd.NewCmdDelete(factory, streams),
			},
		},
		{
			Message: "Deploy Commands:",
			Commands: []*cobra.Command{
				rollout.NewCmdRollout(kubeDisplayName, factory, streams),
				scale.NewCmdScale(factory, streams),
				autoscale.NewCmdAutoscale(factory, streams),
			},
		},
		{
			Message: "Cluster Management Commands:",
			Commands: []*cobra.Command{
				certificates.NewCmdCertificate(factory, streams),
				clusterinfo.NewCmdClusterInfo(factory, streams),
				newKubeTopCommand(factory, streams),
				drain.NewCmdCordon(factory, streams),
				drain.NewCmdUncordon(factory, streams),
				drain.NewCmdDrain(factory, streams),
				taint.NewCmdTaint(factory, streams),
			},
		},
		{
			Message: "Troubleshooting and Debugging Commands:",
			Commands: []*cobra.Command{
				describecmd.NewCmdDescribe(kubeDisplayName, factory, streams),
				logscmd.NewCmdLogs(factory, streams),
				attach.NewCmdAttach(factory, streams),
				cmdexec.NewCmdExec(factory, streams),
				portforward.NewCmdPortForward(factory, streams),
				proxy.NewCmdProxy(factory, streams),
				cp.NewCmdCp(factory, streams),
				kubectlauth.NewCmdAuth(factory, streams),
				debugCommand,
				events.NewCmdEvents(factory, streams),
			},
		},
		{
			Message: "Advanced Commands:",
			Commands: []*cobra.Command{
				diff.NewCmdDiff(factory, streams),
				apply.NewCmdApply(kubeDisplayName, factory, streams),
				patch.NewCmdPatch(factory, streams),
				replace.NewCmdReplace(factory, streams),
				wait.NewCmdWait(factory, streams),
				kustomize.NewCmdKustomize(streams),
			},
		},
		{
			Message: "Settings Commands:",
			Commands: []*cobra.Command{
				label.NewCmdLabel(factory, streams),
				annotate.NewCmdAnnotate(kubeDisplayName, factory, streams),
			},
		},
		{
			Message: "Discovery Commands:",
			Commands: []*cobra.Command{
				apiresources.NewCmdAPIVersions(factory, streams),
				apiresources.NewCmdAPIResources(factory, streams),
			},
		},
	}
	groups.Add(command)
	templates.ActsAsRootCommand(command, nil, groups...)
	rewriteKubectlExamples(command, kubeDisplayName)
	return command
}
```

Do not import kubectl's top-level `pkg/cmd`, config, completion, plugin,
version, options, kuberc, or alpha packages.

- [ ] **Step 7: Format and let Go report the newly activated module graph**

Run:

```bash
gofmt -w pkg/cmd/kube.go pkg/cmd/kube_test.go
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go test ./pkg/cmd -run TestKubeRegistersCompleteOperationCommandSet -count=1
```

Expected before tidy: setup may fail with missing `go.sum` entries for modules
such as `github.com/gorilla/websocket`, `k8s.io/streaming`,
`github.com/jonboulle/clockwork`, `github.com/distribution/reference`,
`github.com/mxk/go-flowrate`, and the kustomize command package. This is the
expected dependency activation, not a reason to remove commands.

- [ ] **Step 8: Tidy the module graph**

Run:

```bash
go mod tidy
```

Expected: `go.mod`/`go.sum` add only modules required by the imported operation
packages; Kubernetes modules remain aligned at v0.36.2.

- [ ] **Step 9: Run the complete kube RED-to-GREEN test set**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go test ./pkg/cmd \
  -run 'TestKube(RegistersCompleteOperationCommandSet|PreservesRepresentativeKubectlSubcommandsAndFlags|HelpGroupsOperationCommands|OperationHelpUsesEntrypointDisplayName|DeleteRoutesMutationThroughSpecifiedCluster)$' \
  -count=1
```

Expected: PASS. If the delete test reveals an additional standard discovery
or GET request, extend only the fake server response for that exact upstream
request; do not weaken the assertion that every path except the deliberate
core fallback is under `/clusters/member`.

- [ ] **Step 10: Run package and module consistency checks**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/cmd -count=1
go mod tidy -diff
git diff --check
```

Expected: PASS; tidy prints no diff.

- [ ] **Step 11: Commit the complete operation suite**

```bash
git add pkg/cmd/kube.go pkg/cmd/kube_test.go go.mod go.sum
git commit -m "add kubernetes operation command suite"
```

---

### Task 3: Update User Documentation and the Unreleased Migration Notice

**Files:**
- Modify: `README.md`
- Modify: `docs/cli.md`
- Modify: `docs/cli_zh.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the implemented command tree and approved design vocabulary.
- Produces: English and Chinese user guidance for `get`, `kube`, mutations, `--cluster`, and breaking migration paths.
- Produces: `[Unreleased]` notes without changing `[0.2.0]`.

- [ ] **Step 1: Update the README feature boundary and examples**

Replace the read-only resource summary with:

```markdown
- Inspect Kubernetes and discovered KubeSphere resources concisely with the
  top-level `get` command.
- Use `kube` for the complete kubectl-compatible Kubernetes operation command
  set, routed through KubeSphere authentication and member-Cluster selection.

Top-level `get` and `tenant get` are read-only. Commands under `kube` may
change the selected Kubernetes Cluster.
```

Update Quick start examples to:

```bash
ksctl auth login
ksctl get pods -A
ksctl kube describe deployment web -n demo
ksctl kube logs deployment/web -n demo --all-pods
ksctl kube top pod -n demo
ksctl kube apply -f app.yaml --cluster member-1
ksctl tenant get workspace
```

Replace the resource rows in the command overview with:

```markdown
| `get` | Read Kubernetes and discovered KubeSphere resources. |
| `kube` | Run Kubernetes read, mutation, rollout, debugging, streaming, and cluster-management operations. |
```

Mention once that the release companion renders the same tree as
`unictl ks kube ...`.

- [ ] **Step 2: Rewrite the English CLI Kubernetes section around `kube`**

In `docs/cli.md`:

1. Keep top-level `get` syntax and examples.
2. Change describe/logs/top syntax to:

```text
ksctl kube describe TYPE [NAME_PREFIX]
ksctl kube logs (POD | TYPE/NAME)
ksctl kube top (pod | node) [NAME]
```

3. Add this operation-category table:

```markdown
| Category | `kube` commands |
| --- | --- |
| Basic | `create`, `expose`, `run`, `set`, `explain`, `get`, `edit`, `delete` |
| Deployment | `rollout`, `scale`, `autoscale` |
| Cluster management | `certificate`, `cluster-info`, `top`, `cordon`, `uncordon`, `drain`, `taint` |
| Troubleshooting | `describe`, `logs`, `attach`, `exec`, `port-forward`, `proxy`, `cp`, `auth`, `debug`, `events` |
| Advanced | `diff`, `apply`, `patch`, `replace`, `wait`, `kustomize` |
| Settings and discovery | `label`, `annotate`, `api-versions`, `api-resources` |
```

4. Add this migration notice:

```markdown
> **Breaking change:** `describe`, `logs`, and `top` now run only under
> `ksctl kube`. The former top-level paths are removed. `--request-timeout`
> also moved from the root to `ksctl kube`, while `--cluster` remains a ksctl
> root flag inherited by all `kube` operations.
```

5. Replace every active `ksctl describe`, `ksctl logs`, and `ksctl top`
example with the corresponding `ksctl kube ...` form.
6. Change the flag table so `--request-timeout` appears only in the `kube`
scope.
7. State explicitly that `kube` does not expose kubeconfig self-management
commands and does not read `~/.kube/config`.

- [ ] **Step 3: Mirror the same user contract in Chinese**

In `docs/cli_zh.md`, use:

```markdown
> **破坏性变更：** `describe`、`logs` 和 `top` 现在只能通过
> `ksctl kube` 使用，原顶层命令已删除。`--request-timeout` 也从根命令
> 移至 `ksctl kube`；`--cluster` 仍是 ksctl 根参数，并由所有 `kube`
> 操作继承。
```

Use this category table:

```markdown
| 分类 | `kube` 命令 |
| --- | --- |
| 基础操作 | `create`、`expose`、`run`、`set`、`explain`、`get`、`edit`、`delete` |
| 部署管理 | `rollout`、`scale`、`autoscale` |
| Cluster 管理 | `certificate`、`cluster-info`、`top`、`cordon`、`uncordon`、`drain`、`taint` |
| 排障与调试 | `describe`、`logs`、`attach`、`exec`、`port-forward`、`proxy`、`cp`、`auth`、`debug`、`events` |
| 高级操作 | `diff`、`apply`、`patch`、`replace`、`wait`、`kustomize` |
| 设置与发现 | `label`、`annotate`、`api-versions`、`api-resources` |
```

Keep top-level `get` examples. Rewrite all active describe/logs/top examples
as `ksctl kube ...`, document mutations and RBAC, move the timeout flag row to
the `kube` scope, and mention `unictl ks kube ...` as the equivalent release
entry point.

- [ ] **Step 4: Add only Unreleased changelog entries**

Under `## [Unreleased]`, add:

```markdown
### Added

- Add the `kube` command suite with kubectl-compatible Kubernetes reads,
  mutations, rollouts, debugging, streaming, and Cluster management through
  KubeSphere authentication and `--cluster` routing.

### Changed

- **Breaking:** Move `describe`, `logs`, and `top` from the root to
  `kube describe`, `kube logs`, and `kube top`; remove the former top-level
  paths.
- **Breaking:** Move `--request-timeout` from the ksctl root to the `kube`
  command group.
```

Do not change any line inside `## [0.2.0]`.

- [ ] **Step 5: Verify user documentation against the live command tree**

Build a temporary binary:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go build -trimpath -o /private/tmp/ksctl-kube-docs ./cmd/ksctl
```

Run:

```bash
/private/tmp/ksctl-kube-docs --help
/private/tmp/ksctl-kube-docs kube --help
/private/tmp/ksctl-kube-docs kube apply --help

rg -n 'ksctl (describe|logs|top)( |$)' README.md docs/cli.md docs/cli_zh.md
rg -n 'request-timeout' README.md docs/cli.md docs/cli_zh.md
git diff --check
```

Expected:

- live help matches the documented inventory;
- the first search finds no active invocation outside explicitly marked
  migration text;
- timeout references describe only `kube` scope;
- no whitespace errors.

- [ ] **Step 6: Commit user documentation**

```bash
git add README.md docs/cli.md docs/cli_zh.md CHANGELOG.md
git commit -m "document kube command suite"
```

---

### Task 4: Update the English and Chinese Architecture Documents

**Files:**
- Modify: `docs/design.md`
- Modify: `docs/design_zh.md`

**Interfaces:**
- Consumes: `pkg/cmd/kube.go`, `pkg/cmd/kube_top.go`, the approved design spec, and the implemented security boundary.
- Produces: matching English and Chinese current-architecture references.

- [ ] **Step 1: Rewrite the English goals and non-goals**

In `docs/design.md`, replace the generic read-only boundary with:

```markdown
- Keep top-level `get` and tenant inspection read-only.
- Expose generic Kubernetes reads and mutations under `kube`, using the same
  KubeSphere authentication and Cluster routing.
- Keep Extension lifecycle writes in their purpose-built workflow.
```

Remove generic Kubernetes mutations from Non-goals. Retain these non-goals:

```markdown
- A local reimplementation or fork of kubectl operation behavior
- Reading, merging, or writing `~/.kube/config`
- kubectl CLI self-management commands under `kube`
- Automatic fallback of failed member-Cluster operations to the host Cluster
```

- [ ] **Step 2: Document command assembly and data flow**

Replace the current four-command pipeline with:

```text
ksctl kube COMMAND
  -> upstream kubectl v0.36.2 command/options
  -> shared kubectl Factory
  -> ksctl Kubernetes RESTClientGetter
  -> discovery/RESTMapper/client as required
  -> KubeSphere Endpoint or /clusters/<cluster>
  -> Kubernetes API
```

Document:

- top-level `get` is a second instance of the upstream get constructor;
- `kube` owns persistent `--namespace` and `--request-timeout`;
- root `--cluster` scopes all remote kube operations;
- the assembler tracks the upstream v0.36.2 operation list;
- config/plugin/version/completion/options/kuberc/empty alpha are excluded;
- `exec`, `attach`, `cp`, `port-forward`, and `proxy` keep upstream transport
  behavior through the routed REST config; and
- `top` replaces only the upstream options' DiscoveryClient after Complete.

- [ ] **Step 3: Rewrite the security boundary**

Use this exact distinction:

```markdown
Top-level `get` and tenant inspection do not mutate resources. The `kube`
namespace is a general Kubernetes administration surface: commands such as
apply, delete, drain, debug, and rollout may change the selected Cluster and
are authorized by Kubernetes RBAC for the resolved KubeSphere credential.
ksctl does not add confirmation or retry a member-Cluster failure against the
host Cluster.
```

Keep the existing Extension and raw API security discussions, adjusting only
statements that previously claimed all kubectl-backed commands were read-only.

- [ ] **Step 4: Mirror every architecture change in Chinese**

In `docs/design_zh.md`, use the same section ordering and technical detail.
The core boundary must read:

```markdown
顶层 `get` 和租户查看不会变更资源。`kube` 是通用 Kubernetes 管理界面：
`apply`、`delete`、`drain`、`debug` 和 `rollout` 等命令可能变更选中的
Cluster，并由解析出的 KubeSphere 凭据所具备的 Kubernetes RBAC 权限授权。
ksctl 不增加确认步骤，也不会在成员 Cluster 请求失败后改为操作 host Cluster。
```

Use the same command/data-flow diagram and keep names such as Factory,
RESTClientGetter, DiscoveryClient, SPDY, WebSocket, and HTTP Upgrade
technically aligned with the English document.

- [ ] **Step 5: Verify the architecture mirrors**

Run:

```bash
rg -n 'read-only|只读|kube|request-timeout|DiscoveryClient|SPDY|WebSocket|HTTP Upgrade' \
  docs/design.md docs/design_zh.md
rg -n 'kubectl ks|kubectl-ks' docs/design.md docs/design_zh.md
git diff --check
```

Expected:

- both documents describe the same command, routing, mutation, timeout, top,
  and streaming boundaries;
- no active `kubectl ks` entry-point claim remains;
- no whitespace errors.

- [ ] **Step 6: Commit architecture documentation**

```bash
git add docs/design.md docs/design_zh.md
git commit -m "update kube command architecture"
```

---

### Task 5: Run Full Verification and Entry-Point Smoke Tests

**Files:**
- Verify only; no planned source modifications.

**Interfaces:**
- Consumes: all prior task commits.
- Produces: evidence that formatting, modules, vet, normal tests, race tests, both binaries, help paths, and workspace cleanliness satisfy the repository gates.

- [ ] **Step 1: Confirm no accidental staging-tree or published changelog edits**

Run:

```bash
git diff d2338cd -- staging
git diff d2338cd -- CHANGELOG.md
```

Expected:

- no `staging/` diff;
- changelog changes occur only below `[Unreleased]` and before `[0.2.0]`.

- [ ] **Step 2: Run the repository verification gate**

Run:

```bash
make verify
```

Expected: formatting, module checks, vet, normal tests, race tests, and the
release-style `ksctl` build all pass.

- [ ] **Step 3: Build both entry points outside the repository**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go build -trimpath -o /private/tmp/ksctl-kube-final ./cmd/ksctl
env GOCACHE=/private/tmp/ksctl-go-build-cache \
  go build -trimpath -o /private/tmp/unictl-ks-kube-final ./cmd/unictl-ks
```

Expected: both builds succeed.

- [ ] **Step 4: Smoke-test command shape, exclusions, and display names**

Run:

```bash
/private/tmp/ksctl-kube-final get --help
/private/tmp/ksctl-kube-final kube --help
/private/tmp/ksctl-kube-final kube get --help
/private/tmp/ksctl-kube-final kube apply --help
/private/tmp/unictl-ks-kube-final kube get --help
/private/tmp/unictl-ks-kube-final kube apply --help

if /private/tmp/ksctl-kube-final describe --help >/private/tmp/ksctl-root-describe.out 2>&1; then
  echo "root describe unexpectedly succeeded"
  exit 1
fi
if /private/tmp/ksctl-kube-final kube config --help >/private/tmp/ksctl-kube-config.out 2>&1; then
  echo "kube config unexpectedly succeeded"
  exit 1
fi
```

Expected:

- root get help succeeds;
- kube help lists all operation groups;
- nested help uses `ksctl kube ...` or `unictl ks kube ...`;
- root describe and excluded kube config do not expose a valid usage path.

- [ ] **Step 5: Record the live KubeSphere transport validation status**

The unit suite deliberately does not emulate SPDY or WebSocket. If the user
has supplied an approved non-production KubeSphere environment, validate this
matrix against both the host Cluster and one member Cluster:

| Command | Acceptance criterion |
| --- | --- |
| `kube exec` | An interactive or non-interactive command runs in a Pod and returns its exact output. |
| `kube attach` | The client attaches to an existing container stream and exits without bypassing KubeSphere. |
| `kube cp` | A small file round-trips between the local machine and a container that has `tar`. |
| `kube port-forward` | A local TCP connection reaches the selected Pod or Service and closes cleanly. |

For the member row, include `--cluster` and confirm the target exists only in
that member Cluster. For the host row, omit `--cluster`. Do not create or
delete resources in a production environment.

If no approved live environment is available, do not fabricate a success.
State in the final handoff that the automated REST-routing tests passed but
the host/member upgraded-transport matrix remains a release validation gate.

- [ ] **Step 6: Verify clean diffs and module state**

Run:

```bash
go mod tidy -diff
git diff --check
git status --short
```

Expected:

- tidy prints no diff;
- diff check passes;
- status is clean;
- no generated binary is tracked or left under `bin/` beyond the ignored
  `make verify` output.
