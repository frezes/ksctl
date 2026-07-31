# ksctl Tenant Get Arbitrary Resources Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `tenant get` to query arbitrary Kubernetes resources while preserving native tenant resources and safely aggregating a tenant user's accessible Namespaces for `-A` and `--workspace`.

**Architecture:** Keep kubectl v0.36.2's `GetOptions` as the command, discovery, builder, and printing engine. Wrap only the tenant command's Kubernetes REST configuration with a scope-aware transport that falls back from a forbidden all-Namespaces collection request to bounded per-Namespace requests resolved through the KubeSphere tenant API, then merges complete List or Table responses before kubectl sees them.

**Tech Stack:** Go 1.26, Cobra/pflag, kubectl and cli-runtime v0.36.2, client-go REST/discovery, KubeSphere client-go, `httptest`, Go `testing`, and `golang.org/x/sync/errgroup`.

## Global Constraints

- Preserve `tenant get workspace|namespace|cluster`, their existing aliases, KubeSphere API paths, flags, and output.
- Do not change top-level `ksctl get` or `ksctl kube get`.
- Do not modify any file under `staging/`.
- `--workspace` is mutually exclusive with explicit `-n`/`--namespace` and `-A`/`--all-namespaces`.
- `--workspace` is valid only for namespaced collection queries and is rejected with raw URLs, filenames, named queries, root-scoped resources, and watch modes.
- A successful native all-Namespaces request remains a single request; only `403 Forbidden` activates tenant fallback.
- A tenant aggregate succeeds only when every Namespace and page succeeds.
- Namespace fan-out uses at most eight concurrent workers and merges in tenant API Namespace order.
- Tenant aggregation does not support `--watch` or `--watch-only`.
- Aggregate `continue` and `resourceVersion` are empty.
- No dependency is added; existing pinned Kubernetes and KubeSphere modules are reused.
- Production code follows TDD: every behavior change starts with a focused failing test whose failure is observed before implementation.

---

## File Map

- Modify `pkg/cmd/tenant/command.go`: retain native tenant handlers, introduce compatible constructor options, and mount native children below the generic get parent.
- Create `pkg/cmd/tenant/generic_get.go`: assemble kubectl `GetOptions`, tenant-only flags, validation, conditional output, and execution.
- Create `pkg/cmd/tenant/scope.go`: resolve and cache accessible KubeSphere Namespace names.
- Create `pkg/cmd/tenant/aggregate_request.go`: parse Kubernetes resource request paths and classify REST scope.
- Create `pkg/cmd/tenant/aggregate_response.go`: decode and merge Kubernetes List and Table documents.
- Create `pkg/cmd/tenant/aggregate_transport.go`: wrap REST configuration, perform direct/fallback/workspace routing, pagination, bounded fan-out, and synthetic responses.
- Modify `pkg/cmd/tenant/command_test.go`: command composition, flag validation, native compatibility, watch, and end-to-end aggregate tests.
- Create `pkg/cmd/tenant/scope_test.go`: Namespace resolver tests.
- Create `pkg/cmd/tenant/aggregate_request_test.go`: request parser and REST scope tests.
- Create `pkg/cmd/tenant/aggregate_response_test.go`: List/Table merge tests.
- Create `pkg/cmd/tenant/aggregate_transport_test.go`: routing, pagination, concurrency, ordering, cancellation, and atomic failure tests.
- Modify `pkg/cmd/root.go` and `pkg/cmd/root_test.go`: inject both connection getters, namespace option, active display name, and streams.
- Modify `README.md`, `README_zh.md`, `CHANGELOG.md`, `docs/cli.md`, `docs/cli_zh.md`, `docs/design.md`, and `docs/design_zh.md`: document the new surface and boundaries.

---

### Task 1: Mount kubectl Get Beneath Tenant Without Breaking Native Resources

**Files:**
- Modify: `pkg/cmd/tenant/command.go`
- Create: `pkg/cmd/tenant/generic_get.go`
- Modify: `pkg/cmd/tenant/command_test.go`
- Modify: `pkg/cmd/root.go`
- Modify: `pkg/cmd/root_test.go`

**Interfaces:**
- Consumes: existing `KubeSphereRESTClientGetter` behavior from `pkg/client/kubesphere/connection.RESTClientGetter`; `genericclioptions.RESTClientGetter`; `genericiooptions.IOStreams`.
- Produces:

```go
type CommandOptions struct {
	DisplayName      string
	KubeSphereGetter KubeSphereRESTClientGetter
	KubernetesGetter genericclioptions.RESTClientGetter
	Streams          genericiooptions.IOStreams
	Namespace        *string
}

func NewCommand(getter KubeSphereRESTClientGetter) *cobra.Command
func NewCommandWithOptions(options CommandOptions) *cobra.Command
```

- `NewCommand` remains as the compatibility constructor used by focused native tenant tests and callers.
- `NewCommandWithOptions` builds the production command with arbitrary-resource support.

- [ ] **Step 1: Write failing command-composition tests**

Add tests that name the regressions they catch:

