# Extension CLI UX Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make extension installation and human-readable extension output safer and easier to use while preserving exact server contracts.

**Architecture:** Keep Cobra-only validation and formatting in `pkg/cmd/extension`, while server-derived version and Cluster decisions stay in `internal/extension.Service`. Extend the existing REST abstraction with a minimal Cluster list model, then reuse the current scheduling and accepted-response validation paths.

**Tech Stack:** Go 1.26+, Cobra, KubeSphere REST client, Kubernetes condition conventions, Go `testing`.

## Global Constraints

- `extension install NAME` uses `Extension.status.recommendedVersion` only when `--version` is omitted; it never guesses the newest version.
- An explicit version remains exact and opaque.
- `--all-clusters` is explicit, is mutually exclusive with `--clusters`, and does not change the host-only default.
- All-cluster selection includes host Clusters and filters only deleting, not-ready, or explicitly unschedulable Clusters.
- All-cluster placement writes a lexically sorted explicit `placement.clusters` snapshot.
- `extension list` removes `TARGET` from both table modes without changing JSON or YAML.
- Default `extension show` is concise; `-o wide` preserves detailed human-readable fields.
- `extension show` renders `clusterSchedulingStatuses` sorted by Cluster name.
- Default diagnosis is problem-focused; `--verbose` renders all checks.
- Do not add terminal color or symbols, new dependencies, or edits under `staging/`.
- Preserve terminal escaping, writer-error propagation, structured-output completeness, and existing exit-code semantics.

---

### Task 1: Optional Install Version Resolution

**Files:**
- Modify: `internal/extension/lifecycle.go:36-104`
- Modify: `internal/extension/lifecycle_test.go:28-158`
- Modify: `pkg/cmd/extension/mutation.go:128-182`
- Modify: `pkg/cmd/extension/mutation_test.go:90-230`

**Interfaces:**
- Consumes: `Extension.status.recommendedVersion` through the existing `GetExtension`.
- Produces: `Service.Install(ctx, name, InstallOptions{Version: ""})` resolving an exact non-empty version before ExtensionVersion lookup.
- Produces: the actual resolved value in `InstallPlan.spec.extension.version` and `Operation.TargetVersion`.

- [ ] **Step 1: Write failing service tests for recommendation resolution**

Add:

```go
func TestServiceInstallUsesRecommendedVersionWhenVersionIsOmitted(t *testing.T) {
	client := newFakeAPIClient(t)
	version := lifecycleVersion("demo", "1.2.1", "HostOnly")
	prepareExtensionForLifecycle(t, client, "demo", version)
	extension := extensionForTest("demo")
	extension.Status.RecommendedVersion = "1.2.1"
	client.extensionObjects["demo"] = objectForTest(t, extension)

	operation, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{},
	)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := client.createdPlans[0].Spec.Extension.Version; got != "1.2.1" {
		t.Fatalf("created version = %q, want 1.2.1", got)
	}
	if operation.TargetVersion != "1.2.1" {
		t.Fatalf("operation target = %q, want 1.2.1", operation.TargetVersion)
	}
}

func TestServiceInstallRejectsMissingRecommendedVersionWithoutWrite(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensionObjects["demo"] = objectForTest(t, extensionForTest("demo"))

	_, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "extension versions demo") {
		t.Fatalf("Install() error = %v, want versions hint", err)
	}
	if len(client.createdPlans) != 0 {
		t.Fatalf("created plans = %#v", client.createdPlans)
	}
	for _, call := range client.calls {
		if strings.HasPrefix(call, "get extension version ") {
			t.Fatalf("unexpected version lookup: %q", call)
		}
	}
}
```

Extend the success test so an explicit `Version: "1.2.1"` still wins even
when the Extension recommends a different version.

- [ ] **Step 2: Run the focused service tests and verify RED**

Run:

```bash
go test ./internal/extension -run 'TestServiceInstall(UsesRecommendedVersionWhenVersionIsOmitted|RejectsMissingRecommendedVersionWithoutWrite)' -count=1
```

Expected: FAIL because `Install` still returns `exact extension version is required`.

- [ ] **Step 3: Implement minimal service-side resolution**

In `Service.Install`, replace the early empty-version rejection and duplicate
Extension read with:

```go
extension, err := s.client.GetExtension(ctx, name)
if err != nil {
	return Operation{}, fmt.Errorf("get extension %q: %w", name, err)
}
selectedVersion := options.Version
if strings.TrimSpace(selectedVersion) == "" {
	selectedVersion = extension.Value.Status.RecommendedVersion
	if strings.TrimSpace(selectedVersion) == "" {
		return Operation{}, fmt.Errorf(
			"extension %q has no recommended version; run extension versions %s",
			name,
			name,
		)
	}
}
options.Version = selectedVersion
version, err := s.exactVersion(ctx, name, options.Version)
if err != nil {
	return Operation{}, err
}
```

Keep all later code using `options.Version`, so the resolved value flows
through the plan, operation, accepted response, and waiter.

- [ ] **Step 4: Run the service tests and verify GREEN**

Run:

```bash
go test ./internal/extension -run 'TestServiceInstall' -count=1
```

Expected: PASS.

- [ ] **Step 5: Write a failing command test for omitted `--version`**

Add:

```go
func TestInstallPassesEmptyVersionForServiceResolution(t *testing.T) {
	streams, _, _ := bufferedStreams()
	var got internalextension.InstallOptions
	service := &fakeService{
		installFn: func(
			_ context.Context,
			_ string,
			options internalextension.InstallOptions,
		) (internalextension.Operation, error) {
			got = options
			return internalextension.Operation{
				Kind:          internalextension.OperationInstall,
				Name:          "demo",
				TargetVersion: "1.2.1",
			}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "install", "demo"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Version != "" {
		t.Fatalf("Version = %q, want service-resolved empty value", got.Version)
	}
}
```

- [ ] **Step 6: Run the command test and verify RED**

Run:

```bash
go test ./pkg/cmd/extension -run TestInstallPassesEmptyVersionForServiceResolution -count=1
```

Expected: FAIL with the current `--version requires a non-empty exact version`.

- [ ] **Step 7: Make `--version` optional in the command**

Remove the unconditional empty-version error from `newInstallCommand`. Keep
the flag and pass its value unchanged. Update command copy to:

```go
Use:     "install NAME",
Short:   "Install a KubeSphere extension",
Example: parent + " extension install NAME [--version VERSION]",
```

Change the flag help to:

```go
"Exact extension version; defaults to status.recommendedVersion"
```

- [ ] **Step 8: Run focused and package tests**

Run:

```bash
go test ./pkg/cmd/extension ./internal/extension -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit Task 1**

```bash
git add internal/extension/lifecycle.go internal/extension/lifecycle_test.go pkg/cmd/extension/mutation.go pkg/cmd/extension/mutation_test.go
git commit -m "allow recommended extension install version"
```

---

### Task 2: Explicit All-Cluster Placement

**Files:**
- Modify: `internal/extension/types.go:1-155`
- Modify: `internal/extension/rest_client.go:14-78`
- Modify: `internal/extension/rest_client_test.go:27-98`
- Modify: `internal/extension/fake_client_test.go:11-245`
- Modify: `internal/extension/lifecycle.go:36-120`
- Modify: `internal/extension/lifecycle_test.go:55-158`
- Modify: `pkg/cmd/extension/mutation.go:72-182`
- Modify: `pkg/cmd/extension/mutation_test.go:141-230`

**Interfaces:**
- Produces: `APIClient.ListClusters(context.Context) (List[Cluster], error)`.
- Produces: `InstallOptions.AllClusters bool`.
- Consumes: Cluster `metadata.name`, `metadata.deletionTimestamp`, and status conditions `KSCoreReady` / `Schedulable`.
- Produces: sorted eligible names passed into existing `BuildInstallScheduling`.

- [ ] **Step 1: Write failing REST and service tests**

Add a REST path/decoding test:

```go
func TestRESTClientListsFleetClusters(t *testing.T) {
	var gotPath string
	client := newTestAPIClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		gotPath = request.URL.Path
		writeJSONResponse(t, response,
			`{"apiVersion":"cluster.kubesphere.io/v1alpha1","kind":"ClusterList","items":[{"metadata":{"name":"host"},"status":{"conditions":[{"type":"KSCoreReady","status":"True"}]}}]}`,
		)
	}), nil)

	clusters, err := client.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if gotPath != "/apis/cluster.kubesphere.io/v1alpha1/clusters" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(clusters.Items) != 1 ||
		clusters.Items[0].Value.Metadata.Name != "host" {
		t.Fatalf("clusters = %#v", clusters.Items)
	}
}
```

Add a service test with Clusters in deliberately unsorted order:

```go
func clusterForTest(
	name string,
	deletionTimestamp *metav1.Time,
	conditions ...ClusterCondition,
) Cluster {
	return Cluster{
		Metadata: ObjectMeta{
			Name:              name,
			DeletionTimestamp: deletionTimestamp,
		},
		Status: ClusterStatus{Conditions: conditions},
	}
}