```go
func TestCommandKeepsNativeResourcesAheadOfGenericGet(t *testing.T) {
	namespace := ""
	command := NewCommandWithOptions(CommandOptions{
		DisplayName:      "ksctl",
		KubeSphereGetter: fakeRESTClientGetter{},
		KubernetesGetter: newFakeKubernetesGetter("default"),
		Streams:          genericiooptions.IOStreams{Out: io.Discard, ErrOut: io.Discard},
		Namespace:        &namespace,
	})
	get := findCommand(command, "get")
	for _, name := range []string{"workspace", "namespace", "cluster"} {
		if findCommand(get, name) == nil {
			t.Fatalf("native child %q is missing", name)
		}
	}
	for _, name := range []string{"all-namespaces", "selector", "field-selector", "output", "workspace", "namespace"} {
		if get.Flags().Lookup(name) == nil {
			t.Fatalf("generic get flag --%s is missing", name)
		}
	}
}

func TestRootTenantGetRegistersArbitraryResourceFlagsOnlyOnTenantGet(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	tenantGet := findSubcommand(findSubcommand(root, "tenant"), "get")
	if tenantGet.Flags().Lookup("workspace") == nil {
		t.Fatal("tenant get --workspace is missing")
	}
	if findSubcommand(root, "get").Flags().Lookup("workspace") != nil {
		t.Fatal("top-level get unexpectedly has --workspace")
	}
	if findSubcommand(findSubcommand(root, "kube"), "get").Flags().Lookup("workspace") != nil {
		t.Fatal("kube get unexpectedly has --workspace")
	}
}
```

Update the existing registration test to continue asserting native children
and add an arbitrary-resource parent assertion. Use a fake Kubernetes getter
that implements all four `genericclioptions.RESTClientGetter` methods without
making a connection during command construction.

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant ./pkg/cmd -run 'TestCommandKeepsNativeResourcesAheadOfGenericGet|TestRootTenantGetRegistersArbitraryResourceFlagsOnlyOnTenantGet' -count=1
```

Expected: compilation fails because `CommandOptions` and
`NewCommandWithOptions` do not exist.

- [ ] **Step 3: Extract native children and add the compatibility constructor**

Rename the current narrow interface to make its protocol explicit:

```go
type KubeSphereRESTClientGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
	KubeSphereCluster() (string, error)
}
```

Keep:

```go
func NewCommand(getter KubeSphereRESTClientGetter) *cobra.Command {
	return newNativeOnlyCommand(getter)
}
```

Extract construction of the Workspace, Namespace, and Cluster children to:

```go
func newNativeGetCommands(getter KubeSphereRESTClientGetter) []*cobra.Command
```

Each native child retains its own existing `-o/--output`; Namespace and
Cluster retain their own long-form `--workspace`. Their `RunE` bodies continue
to call the unchanged `runGet`.

- [ ] **Step 4: Add a direct kubectl GetOptions command constructor**

In `generic_get.go`, create:

```go
func newGenericGetCommand(
	parent string,
	factory cmdutil.Factory,
	streams genericiooptions.IOStreams,
	namespace *string,
) (*cobra.Command, *getcmd.GetOptions) {
	options := getcmd.NewGetOptions(parent, streams)
	command := &cobra.Command{
		Use:                   fmt.Sprintf("get [(-o|--output=)%s] (TYPE [NAME | -l label] | TYPE/NAME ...) [flags]", strings.Join(options.PrintFlags.AllowedFormats(), "|")),
		DisableFlagsInUseLine: true,
		Short:                 "Display KubeSphere tenant resources or arbitrary Kubernetes resources",
		RunE: func(command *cobra.Command, args []string) error {
			if err := options.Complete(factory, command, args); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(factory, args)
		},
	}
	options.PrintFlags.AddFlags(command)
	command.Flags().StringVar(&options.Raw, "raw", options.Raw, "Raw URI to request from the server")
	command.Flags().BoolVarP(&options.Watch, "watch", "w", options.Watch, "After listing/getting the requested object, watch for changes.")
	command.Flags().BoolVar(&options.WatchOnly, "watch-only", options.WatchOnly, "Watch for changes without an initial list.")
	command.Flags().BoolVar(&options.OutputWatchEvents, "output-watch-events", options.OutputWatchEvents, "Output watch event objects.")
	command.Flags().BoolVar(&options.IgnoreNotFound, "ignore-not-found", options.IgnoreNotFound, "Suppress NotFound errors for named objects.")
	command.Flags().StringVar(&options.FieldSelector, "field-selector", options.FieldSelector, "Selector (field query) to filter on.")
	command.Flags().BoolVarP(&options.AllNamespaces, "all-namespaces", "A", options.AllNamespaces, "List the requested object(s) across all namespaces.")
	command.Flags().StringVarP(namespace, "namespace", "n", "", "Kubernetes namespace or KubeSphere project")
	command.Flags().BoolVar(&options.ServerPrint, "server-print", options.ServerPrint, "Have the server return table output.")
	cmdutil.AddFilenameOptionFlags(command, &options.FilenameOptions, "identifying the resource to get from a server.")
	cmdutil.AddChunkSizeFlag(command, &options.ChunkSize)
	cmdutil.AddLabelSelectorFlagVar(command, &options.LabelSelector)
	cmdutil.AddSubresourceFlags(command, &options.Subresource, "If specified, gets the subresource of the requested object.")
	return command, options
}
```

Use the exact upstream help strings where practical, register
`ValidArgsFunction = utilcomp.ResourceTypeAndNameCompletionFunc(factory)`, and
run the existing `rewriteKubectlExamples` equivalent local to the tenant
package with `parent + " tenant"` as the visible command parent.

- [ ] **Step 5: Build `NewCommandWithOptions` and wire root construction**

Implementation shape:

```go
func NewCommandWithOptions(options CommandOptions) *cobra.Command {
	command := &cobra.Command{Use: "tenant", Short: "Inspect KubeSphere tenant resources"}
	if options.KubernetesGetter == nil {
		return newNativeOnlyCommand(options.KubeSphereGetter)
	}
	factory := cmdutil.NewFactory(cmdutil.NewMatchVersionFlags(options.KubernetesGetter))
	get, _ := newGenericGetCommand(
		options.DisplayName+" tenant",
		factory,
		options.Streams,
		options.Namespace,
	)
	get.Flags().String("workspace", "", "KubeSphere workspace name")
	get.AddCommand(newNativeGetCommands(options.KubeSphereGetter)...)
	command.AddCommand(get)
	return command
}
```

In `pkg/cmd/root.go`, create the kubectl Factory after both getters are
constructed, then register:

```go
cmd.AddCommand(tenantcmd.NewCommandWithOptions(tenantcmd.CommandOptions{
	DisplayName:      cmd.DisplayName(),
	KubeSphereGetter: kubeSphereGetter,
	KubernetesGetter: kubernetesGetter,
	Streams:          kubeStreams,
	Namespace:        &connection.Namespace,
}))
```

Reuse the same `kubeStreams` for the root and kube command registrations.

- [ ] **Step 6: Run focused and existing tenant tests**

Run:

```bash
go test ./pkg/cmd/tenant ./pkg/cmd -run 'Tenant|CommandKeepsNative|ArbitraryResourceFlags' -count=1
```

Expected: PASS. Existing native route tests must remain unchanged.

- [ ] **Step 7: Commit the direct arbitrary-resource command surface**

```bash
git add pkg/cmd/tenant/command.go pkg/cmd/tenant/generic_get.go pkg/cmd/tenant/command_test.go pkg/cmd/root.go pkg/cmd/root_test.go
git diff --cached --check
git commit -m "add arbitrary resources to tenant get"
```

---

### Task 2: Resolve and Cache Tenant Namespaces

**Files:**
- Create: `pkg/cmd/tenant/scope.go`
- Create: `pkg/cmd/tenant/scope_test.go`

**Interfaces:**
- Consumes: `KubeSphereRESTClientGetter`, existing `Client.Get`, and
  `Request{Resource: ResourceNamespace, Workspace, Cluster}`.
- Produces:

```go
type namespaceResolver interface {
	Namespaces(ctx context.Context, workspace string) ([]string, error)
}

func newNamespaceResolver(getter KubeSphereRESTClientGetter) namespaceResolver
```

- [ ] **Step 1: Write failing resolver behavior tests**

Use an `httptest.Server` and test:

```go
func TestNamespaceResolverUsesClusterAndWorkspaceScope(t *testing.T)
func TestNamespaceResolverDeduplicatesNamesInServerOrder(t *testing.T)
func TestNamespaceResolverRejectsMissingOrInvalidName(t *testing.T)
func TestNamespaceResolverCachesEachWorkspaceForOneExecution(t *testing.T)
func TestNamespaceResolverReturnsIndependentSlices(t *testing.T)
```

The representative response and expected result are literals:

```go
response := `{"items":[
  {"metadata":{"name":"team-b"}},
  {"metadata":{"name":"team-a"}},
  {"metadata":{"name":"team-b"}}
]}`
want := []string{"team-b", "team-a"}
```

Assert the Workspace request path is exactly:

```text
/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces
```

Mutating the returned slice must not mutate the cached result returned by a
later call.

- [ ] **Step 2: Run resolver tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestNamespaceResolver' -count=1
```

Expected: compilation fails because the resolver does not exist.

- [ ] **Step 3: Implement request and extraction**

Use one cache entry per Workspace:

```go
type namespaceCacheEntry struct {
	once  sync.Once
	names []string
	err   error
}

type kubeSphereNamespaceResolver struct {
	getter  KubeSphereRESTClientGetter
	mu      sync.Mutex
	entries map[string]*namespaceCacheEntry
}
```

Inside the entry's `once.Do`:

1. Resolve the effective Cluster.
2. Resolve the KubeSphere REST configuration.
3. Build the existing KubeSphere REST client.
4. Call `Client.Get` with Namespace resource, Workspace, and Cluster.
5. Require a non-empty string at `metadata.name`.
6. Validate with `validation.IsDNS1123Label`.
7. Deduplicate with a map while appending only the first occurrence.

Return `slices.Clone(entry.names)` so callers cannot mutate cache state. Wrap
errors with `resolve tenant namespaces` and include the Workspace when set.

- [ ] **Step 4: Verify resolver tests and race safety**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestNamespaceResolver' -count=1
go test -race ./pkg/cmd/tenant -run '^TestNamespaceResolver' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the Namespace resolver**

```bash
git add pkg/cmd/tenant/scope.go pkg/cmd/tenant/scope_test.go
git diff --cached --check
git commit -m "resolve tenant namespaces for resource queries"
```

---

### Task 3: Parse Resource Requests and Merge Kubernetes Responses

**Files:**
- Create: `pkg/cmd/tenant/aggregate_request.go`
- Create: `pkg/cmd/tenant/aggregate_request_test.go`
- Create: `pkg/cmd/tenant/aggregate_response.go`
- Create: `pkg/cmd/tenant/aggregate_response_test.go`

**Interfaces:**
- Produces:

```go
type resourceEndpoint struct {
	GVR        schema.GroupVersionResource
	BasePath   string
	Resource   string
	Collection bool
	Namespace  string
}

func parseResourceEndpoint(hostPath, requestPath string) (resourceEndpoint, bool)
func (e resourceEndpoint) pathForNamespace(namespace string) string

type scopedDocument struct {
	Namespace string
	Body      []byte
}

func mergeDocuments(
	documents []scopedDocument,
	mapping *meta.RESTMapping,
	tableRequested bool,
) ([]byte, error)
```

- [ ] **Step 1: Write failing request parser tests**

Table-drive these literal cases:

```go
tests := []struct {
	hostPath    string
	requestPath string
	wantGVR     schema.GroupVersionResource
	wantNS      string
	wantPath    string
	wantOK      bool
}{
	{"/proxy/clusters/member", "/proxy/clusters/member/api/v1/pods",
		schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "",
		"/proxy/clusters/member/api/v1/namespaces/demo/pods", true},
	{"", "/apis/apps/v1/deployments",
		schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "",
		"/apis/apps/v1/namespaces/demo/deployments", true},
	{"", "/api/v1/namespaces/demo/pods",
		schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "demo", "", true},
	{"", "/apis/apps/v1/deployments/web", schema.GroupVersionResource{}, "", "", false},
	{"", "/apis", schema.GroupVersionResource{}, "", "", false},
}
```

The fourth case proves that named resource paths are not collection
aggregation candidates. Add encoded traversal and host-prefix mismatch cases
that return `false`.

- [ ] **Step 2: Run request parser tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestParseResourceEndpoint' -count=1
```

Expected: compilation fails because the parser does not exist.

- [ ] **Step 3: Implement strict path parsing**

Parse only these shapes after stripping the exact normalized REST config host
path:

```text
/api/{version}/{resource}
/api/{version}/namespaces/{namespace}/{resource}
/apis/{group}/{version}/{resource}
/apis/{group}/{version}/namespaces/{namespace}/{resource}
```

Require a collection path with no name or trailing subresource for
aggregation. `pathForNamespace` inserts
`namespaces/{url.PathEscape(namespace)}` between version and resource while
retaining the validated host prefix.

- [ ] **Step 4: Write failing List and Table merge tests**

Add:

```go
func TestMergeDocumentsCombinesListsAndClearsSharedMetadata(t *testing.T)
func TestMergeDocumentsCombinesTablesAndInjectsNamespaceMetadata(t *testing.T)
func TestMergeDocumentsRejectsMismatchedTypeOrColumns(t *testing.T)
func TestMergeDocumentsBuildsEmptyListAndTable(t *testing.T)
func TestMergeDocumentsRejectsMalformedOrNonListJSON(t *testing.T)
```

For Lists, assert literal order `team-b/pod-b`, then `team-a/pod-a`, and:

```go
if got.GetContinue() != "" || got.GetResourceVersion() != "" {
	t.Fatalf("aggregate metadata = continue %q resourceVersion %q", got.GetContinue(), got.GetResourceVersion())
}
```

For Tables, make the server rows omit `object`; after merging, convert the
result to `metav1.Table` and assert each row object's metadata Namespace is
the source Namespace. Use identical literal column definitions in the success
case and change `Name` to `Pod` in the mismatch case.

- [ ] **Step 5: Run merge tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestMergeDocuments' -count=1
```

Expected: compilation fails because `mergeDocuments` does not exist.

- [ ] **Step 6: Implement List and Table merging**

Decode each body to `unstructured.Unstructured`. The first body supplies the
aggregate envelope. Require matching `apiVersion` and `kind`.

For ordinary Lists:

```go
items, found, err := unstructured.NestedSlice(object.Object, "items")
```

Require `found`, append all items, set the merged slice, and set
`metadata.continue` and `metadata.resourceVersion` to empty strings.

For Tables:

1. Require `kind == "Table"` and group `meta.k8s.io`.
2. Decode `columnDefinitions` and compare with `apiequality.Semantic.DeepEqual`.
3. Decode `rows`.
4. When `row.object` is absent, insert:

```go
map[string]any{
	"apiVersion": mapping.GroupVersionKind.GroupVersion().String(),
	"kind":       mapping.GroupVersionKind.Kind,
	"metadata": map[string]any{
		"namespace": document.Namespace,
	},
}
```

5. Append rows and clear shared list metadata.

For zero documents, synthesize either the mapped resource List GVK or
`meta.k8s.io/v1, Kind=Table` with empty slices.

- [ ] **Step 7: Verify merge and parser tests**

Run:

```bash
go test ./pkg/cmd/tenant -run 'TestParseResourceEndpoint|TestMergeDocuments' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit request and response primitives**

```bash
git add pkg/cmd/tenant/aggregate_request.go pkg/cmd/tenant/aggregate_request_test.go pkg/cmd/tenant/aggregate_response.go pkg/cmd/tenant/aggregate_response_test.go
git diff --cached --check
git commit -m "add tenant response aggregation primitives"
```

---

### Task 4: Add the Aggregation-Aware REST Getter and Basic Fallback

**Files:**
- Create: `pkg/cmd/tenant/aggregate_transport.go`
- Create: `pkg/cmd/tenant/aggregate_transport_test.go`

**Interfaces:**
- Consumes: `namespaceResolver`, `parseResourceEndpoint`, `mergeDocuments`,
  and the original `genericclioptions.RESTClientGetter`.
- Produces:

```go
type aggregateMode uint8

const (
	aggregateDisabled aggregateMode = iota
	aggregateOnForbidden
	aggregateWorkspace
)

type aggregationState struct {
	mode      aggregateMode
	workspace string
	used      atomic.Bool
}

func newAggregatingRESTClientGetter(
	delegate genericclioptions.RESTClientGetter,
	resolver namespaceResolver,
	state *aggregationState,
) genericclioptions.RESTClientGetter
```

- [ ] **Step 1: Write failing getter and direct-routing tests**

Add:

```go
func TestAggregatingGetterDelegatesDiscoveryMapperAndClientConfig(t *testing.T)
func TestAggregatingTransportLeavesSuccessfulAllNamespacesRequestAlone(t *testing.T)
func TestAggregatingTransportLeavesRootScopedResourceAlone(t *testing.T)
func TestAggregatingTransportDoesNotFallbackForNonForbiddenErrors(t *testing.T)
```

Use `meta.NewDefaultRESTMapper` with:

```go
mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, meta.RESTScopeNamespace)
mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)
```

The successful Pod `-A` test must observe exactly one
`/api/v1/pods` request and zero resolver calls. The Node test must remain
direct even if the base transport returns `403`.

- [ ] **Step 2: Run direct-routing tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestAggregating(Getter|TransportLeaves|TransportDoesNot)' -count=1
```