func TestServiceInstallAllClustersUsesEligibleSortedSnapshotIncludingHost(
	t *testing.T,
) {
	client := newFakeAPIClient(t)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "Multicluster"),
	)
	client.clusters = listForTest(
		t,
		"cluster.kubesphere.io/v1alpha1",
		"ClusterList",
		clusterForTest("member-z", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"}),
		clusterForTest("host", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"}),
		clusterForTest("not-ready", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "False"}),
		clusterForTest("unschedulable", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"},
			ClusterCondition{Type: "Schedulable", Status: "False"}),
		clusterForTest("member-a", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"}),
	)

	_, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{Version: "1.2.1", AllClusters: true},
	)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	got := client.createdPlans[0].Spec.ClusterScheduling.Placement.Clusters
	want := []string{"host", "member-a", "member-z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clusters = %#v, want %#v", got, want)
	}
}
```

Add explicit preflight cases:

```go
func TestServiceInstallAllClustersRejectsInvalidSelectionWithoutCreate(
	t *testing.T,
) {
	now := metav1.Now()
	tests := []struct {
		name       string
		mode       string
		clusters   []Cluster
		overrides  map[string]string
		want       string
	}{
		{
			name: "HostOnly",
			mode: "HostOnly",
			clusters: []Cluster{clusterForTest(
				"host",
				nil,
				ClusterCondition{Type: "KSCoreReady", Status: "True"},
			)},
			want: "--all-clusters",
		},
		{
			name: "no eligible Clusters",
			mode: "Multicluster",
			clusters: []Cluster{
				clusterForTest(
					"deleting",
					&now,
					ClusterCondition{Type: "KSCoreReady", Status: "True"},
				),
				clusterForTest(
					"not-ready",
					nil,
					ClusterCondition{Type: "KSCoreReady", Status: "False"},
				),
			},
			want: "no ready, schedulable Clusters",
		},
		{
			name: "override outside selection",
			mode: "Multicluster",
			clusters: []Cluster{clusterForTest(
				"member-a",
				nil,
				ClusterCondition{Type: "KSCoreReady", Status: "True"},
			)},
			overrides: map[string]string{"member-b": "key: value\n"},
			want: "member-b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			prepareExtensionForLifecycle(
				t,
				client,
				"demo",
				lifecycleVersion("demo", "1.2.1", test.mode),
			)
			client.clusters = listForTest(
				t,
				"cluster.kubesphere.io/v1alpha1",
				"ClusterList",
				test.clusters...,
			)
			_, err := NewService(client).Install(
				context.Background(),
				"demo",
				InstallOptions{
					Version:     "1.2.1",
					AllClusters: true,
					Overrides:   test.overrides,
				},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if len(client.createdPlans) != 0 {
				t.Fatalf("created plans = %#v", client.createdPlans)
			}
		})
	}
}
```

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./internal/extension -run 'Test(RESTClientListsFleetClusters|ServiceInstallAllClusters)' -count=1
```

Expected: compile failure because Cluster, `ListClusters`, and `AllClusters`
do not exist.

- [ ] **Step 3: Add the minimal Cluster model and client method**

Add:

```go
type ClusterCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type ClusterStatus struct {
	Conditions []ClusterCondition `json:"conditions,omitempty"`
}

type Cluster struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Metadata   ObjectMeta    `json:"metadata"`
	Status     ClusterStatus `json:"status,omitempty"`
}
```

Extend `APIClient`:

```go
ListClusters(context.Context) (List[Cluster], error)
```