Expected: compilation fails because the getter and transport do not exist.

- [ ] **Step 3: Implement the delegating RESTClientGetter**

The wrapper delegates three methods exactly:

```go
func (g *aggregatingRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error)
func (g *aggregatingRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error)
func (g *aggregatingRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig
```

In `ToRESTConfig`:

1. Copy the delegate config.
2. Call `rest.TransportFor(config)` once to materialize its TLS/auth transport.
3. Parse `config.Host` and save its normalized URL path.
4. Assign the aggregate RoundTripper to `config.Transport`.
5. Clear `config.WrapTransport` and `config.TLSClientConfig` so client-go does
   not reject a custom transport combined with TLS fields.

- [ ] **Step 4: Implement REST scope classification and direct routing**

Cache mapping decisions by GVR in `sync.Map`. Resolve:

```go
kind, err := mapper.KindFor(endpoint.GVR)
mapping, err := mapper.RESTMapping(kind.GroupKind(), endpoint.GVR.Version)
namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace
```

Delegate unchanged when:

- aggregation mode is disabled;
- method is not GET;
- the path is discovery, named, already namespaced, or not a resource;
- mapping scope is root; or
- an on-forbidden direct request does not return `403`.

Always close a discarded `403` body before fallback.

- [ ] **Step 5: Write failing fallback and Workspace tests**

Add:

```go
func TestAggregatingTransportFallsBackAfterNamespacedForbidden(t *testing.T)
func TestAggregatingTransportWorkspaceSkipsGlobalRequest(t *testing.T)
func TestAggregatingTransportRejectsWorkspaceRootResource(t *testing.T)
```

For fallback, the base transport returns:

- `403` for `/api/v1/pods`;
- a List containing `pod-b` for `/api/v1/namespaces/team-b/pods`; and
- a List containing `pod-a` for `/api/v1/namespaces/team-a/pods`.

The resolver returns `[]string{"team-b", "team-a"}`. Assert the final body
contains both items in that order and `state.used.Load()` is true.

For Workspace mode, assert `/api/v1/pods` is never requested and the resolver
receives `"platform"`.

- [ ] **Step 6: Run fallback tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestAggregatingTransport(FallsBack|Workspace)' -count=1
```

Expected: tests fail because fan-out is not implemented.

- [ ] **Step 7: Implement minimal complete fan-out**

Resolve Namespace names, clone the original request with
`request.Clone(ctx)`, clone its URL, replace only `URL.Path`, and invoke the
materialized base transport. Read and close every response body. Treat any
non-2xx result as:

```go
fmt.Errorf("get %s in namespace %q: server returned %s", endpoint.GVR.Resource, namespace, response.Status)
```

Pass scoped bodies to `mergeDocuments`, then create one synthetic response:

```go
&http.Response{
	StatusCode:    http.StatusOK,
	Status:        "200 OK",
	Header:        http.Header{"Content-Type": []string{"application/json"}},
	Body:          io.NopCloser(bytes.NewReader(body)),
	ContentLength: int64(len(body)),
	Request:       original,
}
```

Mark `state.used` before resolving Namespaces so all fallback errors activate
transactional output.

- [ ] **Step 8: Verify basic transport tests**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestAggregating(Getter|Transport)' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit the basic transport**

```bash
git add pkg/cmd/tenant/aggregate_transport.go pkg/cmd/tenant/aggregate_transport_test.go
git diff --cached --check
git commit -m "aggregate forbidden tenant resource lists"
```

---

### Task 5: Add Independent Pagination, Bounded Concurrency, and Cancellation

**Files:**
- Modify: `pkg/cmd/tenant/aggregate_transport.go`
- Modify: `pkg/cmd/tenant/aggregate_transport_test.go`

**Interfaces:**
- Produces:

```go
const maxNamespaceConcurrency = 8

func (t *aggregatingRoundTripper) fetchNamespace(
	ctx context.Context,
	request *http.Request,
	endpoint resourceEndpoint,
	namespace string,
) ([]byte, error)

func continueToken(body []byte) (string, error)
```

- [ ] **Step 1: Write failing independent-pagination test**

Create two Namespaces whose first pages return different literal continue
tokens:

```text
team-a -> continue=a-next
team-b -> continue=b-next
```

Assert the second request for each Namespace receives only its own token, all
four objects appear in Namespace order, and the final aggregate continue token
is empty.

- [ ] **Step 2: Write failing concurrency and deterministic-order tests**

Add:

```go
func TestAggregatingTransportLimitsNamespaceConcurrency(t *testing.T)
func TestAggregatingTransportPreservesNamespaceOrderAcrossCompletionOrder(t *testing.T)
```

Use atomics in the fake transport to record current and maximum in-flight
requests. Return ten Namespaces, block requests on a channel, and assert the
maximum is exactly eight or fewer and greater than one. Release responses in
reverse Namespace order and assert merged items remain in resolver order.

- [ ] **Step 3: Write failing error and cancellation tests**

Add literal subtests for `403`, `404`, `429`, `500`, malformed JSON, and a
failing second page. For each, assert:

- returned error includes the Namespace;
- `state.used` is true;
- every response body reports `Close` called; and
- a blocked peer observes context cancellation.

- [ ] **Step 4: Run transport robustness tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run 'Pagination|Concurrency|CompletionOrder|Cancellation|NamespaceError' -count=1
```

Expected: pagination and concurrency assertions fail against the sequential
single-page implementation.

- [ ] **Step 5: Implement page consumption**

For each Namespace:

1. Clone the original URL query.
2. Send one request.
3. Decode and retain the page document.
4. Extract `metadata.continue`.
5. Stop on empty token; otherwise set only that request's `continue`.
6. Merge that Namespace's pages with `mergeDocuments`.

Do not pass one Namespace's token to another. Wrap decode errors with the
Namespace and page number.

- [ ] **Step 6: Implement bounded errgroup fan-out**

Use:

```go
group, groupContext := errgroup.WithContext(ctx)
group.SetLimit(maxNamespaceConcurrency)
documents := make([]scopedDocument, len(namespaces))
for index, namespace := range namespaces {
	index, namespace := index, namespace
	group.Go(func() error {
		body, err := t.fetchNamespace(groupContext, request, endpoint, namespace)
		if err != nil {
			return err
		}
		documents[index] = scopedDocument{Namespace: namespace, Body: body}
		return nil
	})
}
if err := group.Wait(); err != nil {
	return nil, err
}
```

Every request must use `groupContext`. Read a response body completely and
close it on every status path. Preserve only the original query parameters
plus the current Namespace's continue token.

- [ ] **Step 7: Verify focused and race tests**

Run:

```bash
go test ./pkg/cmd/tenant -run 'AggregatingTransport|Pagination|Concurrency|CompletionOrder|Cancellation' -count=1
go test -race ./pkg/cmd/tenant -run 'AggregatingTransport|Pagination|Concurrency|CompletionOrder|Cancellation' -count=1
```

Expected: PASS with no race reports.

- [ ] **Step 8: Commit transport robustness**

```bash
git add pkg/cmd/tenant/aggregate_transport.go pkg/cmd/tenant/aggregate_transport_test.go
git diff --cached --check
git commit -m "bound tenant namespace aggregation"
```

---

### Task 6: Integrate Workspace Validation, Atomic Output, and Watch Behavior

**Files:**
- Modify: `pkg/cmd/tenant/generic_get.go`
- Modify: `pkg/cmd/tenant/command.go`
- Modify: `pkg/cmd/tenant/command_test.go`
- Modify: `pkg/cmd/root.go`
- Modify: `pkg/cmd/root_test.go`

**Interfaces:**
- Consumes: `newNamespaceResolver`, `newAggregatingRESTClientGetter`,
  `aggregationState`.
- Produces:

```go
type conditionalWriter struct {
	delegate io.Writer
	state    *aggregationState
	buffer   bytes.Buffer
}

func (w *conditionalWriter) Write(p []byte) (int, error)
func (w *conditionalWriter) Commit() error
```

- [ ] **Step 1: Write failing flag-validation tests**

Add table-driven cases:

```go
tests := []struct {
	args []string
	want string
}{
	{[]string{"get", "pods", "--workspace", "platform", "-n", "demo"}, "--workspace and --namespace"},
	{[]string{"get", "pods", "--workspace", "platform", "-A"}, "--workspace and --all-namespaces"},
	{[]string{"get", "pods", "--workspace", "platform", "-w"}, "watch is not supported"},
	{[]string{"get", "--raw", "/api/v1/pods", "--workspace", "platform"}, "--workspace and --raw"},
	{[]string{"get", "-f", "pod.yaml", "--workspace", "platform"}, "--workspace and --filename"},
	{[]string{"get", "pod/web", "--workspace", "platform"}, "collection queries"},
	{[]string{"get", "nodes", "--workspace", "platform"}, "cluster-scoped"},
}
```

Use a fake mapper for Pod/Node and assert no resource request occurs for each
invalid case.

- [ ] **Step 2: Write failing routing and output tests through the command**

Use a single `httptest.Server` providing discovery, tenant Namespace, and
Kubernetes paths. Add:

```go
func TestCommandAdministratorAllNamespacesUsesNativeKubectl(t *testing.T)
func TestCommandTenantAllNamespacesFallsBackAndPrintsOneTable(t *testing.T)
func TestCommandWorkspacePrintsJSONFromOnlyWorkspaceNamespaces(t *testing.T)
func TestCommandAggregateFailurePrintsNoPartialOutput(t *testing.T)
func TestCommandTenantWatchDiscardsBufferedInitialList(t *testing.T)
func TestCommandAdministratorWatchRemainsNative(t *testing.T)
```

The administrator test asserts no `/kapis/tenant.../namespaces` request. The
tenant table test returns server Tables without row objects and asserts:

```text
NAMESPACE   NAME
team-b      pod-b
team-a      pod-a
```

The JSON test decodes stdout once and asserts it is one valid PodList. The
failure and tenant-watch tests assert stdout is empty.

- [ ] **Step 3: Run integration tests and verify RED**

Run:

```bash
go test ./pkg/cmd/tenant -run '^TestCommand(Administrator|TenantAll|WorkspacePrints|AggregateFailure|TenantWatch)' -count=1
```

Expected: validation and fallback tests fail because the command still uses
the direct getter.

- [ ] **Step 4: Implement the conditional writer**

`Write` delegates immediately while `state.used` is false. Once fallback or
Workspace aggregation marks the state used, subsequent writes go to the
buffer. `Commit` copies the complete buffer to the delegate and wraps failure:

```go
func (w *conditionalWriter) Commit() error {
	if !w.state.used.Load() {
		return nil
	}
	if _, err := io.Copy(w.delegate, bytes.NewReader(w.buffer.Bytes())); err != nil {
		return fmt.Errorf("write tenant get output: %w", err)
	}
	return nil
}
```