Implement:

```go
const clusterAPIPath = "/apis/cluster.kubesphere.io/v1alpha1"

func (c *restClient) ListClusters(ctx context.Context) (List[Cluster], error) {
	raw, err := resultRaw(c.client.Get().
		AbsPath(clusterAPIPath, "clusters").
		Do(ctx))
	if err != nil {
		return List[Cluster]{}, fmt.Errorf("list Fleet clusters: %w", err)
	}
	clusters, err := decodeList[Cluster](raw)
	if err != nil {
		return List[Cluster]{}, fmt.Errorf("decode Fleet clusters: %w", err)
	}
	return clusters, nil
}
```

Extend `fakeAPIClient` with `clusters List[Cluster]`, `clusterErr error`, and:

```go
func (f *fakeAPIClient) ListClusters(context.Context) (List[Cluster], error) {
	f.calls = append(f.calls, "list clusters")
	if f.clusterErr != nil {
		return List[Cluster]{}, f.clusterErr
	}
	return f.clusters, nil
}
```

- [ ] **Step 4: Implement all-cluster selection in the service**

Extend:

```go
type InstallOptions struct {
	Version     string
	Config      *string
	Clusters    []string
	AllClusters bool
	Overrides   map[string]string
}
```

Add focused helpers:

```go
func clusterCondition(
	cluster Cluster,
	conditionType string,
) (string, bool) {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status, true
		}
	}
	return "", false
}

func eligibleClusterNames(clusters List[Cluster]) []string {
	names := make([]string, 0, len(clusters.Items))
	for _, item := range clusters.Items {
		cluster := item.Value
		if cluster.Metadata.DeletionTimestamp != nil {
			continue
		}
		ready, found := clusterCondition(cluster, "KSCoreReady")
		if !found || ready != "True" {
			continue
		}
		if schedulable, found := clusterCondition(
			cluster,
			"Schedulable",
		); found && schedulable == "False" {
			continue
		}
		names = append(names, cluster.Metadata.Name)
	}
	slices.Sort(names)
	return names
}
```

After exact-version lookup and before normal scheduling construction:

```go
if options.AllClusters {
	if version.Value.Spec.InstallationMode != "Multicluster" {
		return Operation{}, fmt.Errorf(
			"extension version %q uses installation mode %q and does not accept --all-clusters",
			options.Version,
			valueOrNone(version.Value.Spec.InstallationMode),
		)
	}
	clusters, err := s.client.ListClusters(ctx)
	if err != nil {
		return Operation{}, err
	}
	options.Clusters = eligibleClusterNames(clusters)
	if len(options.Clusters) == 0 {
		return Operation{}, fmt.Errorf(
			"no ready, schedulable Clusters are available for --all-clusters",
		)
	}
}
```

Pass `options.Clusters` into the existing scheduling builder.

- [ ] **Step 5: Run internal tests and verify GREEN**

Run:

```bash
go test ./internal/extension -count=1
```

Expected: PASS.

- [ ] **Step 6: Write failing command tests for the new flag**

Add:

```go
func TestInstallPassesAllClustersToService(t *testing.T) {
	streams, _, _ := bufferedStreams()
	var got internalextension.InstallOptions
	service := &fakeService{installFn: func(
		_ context.Context,
		_ string,
		options internalextension.InstallOptions,
	) (internalextension.Operation, error) {
		got = options
		return internalextension.Operation{
			Kind:          internalextension.OperationInstall,
			Name:          "demo",
			TargetVersion: "1.2.1",
		}, nil
	}}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension", "install", "demo",
			"--version", "1.2.1",
			"--all-clusters",
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !got.AllClusters {
		t.Fatalf("AllClusters = false, want true")
	}
}

func TestInstallRejectsAllClustersWithExplicitClustersBeforeFactory(
	t *testing.T,
) {
	streams, out, _ := bufferedStreams()
	called := false
	err := executeExtensionCommand(
		t,
		[]string{
			"extension", "install", "demo",
			"--version", "1.2.1",
			"--clusters", "member-a",
			"--all-clusters",
		},
		streams,
		func() (Service, error) {
			called = true
			return &fakeService{}, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("Execute() error = %v", err)
	}
	if called {
		t.Fatal("service factory was called")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
}
```

- [ ] **Step 7: Run command tests and verify RED**

Run:

```bash
go test ./pkg/cmd/extension -run 'TestInstall.*AllClusters' -count=1
```

Expected: FAIL because the flag is unknown.

- [ ] **Step 8: Add the command flag and mutual exclusion**

In `newInstallCommand`:

```go
var allClusters bool
```

Pass:

```go
AllClusters: allClusters,
```

Register:

```go
command.Flags().BoolVar(
	&allClusters,
	"all-clusters",
	false,
	"Install the extension agent on every ready, schedulable Fleet Cluster",
)
command.MarkFlagsMutuallyExclusive("clusters", "all-clusters")
```

- [ ] **Step 9: Run focused and package tests**

Run:

```bash
go test ./pkg/cmd/extension ./internal/extension -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit Task 2**

```bash
git add internal/extension/types.go internal/extension/rest_client.go internal/extension/rest_client_test.go internal/extension/fake_client_test.go internal/extension/lifecycle.go internal/extension/lifecycle_test.go pkg/cmd/extension/mutation.go pkg/cmd/extension/mutation_test.go
git commit -m "add all-cluster extension installation"
```

---

### Task 3: Concise List and Show Output

**Files:**
- Modify: `pkg/cmd/extension/query.go:13-125`
- Modify: `pkg/cmd/extension/output.go:99-318`
- Modify: `pkg/cmd/extension/output_test.go:47-352`
- Modify: `pkg/cmd/extension/query_test.go:48-190`
- Modify: `pkg/cmd/extension_integration_test.go`

**Interfaces:**
- Produces: `printShow(io.Writer, ShowResult, outputFormat) error`.
- Default show consumes the seven-field concise contract and omits empty
  optional fields.
- Wide show consumes the existing full field set.
- Both modes consume `InstallPlan.status.clusterSchedulingStatuses`.

- [ ] **Step 1: Change output expectations first**

Update list tests to expect:

```text
NAME  CATEGORY  RECOMMENDED  INSTALLED  STATE
```

and wide:

```text
NAME  CATEGORY  RECOMMENDED  INSTALLED  STATE  PROVIDER  ENABLED
```

Add a concise show test:

```go
func TestOutputShowDefaultIsConciseAndOmitsEmptyValues(t *testing.T) {
	result := internalextension.ShowResult{
		Extension: internalextension.Object[internalextension.Extension]{
			Value: internalextension.Extension{
				Metadata: internalextension.ObjectMeta{Name: "demo"},
				Spec: internalextension.ExtensionSpec{
					DisplayName: map[string]string{"en": "Demo"},
				},
				Status: internalextension.ExtensionStatus{
					RecommendedVersion: "1.3.0",
				},
			},
		},
		InstallPlan: &internalextension.Object[internalextension.InstallPlan]{
			Value: internalextension.InstallPlan{
				Status: internalextension.InstallPlanStatus{
					InstallationStatus: internalextension.InstallationStatus{
						State:   "Installed",
						Version: "1.2.1",
					},
				},
			},
		},
	}
	var output bytes.Buffer
	if err := printShow(&output, result, outputTable); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	for _, want := range []string{
		"Name                 demo\n",
		"Display Name         Demo\n",
		"State                Installed\n",
		"Installed Version    1.2.1\n",
		"Recommended Version  1.3.0\n",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
	for _, absent := range []string{"Description", "Category", "Provider"} {
		if strings.Contains(output.String(), absent) {
			t.Fatalf("output = %q, unexpectedly contains %q",
				output.String(), absent)
		}
	}
}
```

Add a wide show test that uses the existing full field-order assertion with
`printShow(&output, result, outputWide)`. Add this scheduling assertion:

```go
func TestOutputShowPrintsSortedClusterSchedulingStatuses(t *testing.T) {
	result := internalextension.ShowResult{
		Extension: internalextension.Object[internalextension.Extension]{
			Value: internalextension.Extension{
				Metadata: internalextension.ObjectMeta{Name: "demo"},
			},
		},
		InstallPlan: &internalextension.Object[internalextension.InstallPlan]{
			Value: internalextension.InstallPlan{
				Status: internalextension.InstallPlanStatus{
					ClusterSchedulingStatuses: map[string]internalextension.InstallationStatus{
						"member-z": {
							Version:         "1.2.1",
							State:           "Installing",
							TargetNamespace: "demo-system",
							JobName:         "job-z",
						},
						"member-a": {
							Version:         "1.2.1",
							State:           "Installed",
							TargetNamespace: "demo-system",
							JobName:         "job-a",
						},
					},
				},
			},
		},
	}
	var compact bytes.Buffer
	if err := printShow(&compact, result, outputTable); err != nil {
		t.Fatalf("printShow(table) error = %v", err)
	}
	if !strings.Contains(
		compact.String(),
		"clusterSchedulingStatuses\n\nCLUSTER   VERSION  STATE\n"+
			"member-a  1.2.1    Installed\n"+
			"member-z  1.2.1    Installing\n",
	) {
		t.Fatalf("compact output = %q", compact.String())
	}

	var wide bytes.Buffer
	if err := printShow(&wide, result, outputWide); err != nil {
		t.Fatalf("printShow(wide) error = %v", err)
	}
	for _, want := range []string{
		"CLUSTER",
		"VERSION",
		"STATE",
		"NAMESPACE",
		"JOB",
		"member-a",
		"job-a",
		"member-z",
		"job-z",
	} {
		if !strings.Contains(wide.String(), want) {
			t.Fatalf("wide output = %q, want %q", wide.String(), want)
		}
	}
	if strings.Index(wide.String(), "member-a") >
		strings.Index(wide.String(), "member-z") {
		t.Fatalf("wide output is not sorted: %q", wide.String())
	}
}
```

- [ ] **Step 2: Run output/query tests and verify RED**

Run:

```bash
go test ./pkg/cmd/extension -run 'Test(OutputList|OutputShow|Show)' -count=1
```

Expected: FAIL on old `TARGET`, old show fields, and unsupported `wide`.

- [ ] **Step 3: Remove `TARGET` from list rendering**

Reduce the headers to:

```go
headers := []string{
	"NAME",
	"CATEGORY",
	"RECOMMENDED",
	"INSTALLED",
	"STATE",
}
```

Delete `targetVersion` calculation and remove the target cell from each row.
Do not change `ListResult.RawJSON`.

- [ ] **Step 4: Add optional localized values and concise rows**

Split localization into raw and scalar helpers:

```go
func localizedValue(values map[string]string) string {
	for _, key := range []string{"en", "en-US", "zh", "zh-CN"} {
		if value := values[key]; value != "" {
			return value
		}
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if values[key] != "" {
			return values[key]
		}
	}
	return ""
}

func localized(values map[string]string) string {
	return scalar(localizedValue(values))
}

func appendNonEmptyRow(rows *[][]string, field, value string) {
	if value != "" {
		*rows = append(*rows, []string{field, value})
	}
}
```

Change the renderer signature:

```go
func printShow(
	out io.Writer,
	result internalextension.ShowResult,
	format outputFormat,
) error
```

For exact selected versions, continue calling the unchanged
`printSelectedVersion`. For default table output, build:

```go
rows := [][]string{{"FIELD", "VALUE"}}
appendNonEmptyRow(&rows, "Name", extension.Metadata.Name)
appendNonEmptyRow(&rows, "Display Name",
	localizedValue(extension.Spec.DisplayName))
appendNonEmptyRow(&rows, "Description",
	localizedValue(extension.Spec.Description))
appendNonEmptyRow(&rows, "Category", extensionCategory(extension))
appendNonEmptyRow(&rows, "State", state)
appendNonEmptyRow(&rows, "Installed Version", installedVersion)
appendNonEmptyRow(&rows, "Recommended Version",
	extension.Status.RecommendedVersion)
```

For wide output, preserve the existing full rows with scalar `<none>` values.

- [ ] **Step 5: Render `clusterSchedulingStatuses`**

After the main field table succeeds, append the section only when an
InstallPlan exists and its status map is non-empty:

```go
func printClusterSchedulingStatuses(
	out io.Writer,
	statuses map[string]internalextension.InstallationStatus,
	format outputFormat,
) error {
	if len(statuses) == 0 {
		return nil
	}
	if _, err := io.WriteString(
		out,
		"\nclusterSchedulingStatuses\n\n",
	); err != nil {
		return err
	}
	headers := []string{"CLUSTER", "VERSION", "STATE"}
	if format == outputWide {
		headers = append(headers, "NAMESPACE", "JOB")
	}
	rows := [][]string{headers}
	for _, cluster := range slices.Sorted(maps.Keys(statuses)) {
		status := statuses[cluster]
		row := []string{
			cluster,
			scalar(status.Version),
			scalar(status.State),
		}
		if format == outputWide {
			row = append(
				row,
				scalar(status.TargetNamespace),
				scalar(status.JobName),
			)
		}
		rows = append(rows, row)
	}
	return writeTable(out, rows)
}
```

Call this helper after `writeTable(out, rows)`, propagating writer failures.

- [ ] **Step 6: Enable `-o wide` for show**

Parse with:

```go
format, err := parseOutput(output, true)
```

Change help to:

```go
"Output format: table, wide, json, or yaml"
```

Call:

```go
err = printShow(streams.Out, result, format)
```

Update all direct test calls to pass `outputTable` or `outputWide`.

- [ ] **Step 7: Run package and integration tests**

Run:

```bash
go test ./pkg/cmd/extension -count=1
```

Expected: PASS after updating integration table fixtures to the new list and
show contracts.

- [ ] **Step 8: Commit Task 3**

```bash
git add pkg/cmd/extension/query.go pkg/cmd/extension/output.go pkg/cmd/extension/output_test.go pkg/cmd/extension/query_test.go pkg/cmd/extension_integration_test.go
git commit -m "simplify extension query output"
```

---

### Task 4: Problem-Focused Diagnosis and Documentation

**Files:**
- Modify: `pkg/cmd/extension/diagnose.go:12-82`
- Modify: `pkg/cmd/extension/diagnose_test.go:12-218`
- Modify: `docs/cli.md:178-326`
- Modify: `docs/design.md:162-223`
- Modify: `docs/superpowers/specs/2026-07-28-ksctl-extension-management-design.md:140-205,347-375,562-610`

**Interfaces:**
- Produces: `printDiagnosis(io.Writer, string, Diagnosis, bool, bool) error`.
- Parameters after Diagnosis are `verbose` and `complete`.
- Default complete/healthy output is exactly
  `extension/NAME: healthy (N checks passed)\n`.
- Non-healthy and verbose output ends with
  `Summary: OK=N INFO=N WARN=N ERROR=N\n`.
- Incomplete output ends with
  `extension/NAME: diagnosis incomplete (N checks completed)\n`.

- [ ] **Step 1: Write failing diagnosis rendering tests**

Replace the old always-full default expectation and add:

```go
func TestDiagnoseHealthyDefaultPrintsSummaryOnly(t *testing.T) {
	streams, out, _ := bufferedStreams()
	service := &fakeService{diagnoseFn: func(
		context.Context,
		string,
		internalextension.DiagnoseOptions,
	) (internalextension.Diagnosis, error) {
		return internalextension.Diagnosis{Checks: []internalextension.DiagnosticCheck{
			{Name: "extension", Status: internalextension.DiagnosticOK},
			{Name: "install-plan", Status: internalextension.DiagnosticInfo},
		}}, nil
	}}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "diagnose", "demo"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := out.String(),
		"extension/demo: healthy (2 checks passed)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
```

Add a default issue test that supplies one status of each kind and asserts
only WARN/ERROR rows appear, in original order, followed by:

```text
Summary: OK=1 INFO=1 WARN=1 ERROR=1
```

Add a `--verbose` test asserting every row appears with the same summary.
Add a service-error test whose accumulated checks are all OK and expect:

```text
extension/demo: diagnosis incomplete (1 checks completed)
```

while `errors.Is(err, sentinel)` remains true.

- [ ] **Step 2: Run diagnosis tests and verify RED**

Run:

```bash
go test ./pkg/cmd/extension -run TestDiagnose -count=1
```

Expected: FAIL because default output still prints every check and
`--verbose` is unknown.

- [ ] **Step 3: Implement status counting and filtered rendering**

Add:

```go
type diagnosisCounts struct {
	ok    int
	info  int
	warn  int
	error int
}

func countDiagnosis(
	checks []internalextension.DiagnosticCheck,
) diagnosisCounts {
	var counts diagnosisCounts
	for _, check := range checks {
		switch check.Status {
		case internalextension.DiagnosticOK:
			counts.ok++
		case internalextension.DiagnosticInfo:
			counts.info++
		case internalextension.DiagnosticWarn:
			counts.warn++
		case internalextension.DiagnosticError:
			counts.error++
		}
	}
	return counts
}
```

Replace the renderer with:

```go
func printDiagnosis(
	out io.Writer,
	name string,
	diagnosis internalextension.Diagnosis,
	verbose bool,
	complete bool,
) error {
	counts := countDiagnosis(diagnosis.Checks)
	if complete && !verbose && counts.warn == 0 && counts.error == 0 {
		_, err := fmt.Fprintf(
			out,
			"extension/%s: healthy (%d checks passed)\n",
			tableCell(name),
			len(diagnosis.Checks),
		)
		return err
	}

	rows := [][]string{{"CHECK", "STATUS", "MESSAGE"}}
	for _, check := range diagnosis.Checks {
		if !verbose &&
			check.Status != internalextension.DiagnosticWarn &&
			check.Status != internalextension.DiagnosticError {
			continue
		}
		rows = append(rows, []string{
			check.Name,
			string(check.Status),
			check.Message,
		})
	}
	if len(rows) > 1 {
		if err := writeTable(out, rows); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		out,
		"Summary: OK=%d INFO=%d WARN=%d ERROR=%d\n",
		counts.ok,
		counts.info,
		counts.warn,
		counts.error,
	); err != nil {
		return err
	}
	if !complete {
		_, err := fmt.Fprintf(
			out,
			"extension/%s: diagnosis incomplete (%d checks completed)\n",
			tableCell(name),
			len(diagnosis.Checks),
		)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Add `--verbose` and preserve error ordering**

In the command:

```go
var verbose bool
```

Register:

```go
command.Flags().BoolVar(
	&verbose,
	"verbose",
	false,
	"Show every completed diagnostic check",
)
```

Always render after the service returns:

```go
if err := printDiagnosis(
	streams.Out,
	args[0],
	diagnosis,
	verbose,
	serviceErr == nil,
); err != nil {
	return fmt.Errorf("write extension diagnosis output: %w", err)
}
if serviceErr != nil {
	return serviceErr
}
return diagnosis.Err()
```

This preserves the original service-error and diagnosis-error precedence.

- [ ] **Step 5: Run diagnosis tests and package tests**

Run:

```bash
go test ./pkg/cmd/extension -count=1
```

Expected: PASS.

- [ ] **Step 6: Update user and design documentation**

Document these exact command forms:

```text
ksctl extension install NAME [--version VERSION] [--all-clusters]
ksctl extension show NAME [-o table|wide|json|yaml]
ksctl extension diagnose NAME [--target-cluster CLUSTER] [--verbose]
```

Amend the existing design's version-selection, list, show, install workflow,
diagnosis, and output-contract sections. State explicitly that all-cluster
selection includes host Clusters and writes a current eligible snapshot.

- [ ] **Step 7: Run formatting, docs checks, and full verification**

Run:

```bash
gofmt -w internal/extension/types.go internal/extension/rest_client.go internal/extension/rest_client_test.go internal/extension/fake_client_test.go internal/extension/lifecycle.go internal/extension/lifecycle_test.go pkg/cmd/extension/mutation.go pkg/cmd/extension/mutation_test.go pkg/cmd/extension/query.go pkg/cmd/extension/output.go pkg/cmd/extension/output_test.go pkg/cmd/extension/query_test.go pkg/cmd/extension/diagnose.go pkg/cmd/extension/diagnose_test.go pkg/cmd/extension_integration_test.go
git diff --check
make verify
```

Expected: all commands exit 0; `make verify` completes formatting, module,
vet, normal test, race test, and build stages successfully.

- [ ] **Step 8: Commit Task 4**

```bash
git add pkg/cmd/extension/diagnose.go pkg/cmd/extension/diagnose_test.go docs/cli.md docs/design.md docs/superpowers/specs/2026-07-28-ksctl-extension-management-design.md
git commit -m "improve extension diagnosis output"
```