Use separate conditional writers for stdout and stderr. On an aggregate
execution error, return without committing either buffer. Direct kubectl
requests keep writing to their delegates.

- [ ] **Step 5: Activate scope before GetOptions completion**

In the generic command `RunE`:

```go
workspace, _ := command.Flags().GetString("workspace")
if err := validateGenericScope(command, options, args, workspace); err != nil {
	return err
}
state.mode = aggregateDisabled
state.workspace = ""
switch {
case workspace != "":
	state.mode = aggregateWorkspace
	state.workspace = workspace
	state.used.Store(true)
	options.AllNamespaces = true
case options.AllNamespaces:
	state.mode = aggregateOnForbidden
}
```

Then call `Complete`, `Validate`, and `Run`. Workspace mapping errors are
returned by the aggregate transport before a resource request. If `Run`
succeeds, commit stdout then stderr. If it fails and `state.used` is false,
direct output has already retained kubectl semantics; if used is true, discard
the buffers.

Reject explicit Namespace with `command.Flags().Changed("namespace")`; do not
reject a context default Namespace because Workspace mode deliberately
overrides it by setting `AllNamespaces`.

- [ ] **Step 6: Inject the aggregate getter in production command options**

Inside `NewCommandWithOptions`:

```go
state := &aggregationState{}
resolver := newNamespaceResolver(options.KubeSphereGetter)
getter := newAggregatingRESTClientGetter(options.KubernetesGetter, resolver, state)
factory := cmdutil.NewFactory(cmdutil.NewMatchVersionFlags(getter))
```

Pass conditional streams to `getcmd.NewGetOptions`. Native children continue
to use the original KubeSphere getter and command writers.

- [ ] **Step 7: Implement watch rejection at the transport boundary**

If a forbidden eligible request contains `watch=true`, set `state.used` and
return:

```go
fmt.Errorf("tenant multi-namespace watch is not supported; choose one namespace with --namespace")
```

Do not resolve Namespaces or fan out. Workspace watch remains an early command
validation error. An administrator's successful native watch never sets
`state.used`, so output streams continuously as kubectl normally does.

- [ ] **Step 8: Verify command, root, and race tests**

Run:

```bash
go test ./pkg/cmd/tenant ./pkg/cmd -count=1
go test -race ./pkg/cmd/tenant ./pkg/cmd -count=1
```

Expected: PASS. Confirm existing native tenant paths and top-level/kube get
tests remain green.

- [ ] **Step 9: Commit command integration**

```bash
git add pkg/cmd/tenant/generic_get.go pkg/cmd/tenant/command.go pkg/cmd/tenant/command_test.go pkg/cmd/root.go pkg/cmd/root_test.go
git diff --cached --check
git commit -m "integrate tenant scoped resource get"
```

---

### Task 7: Cover CRDs, Selectors, Output Formats, and Connection Routing

**Files:**
- Modify: `pkg/cmd/tenant/command_test.go`
- Modify: `pkg/cmd/root_test.go`
- Modify: `pkg/cmd/tenant/aggregate_transport_test.go`

**Interfaces:**
- Consumes: the complete command and transport implementation.
- Produces: regression coverage for arbitrary discovery and kubectl-compatible
  behavior.

- [ ] **Step 1: Add failing CRD and qualified-resource integration tests**

Serve discovery for `example.io/v1` with namespaced `widgets`, return a CRD
Table, and execute:

```text
tenant get widgets.example.io -A
tenant get widgets.v1.example.io --workspace platform -o yaml
```

Assert discovery uses the selected Cluster prefix, fallback paths are:

```text
/clusters/member/apis/example.io/v1/namespaces/team-a/widgets
```

and YAML is one valid `WidgetList`.

- [ ] **Step 2: Add selector, sorting, and output table tests**

Execute literal cases for:

```text
-l app=web
--field-selector status.phase=Running
--sort-by=.metadata.name
-o wide
-o custom-columns=NAME:.metadata.name,NS:.metadata.namespace
-o json
-o yaml
```

Assert selectors reach every Namespace request unchanged; sorting operates
across the merged object set; and each structured output decodes exactly once.

- [ ] **Step 3: Add explicit and context-default Cluster routing tests**

Reuse config fixtures from `root_test.go`. Assert both tenant Namespace and
Kubernetes requests include `/clusters/{cluster}`, with an explicit root
`--cluster` overriding the Context default.

- [ ] **Step 4: Run new tests and verify any missing behavior fails**

Run:

```bash
go test ./pkg/cmd/tenant ./pkg/cmd -run 'CRD|Qualified|Selector|Sort|Output|Tenant.*Cluster' -count=1
```

Expected: any incomplete query preservation or Cluster path handling fails
with a literal path/output mismatch.

- [ ] **Step 5: Make only the minimal corrections exposed by tests**

Corrections are limited to:

- preserving original URL query values while replacing only `continue`;
- using the REST config host path prefix in aggregate URLs;
- keeping Table row metadata for Namespace decoration; and
- ensuring Workspace mode sets kubectl's all-Namespaces printing option.

Do not special-case individual built-in or CRD resource names.

- [ ] **Step 6: Run focused, full package, and race tests**

Run:

```bash
go test ./pkg/cmd/tenant ./pkg/cmd -count=1
go test -race ./pkg/cmd/tenant ./pkg/cmd -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit compatibility coverage and corrections**

```bash
git add pkg/cmd/tenant/command_test.go pkg/cmd/tenant/aggregate_transport_test.go pkg/cmd/root_test.go pkg/cmd/tenant/*.go
git diff --cached --check
git commit -m "cover tenant get kubectl compatibility"
```

---

### Task 8: Document Tenant Arbitrary-Resource Queries

**Files:**
- Modify: `README.md`
- Modify: `README_zh.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/cli.md`
- Modify: `docs/cli_zh.md`
- Modify: `docs/design.md`
- Modify: `docs/design_zh.md`

**Interfaces:**
- Consumes: final CLI syntax and verified behavior.
- Produces: user and maintainer documentation in English and Chinese.

- [ ] **Step 1: Update CLI guides with exact behavior**

Add these examples in both languages:

```text
ksctl tenant get pods
ksctl tenant get pods -A
ksctl tenant get deployments --workspace platform
ksctl tenant get widgets.example.io --workspace platform -o yaml
```

State:

- native Workspace/Namespace/Cluster commands remain;
- administrator `-A` stays direct;
- a forbidden tenant `-A` resolves accessible Namespaces and aggregates;
- `--workspace` is mutually exclusive with `-n` and `-A`;
- every Namespace must succeed;
- tenant aggregate watch is unsupported; and
- cluster-scoped resources cannot use Workspace scope.

- [ ] **Step 2: Update architecture guides**

Describe the boundary:

```text
tenant generic get
  -> kubectl GetOptions / discovery / RESTMapper
  -> aggregation-aware transport
     -> direct success, or
     -> tenant Namespace resolution + bounded fan-out
  -> merged List/Table
  -> kubectl printer
```

Explain that only the tenant command receives this transport and that
`staging/` remains untouched.

- [ ] **Step 3: Update README summaries and changelog**

Keep README content concise: one capability bullet and one representative
command. Add a changelog entry phrased around user impact:

```text
- Allow tenant users to get arbitrary Kubernetes resources across accessible
  Namespaces, including Workspace-scoped queries, while preserving native
  kubectl behavior for Cluster administrators.
```

- [ ] **Step 4: Check command names and bilingual coverage**

Run:

```bash
rg -n 'tenant get|--workspace|all-namespaces|watch' README.md README_zh.md docs/cli.md docs/cli_zh.md docs/design.md docs/design_zh.md CHANGELOG.md
rg -n 'xx\\|xxx|namespace-a\\|namespace-b' README.md README_zh.md docs/cli.md docs/cli_zh.md
```

Expected: both languages document the same supported forms; no document
recommends a pipe-separated Namespace value.

- [ ] **Step 5: Commit documentation**

```bash
git add README.md README_zh.md CHANGELOG.md docs/cli.md docs/cli_zh.md docs/design.md docs/design_zh.md
git diff --cached --check
git commit -m "document tenant arbitrary resource queries"
```

---

### Task 9: Verify, Review, and Prepare the Branch

**Files:**
- Review: every file changed by Tasks 1-8.

**Interfaces:**
- Consumes: the complete feature branch.
- Produces: evidence that the branch is formatted, tidy, race-safe, buildable,
  scoped, and ready for integration.

- [ ] **Step 1: Format only changed Go files**

Run `gofmt -w` with the explicit changed Go file list:

```bash
gofmt -w pkg/cmd/tenant/command.go pkg/cmd/tenant/generic_get.go pkg/cmd/tenant/scope.go pkg/cmd/tenant/aggregate_request.go pkg/cmd/tenant/aggregate_response.go pkg/cmd/tenant/aggregate_transport.go pkg/cmd/tenant/command_test.go pkg/cmd/tenant/scope_test.go pkg/cmd/tenant/aggregate_request_test.go pkg/cmd/tenant/aggregate_response_test.go pkg/cmd/tenant/aggregate_transport_test.go pkg/cmd/root.go pkg/cmd/root_test.go
```

- [ ] **Step 2: Run focused tests without cache**

```bash
go test ./pkg/cmd/tenant ./pkg/cmd -count=1
go test -race ./pkg/cmd/tenant ./pkg/cmd -count=1
```

Expected: PASS.

- [ ] **Step 3: Run repository verification**

```bash
make verify
```

Expected: formatting, module consistency, vet, normal tests, race tests, and
build all pass.

- [ ] **Step 4: Smoke-test the built CLI**

```bash
./bin/ksctl tenant get --help
./bin/ksctl tenant get workspace --help
./bin/ksctl version
```

Expected:

- generic help shows kubectl get flags plus `--workspace`;
- native Workspace help remains available; and
- version exits successfully with the built version information.

- [ ] **Step 5: Run scope and hygiene checks**

```bash
git diff --check
git status --short
git diff --stat main...HEAD
git diff --name-only main...HEAD -- staging
```

Expected: no whitespace errors, only intended source/docs changes, and no
output for the `staging` check.

- [ ] **Step 6: Perform code review**

Invoke `code-review-and-quality` and inspect:

- authorization is never broadened;
- fallback activates only on eligible namespaced collection `403`;
- every body is closed;
- context cancellation reaches all workers;
- shared state is race-safe;
- output remains deterministic and atomic for aggregation failures;
- errors contain Namespace/resource context but no credentials;
- native tenant, top-level get, and kube get behavior remain unchanged; and
- no unnecessary abstraction or copied kubectl engine was introduced.

Apply any actionable findings with a new failing regression test before the
fix, then rerun Steps 1-5.

- [ ] **Step 7: Commit review corrections if any**

If review produced changes:

```bash
git add pkg/cmd/tenant pkg/cmd/root.go pkg/cmd/root_test.go README.md README_zh.md CHANGELOG.md docs
git diff --cached --check
git commit -m "harden tenant resource aggregation"
```

If review produced no changes, do not create an empty commit.
