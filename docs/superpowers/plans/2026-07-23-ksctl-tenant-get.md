# ksctl Tenant Get Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add read-only `tenant get` commands for KSE 4.2.1 WorkspaceTemplates, Namespaces, and Clusters with correct Fleet/Cluster routing and kubectl-style table, JSON, and YAML output.

**Architecture:** A focused `pkg/client/kubesphere/tenant` package owns `/kapis/tenant.kubesphere.io/v1beta1` route construction and response decoding. Cobra commands reuse the existing KubeSphere connection getter, while a separate printer renders resource tables or preserves complete JSON/YAML server responses.

**Tech Stack:** Go 1.26, Cobra, `kubesphere.io/client-go/rest`, Kubernetes unstructured and duration helpers, cli-runtime tabwriter, `sigs.k8s.io/yaml`, `net/http/httptest`.

## Global Constraints

- Workspace commands present WorkspaceTemplates as Workspaces and always call Fleet-scoped `/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates[/{name}]`.
- Namespace commands optionally accept `--workspace` and follow explicit `--cluster` or Context `defaultCluster`.
- Cluster commands optionally accept `--workspace` and ignore explicit or default Cluster selection.
- `--workspace` has no short form; `-w` must be rejected.
- Accept `workspace|workspaces`, `namespace|namespaces|ns`, and `cluster|clusters`; do not accept `ws`.
- Default tables are Workspace `NAME CLUSTERS ADMINISTRATOR AGE`, Namespace `NAME STATUS AGE`, and Cluster `NAME PROVIDER VERSION`.
- JSON and YAML preserve the complete server response envelope and item order.
- Keep the command surface read-only and do not add pagination, filtering, sorting, watching, custom columns, templates, or name-only output.
- Follow RED-GREEN-REFACTOR for every production behavior.

## File Structure

- Create `pkg/client/kubesphere/tenant/client.go`: resource types, exact routing, GET execution, validation, and response decoding.
- Create `pkg/client/kubesphere/tenant/client_test.go`: routing, Cluster exception, decoding, and error tests.
- Create `pkg/cmd/tenant_print.go`: table, JSON, YAML, and AGE formatting.
- Create `pkg/cmd/tenant_print_test.go`: deterministic output tests.
- Create `pkg/cmd/tenant.go`: Cobra commands, aliases, long-form Workspace flags, and client invocation.
- Create `pkg/cmd/tenant_test.go`: end-to-end command routing and output-mode tests.
- Modify `pkg/cmd/root.go` and `pkg/cmd/root_test.go`: shared registration for both entrypoints.
- Modify `README.md`, `docs/cli.md`, `docs/design.md`, and `CHANGELOG.md`: user and architecture documentation.

---

### Task 1: Native Tenant API Client

**Files:**
- Create: `pkg/client/kubesphere/tenant/client_test.go`
- Create: `pkg/client/kubesphere/tenant/client.go`

**Interfaces:**
- Consumes: `kubesphererest.Interface`.
- Produces:

```go
type Resource string

const (
	ResourceWorkspace Resource = "workspace"
	ResourceNamespace Resource = "namespace"
	ResourceCluster   Resource = "cluster"
)

type Request struct {
	Resource  Resource
	Name      string
	Workspace string
	Cluster   string
}

type Response struct {
	Raw     []byte
	Objects []map[string]any
	IsList  bool
}

func New(restClient kubesphererest.Interface) *Client
func (c *Client) Get(ctx context.Context, request Request) (Response, error)
```

- [ ] **Step 1: Write failing route tests**

Create `pkg/client/kubesphere/tenant/client_test.go` with this route table and
a real unversioned REST client pointed at `httptest.Server`:

```go
package tenant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	kubesphererest "kubesphere.io/client-go/rest"
)

func TestClientGetRoutesTenantResources(t *testing.T) {
	tests := []struct {
		name     string
		request  Request
		wantPath string
		response string
		wantList bool
	}{
		{
			name:     "workspace template list ignores cluster",
			request:  Request{Resource: ResourceWorkspace, Cluster: "ignored"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
			response: `{"items":[{"metadata":{"name":"platform"}}],"total_count":1}`,
			wantList: true,
		},
		{
			name:     "named workspace template ignores cluster",
			request:  Request{Resource: ResourceWorkspace, Name: "platform", Cluster: "ignored"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates/platform",
			response: `{"metadata":{"name":"platform"}}`,
		},
		{
			name:     "namespace list follows cluster",
			request:  Request{Resource: ResourceNamespace, Cluster: "member"},
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantList: true,
		},
		{
			name:     "workspace namespace list follows cluster",
			request:  Request{Resource: ResourceNamespace, Workspace: "platform", Cluster: "member"},
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantList: true,
		},
		{
			name:     "cluster list ignores cluster",
			request:  Request{Resource: ResourceCluster, Cluster: "ignored"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantList: true,
		},
		{
			name:     "workspace cluster list ignores cluster",
			request:  Request{Resource: ResourceCluster, Workspace: "platform", Cluster: "ignored"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantList: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, test.wantPath)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer secret" {
					t.Errorf("Authorization = %q, want Bearer secret", got)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.response)
			}))
			defer server.Close()

			restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(
				&kubesphererest.Config{Host: server.URL, BearerToken: "secret"},
			)
			if err != nil {
				t.Fatalf("ForConfig() error = %v", err)
			}
			response, err := New(restClient).Get(context.Background(), test.request)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if response.IsList != test.wantList {
				t.Fatalf("IsList = %v, want %v", response.IsList, test.wantList)
			}
			if len(response.Objects) != 1 {
				t.Fatalf("Objects = %#v, want one object", response.Objects)
			}
			if string(response.Raw) != test.response {
				t.Fatalf("Raw = %q, want %q", response.Raw, test.response)
			}
		})
	}
}

func TestClientGetRejectsInvalidRequestsBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()
	restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(
		&kubesphererest.Config{Host: server.URL},
	)
	if err != nil {
		t.Fatalf("ForConfig() error = %v", err)
	}

	tests := []struct {
		request Request
		want    string
	}{
		{request: Request{Resource: "widget"}, want: "unsupported tenant resource"},
		{request: Request{Resource: ResourceWorkspace, Workspace: "platform"}, want: "workspace scope is not supported"},
		{request: Request{Resource: ResourceNamespace, Name: "demo"}, want: "resource name is only supported"},
		{request: Request{Resource: ResourceWorkspace, Name: "team/member"}, want: "invalid workspace name"},
		{request: Request{Resource: ResourceNamespace, Workspace: ".."}, want: "invalid workspace"},
		{request: Request{Resource: ResourceNamespace, Cluster: "team/member"}, want: "invalid cluster"},
	}
	for _, test := range tests {
		_, err := New(restClient).Get(context.Background(), test.request)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Get(%#v) error = %v, want %q", test.request, err, test.want)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestClientGetRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		response string
		want     string
	}{
		{response: `{`, want: "decode tenant workspace response"},
		{response: `{"totalItems":0}`, want: `missing "items" array`},
		{response: `{"items":["bad"]}`, want: "item 0 is not an object"},
	}
	for _, test := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, test.response)
		}))
		restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(
			&kubesphererest.Config{Host: server.URL},
		)
		if err != nil {
			t.Fatalf("ForConfig() error = %v", err)
		}
		_, err = New(restClient).Get(context.Background(), Request{Resource: ResourceWorkspace})
		server.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Get() error = %v, want %q", err, test.want)
		}
	}
}
```

- [ ] **Step 2: Run the client tests and verify RED**

Run:

```bash
go test ./pkg/client/kubesphere/tenant -count=1
```

Expected: FAIL because the tenant client package does not exist.

- [ ] **Step 3: Implement routing and response decoding**

Create `pkg/client/kubesphere/tenant/client.go`:

```go
package tenant

import (
	"context"
	"encoding/json"
	"fmt"

	kubesphererest "kubesphere.io/client-go/rest"
)

const apiPath = "/kapis/tenant.kubesphere.io/v1beta1"

type Resource string

const (
	ResourceWorkspace Resource = "workspace"
	ResourceNamespace Resource = "namespace"
	ResourceCluster   Resource = "cluster"
)

type Request struct {
	Resource  Resource
	Name      string
	Workspace string
	Cluster   string
}

type Response struct {
	Raw     []byte
	Objects []map[string]any
	IsList  bool
}

type Client struct {
	restClient kubesphererest.Interface
}

func New(restClient kubesphererest.Interface) *Client {
	return &Client{restClient: restClient}
}

func (c *Client) Get(ctx context.Context, request Request) (Response, error) {
	if c == nil || c.restClient == nil {
		return Response{}, fmt.Errorf("KubeSphere REST client is required")
	}
	segments, list, err := requestSegments(request)
	if err != nil {
		return Response{}, err
	}
	get := c.restClient.Get()
	if request.Resource == ResourceNamespace && request.Cluster != "" {
		get.Cluster(request.Cluster)
	}
	raw, err := get.AbsPath(segments...).Do(ctx).Raw()
	if err != nil {
		return Response{}, fmt.Errorf("get tenant %s: %w", request.Resource, err)
	}
	objects, err := decodeObjects(raw, list, request.Resource)
	if err != nil {
		return Response{}, err
	}
	return Response{Raw: raw, Objects: objects, IsList: list}, nil
}

func requestSegments(request Request) ([]string, bool, error) {
	if request.Name != "" && request.Resource != ResourceWorkspace {
		return nil, false, fmt.Errorf("tenant resource name is only supported for workspace")
	}
	if request.Workspace != "" && request.Resource == ResourceWorkspace {
		return nil, false, fmt.Errorf("workspace scope is not supported for tenant workspace")
	}
	if err := validateSegment("workspace name", request.Name); err != nil {
		return nil, false, err
	}
	if err := validateSegment("workspace", request.Workspace); err != nil {
		return nil, false, err
	}
	if err := validateSegment("cluster", request.Cluster); err != nil {
		return nil, false, err
	}

	switch request.Resource {
	case ResourceWorkspace:
		if request.Name == "" {
			return []string{apiPath, "workspacetemplates"}, true, nil
		}
		return []string{apiPath, "workspacetemplates", request.Name}, false, nil
	case ResourceNamespace:
		if request.Workspace == "" {
			return []string{apiPath, "namespaces"}, true, nil
		}
		return []string{apiPath, "workspaces", request.Workspace, "namespaces"}, true, nil
	case ResourceCluster:
		if request.Workspace == "" {
			return []string{apiPath, "clusters"}, true, nil
		}
		return []string{apiPath, "workspaces", request.Workspace, "clusters"}, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported tenant resource %q", request.Resource)
	}
}

func validateSegment(label, value string) error {
	if value == "" {
		return nil
	}
	if messages := kubesphererest.IsValidPathSegmentName(value); len(messages) != 0 {
		return fmt.Errorf("invalid %s %q: %v", label, value, messages)
	}
	return nil
}

func decodeObjects(raw []byte, list bool, resource Resource) ([]map[string]any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode tenant %s response: %w", resource, err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tenant %s response is not an object", resource)
	}
	if !list {
		return []map[string]any{object}, nil
	}
	rawItems, found := object["items"]
	if !found {
		return nil, fmt.Errorf(`tenant %s list response is missing "items" array`, resource)
	}
	items, ok := rawItems.([]any)
	if !ok {
		return nil, fmt.Errorf(`tenant %s list response "items" is not an array`, resource)
	}
	objects := make([]map[string]any, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tenant %s list item %d is not an object", resource, index)
		}
		objects = append(objects, object)
	}
	return objects, nil
}
```

- [ ] **Step 4: Run tests, format, and commit**

Run:

```bash
go test ./pkg/client/kubesphere/tenant -count=1
gofmt -w pkg/client/kubesphere/tenant/client.go pkg/client/kubesphere/tenant/client_test.go
go test ./pkg/client/kubesphere/tenant -count=1
git add pkg/client/kubesphere/tenant/client.go pkg/client/kubesphere/tenant/client_test.go
git commit -m "add tenant API client"
```

Expected: both test runs after implementation pass; the commit contains only
the client and its tests.

---

### Task 2: Kubectl-Style Tenant Printers

**Files:**
- Create: `pkg/cmd/tenant_print_test.go`
- Create: `pkg/cmd/tenant_print.go`

**Interfaces:**
- Consumes: `tenant.Response`, resource kind, and a deterministic clock.
- Produces:

```go
func printTenantResponse(
	out io.Writer,
	format string,
	resource tenant.Resource,
	response tenant.Response,
	now time.Time,
) error
```

- [ ] **Step 1: Write failing printer tests**

Create `pkg/cmd/tenant_print_test.go`:

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tenantclient "github.com/kubesphere/ksctl/pkg/client/kubesphere/tenant"
)

func TestPrintTenantResponseTables(t *testing.T) {
	now := time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		resource tenantclient.Resource
		objects  []map[string]any
		want     []string
	}{
		{
			resource: tenantclient.ResourceWorkspace,
			objects: []map[string]any{{
				"metadata": map[string]any{"name": "platform", "creationTimestamp": "2026-07-15T14:00:00Z"},
				"spec": map[string]any{
					"placement": map[string]any{"clusters": []any{
						map[string]any{"name": "host"},
						map[string]any{"name": "member"},
					}},
					"template": map[string]any{"spec": map[string]any{"manager": "admin"}},
				},
			}},
			want: []string{"NAME", "CLUSTERS", "ADMINISTRATOR", "AGE", "platform", "host,member", "admin", "8d"},
		},
		{
			resource: tenantclient.ResourceNamespace,
			objects: []map[string]any{{
				"metadata": map[string]any{"name": "demo", "creationTimestamp": "2026-07-03T14:00:00Z"},
				"status":   map[string]any{"phase": "Active"},
			}},
			want: []string{"NAME", "STATUS", "AGE", "demo", "Active", "20d"},
		},
		{
			resource: tenantclient.ResourceCluster,
			objects: []map[string]any{
				{
					"metadata": map[string]any{"name": "host"},
					"spec":     map[string]any{"provider": "kubesphere"},
					"status":   map[string]any{"kubernetesVersion": "v1.33.1"},
				},
				{
					"metadata": map[string]any{"name": "member"},
					"spec":     map[string]any{},
					"status":   map[string]any{"kubernetesVersion": "v1.23.17"},
				},
			},
			want: []string{"NAME", "PROVIDER", "VERSION", "host", "kubesphere", "v1.33.1", "member", "v1.23.17"},
		},
	}
	for _, test := range tests {
		out := new(bytes.Buffer)
		err := printTenantResponse(out, "table", test.resource,
			tenantclient.Response{Objects: test.objects, IsList: true}, now)
		if err != nil {
			t.Fatalf("printTenantResponse() error = %v", err)
		}
		for _, want := range test.want {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("output missing %q:\n%s", want, out.String())
			}
		}
	}
}

func TestPrintTenantResponseUnknownAgeAndEmptyList(t *testing.T) {
	now := time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC)
	out := new(bytes.Buffer)
	err := printTenantResponse(out, "table", tenantclient.ResourceNamespace,
		tenantclient.Response{Objects: []map[string]any{{
			"metadata": map[string]any{"name": "demo"},
			"status":   map[string]any{"phase": "Active"},
		}}}, now)
	if err != nil || !strings.Contains(out.String(), "<unknown>") {
		t.Fatalf("unknown age output = %q, error = %v", out.String(), err)
	}

	out.Reset()
	err = printTenantResponse(out, "table", tenantclient.ResourceNamespace,
		tenantclient.Response{Objects: []map[string]any{}}, now)
	if err != nil || out.String() != "No resources found\n" {
		t.Fatalf("empty output = %q, error = %v", out.String(), err)
	}
}

func TestPrintTenantResponseStructuredOutput(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"demo"}}],"total_count":1}`)
	response := tenantclient.Response{Raw: raw, IsList: true}

	jsonOut := new(bytes.Buffer)
	if err := printTenantResponse(jsonOut, "json", tenantclient.ResourceWorkspace, response, time.Now()); err != nil {
		t.Fatalf("JSON error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil || decoded["total_count"] != float64(1) {
		t.Fatalf("JSON output = %q, error = %v", jsonOut.String(), err)
	}

	yamlOut := new(bytes.Buffer)
	if err := printTenantResponse(yamlOut, "yaml", tenantclient.ResourceWorkspace, response, time.Now()); err != nil {
		t.Fatalf("YAML error = %v", err)
	}
	for _, want := range []string{"items:", "name: demo", "total_count: 1"} {
		if !strings.Contains(yamlOut.String(), want) {
			t.Fatalf("YAML output missing %q:\n%s", want, yamlOut.String())
		}
	}
}

func TestPrintTenantResponseErrors(t *testing.T) {
	err := printTenantResponse(new(bytes.Buffer), "wide", tenantclient.ResourceWorkspace,
		tenantclient.Response{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Fatalf("format error = %v", err)
	}
	err = printTenantResponse(errorWriter{}, "json", tenantclient.ResourceWorkspace,
		tenantclient.Response{Raw: []byte(`{"items":[]}`)}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "write tenant JSON output") {
		t.Fatalf("writer error = %v", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
```

- [ ] **Step 2: Run printer tests and verify RED**

Run:

```bash
go test ./pkg/cmd -run 'TestPrintTenantResponse' -count=1
```

Expected: FAIL with `undefined: printTenantResponse`.

- [ ] **Step 3: Implement the printer**

Create `pkg/cmd/tenant_print.go`:

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	tenantclient "github.com/kubesphere/ksctl/pkg/client/kubesphere/tenant"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/cli-runtime/pkg/printers"
	"sigs.k8s.io/yaml"
)

func printTenantResponse(out io.Writer, format string, resource tenantclient.Resource,
	response tenantclient.Response, now time.Time) error {
	switch format {
	case "json":
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, response.Raw, "", "  "); err != nil {
			return fmt.Errorf("format tenant JSON output: %w", err)
		}
		formatted.WriteByte('\n')
		if _, err := io.Copy(out, &formatted); err != nil {
			return fmt.Errorf("write tenant JSON output: %w", err)
		}
		return nil
	case "yaml":
		data, err := yaml.JSONToYAML(response.Raw)
		if err != nil {
			return fmt.Errorf("format tenant YAML output: %w", err)
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			data = append(data, '\n')
		}
		if _, err := out.Write(data); err != nil {
			return fmt.Errorf("write tenant YAML output: %w", err)
		}
		return nil
	case "table":
		return printTenantTable(out, resource, response.Objects, now)
	default:
		return fmt.Errorf("unsupported output format %q: must be table, json, or yaml", format)
	}
}

func printTenantTable(out io.Writer, resource tenantclient.Resource,
	objects []map[string]any, now time.Time) error {
	if len(objects) == 0 {
		if _, err := fmt.Fprintln(out, "No resources found"); err != nil {
			return fmt.Errorf("write tenant table output: %w", err)
		}
		return nil
	}
	writer := printers.GetNewTabWriter(out)
	switch resource {
	case tenantclient.ResourceWorkspace:
		fmt.Fprintln(writer, "NAME\tCLUSTERS\tADMINISTRATOR\tAGE")
		for _, object := range objects {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
				nestedString(object, "metadata", "name"),
				workspaceClusters(object),
				nestedString(object, "spec", "template", "spec", "manager"),
				objectAge(object, now))
		}
	case tenantclient.ResourceNamespace:
		fmt.Fprintln(writer, "NAME\tSTATUS\tAGE")
		for _, object := range objects {
			fmt.Fprintf(writer, "%s\t%s\t%s\n",
				nestedString(object, "metadata", "name"),
				nestedString(object, "status", "phase"),
				objectAge(object, now))
		}
	case tenantclient.ResourceCluster:
		fmt.Fprintln(writer, "NAME\tPROVIDER\tVERSION")
		for _, object := range objects {
			fmt.Fprintf(writer, "%s\t%s\t%s\n",
				nestedString(object, "metadata", "name"),
				nestedString(object, "spec", "provider"),
				nestedString(object, "status", "kubernetesVersion"))
		}
	default:
		return fmt.Errorf("unsupported tenant resource %q", resource)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write tenant table output: %w", err)
	}
	return nil
}

func nestedString(object map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(object, fields...)
	if err != nil || !found {
		return ""
	}
	return value
}

func workspaceClusters(object map[string]any) string {
	clusters, found, err := unstructured.NestedSlice(object, "spec", "placement", "clusters")
	if err != nil || !found {
		return ""
	}
	names := make([]string, 0, len(clusters))
	for _, item := range clusters {
		cluster, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name := nestedString(cluster, "name"); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

func objectAge(object map[string]any, now time.Time) string {
	created, err := time.Parse(time.RFC3339, nestedString(object, "metadata", "creationTimestamp"))
	if err != nil {
		return "<unknown>"
	}
	return duration.HumanDuration(now.Sub(created))
}
```

- [ ] **Step 4: Run tests, format, and commit**

Run:

```bash
go test ./pkg/cmd -run 'TestPrintTenantResponse' -count=1
gofmt -w pkg/cmd/tenant_print.go pkg/cmd/tenant_print_test.go
go test ./pkg/cmd -run 'TestPrintTenantResponse' -count=1
git add pkg/cmd/tenant_print.go pkg/cmd/tenant_print_test.go
git commit -m "add tenant output printers"
```

Expected: PASS and one printer-only commit.

---

### Task 3: Command Tree, Flags, and Root Wiring

**Files:**
- Create: `pkg/cmd/tenant_test.go`
- Create: `pkg/cmd/tenant.go`
- Modify: `pkg/cmd/root.go`
- Modify: `pkg/cmd/root_test.go`

**Interfaces:**
- Consumes: Task 1's client, Task 2's printer, and the existing getter methods
  `ToRESTConfig()` and `KubeSphereCluster()`.
- Produces: `newTenantCommand(getter tenantRESTClientGetter) *cobra.Command`.

- [ ] **Step 1: Write failing root registration tests**

Add to `pkg/cmd/root_test.go`:

```go
func TestRootRegistersTenantGetCommands(t *testing.T) {
	for _, root := range []*cobra.Command{
		NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}),
		NewKubectlPluginCommand(IOStreams{}, VersionInfo{Version: "dev"}),
	} {
		tenant := findSubcommand(root, "tenant")
		if tenant == nil {
			t.Fatal("tenant command is not registered")
		}
		get := findSubcommand(tenant, "get")
		if get == nil {
			t.Fatal("tenant get command is not registered")
		}
		for _, name := range []string{"workspace", "namespace", "cluster"} {
			if findSubcommand(get, name) == nil {
				t.Fatalf("tenant get %s is not registered", name)
			}
		}
		for _, name := range []string{"namespace", "cluster"} {
			flag := findSubcommand(get, name).Flags().Lookup("workspace")
			if flag == nil || flag.Shorthand != "" {
				t.Fatalf("tenant get %s workspace flag = %#v, want long form only", name, flag)
			}
		}
		if findSubcommand(get, "workspace").Flags().Lookup("workspace") != nil {
			t.Fatal("tenant get workspace accepts --workspace")
		}
	}
}
```

- [ ] **Step 2: Write failing end-to-end routing tests**

Create `pkg/cmd/tenant_test.go`:

```go
package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
)

func TestTenantGetRoutesAndAliases(t *testing.T) {
	tests := []struct {
		args     []string
		wantPath string
		response string
		wantOut  string
	}{
		{
			args:     []string{"tenant", "get", "workspaces", "--cluster", "ignored", "-o", "json"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
			response: `{"items":[{"metadata":{"name":"platform"}}],"total_count":1}`,
			wantOut:  `"name": "platform"`,
		},
		{
			args:     []string{"tenant", "get", "workspace", "platform", "--cluster", "ignored", "-o", "json"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates/platform",
			response: `{"metadata":{"name":"platform"}}`,
			wantOut:  `"name": "platform"`,
		},
		{
			args:     []string{"tenant", "get", "ns", "--cluster", "member", "-o", "yaml"},
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantOut:  "name: demo",
		},
		{
			args:     []string{"tenant", "get", "namespaces", "--workspace", "platform", "--cluster", "member", "-o", "json"},
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantOut:  `"name": "demo"`,
		},
		{
			args:     []string{"tenant", "get", "clusters", "--cluster", "ignored", "-o", "json"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantOut:  `"name": "host"`,
		},
		{
			args:     []string{"tenant", "get", "cluster", "--workspace", "platform", "--cluster", "ignored", "-o", "json"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantOut:  `"name": "host"`,
		},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args[:3], " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, test.wantPath)
				}
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
				}
				if r.Header.Get("User-Agent") != "ksctl/test" {
					t.Errorf("User-Agent = %q, want ksctl/test", r.Header.Get("User-Agent"))
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, test.response)
			}))
			defer server.Close()
			t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

			out := new(bytes.Buffer)
			command := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
			command.SetArgs(append(test.args, "--endpoint", server.URL, "--token", "secret"))
			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), test.wantOut) {
				t.Fatalf("output missing %q:\n%s", test.wantOut, out.String())
			}
		})
	}
}

func TestTenantGetUsesDefaultClusterOnlyForNamespaces(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items":[],"totalItems":0}`)
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: server.URL, Users: map[string]config.User{
		"admin": {BearerToken: "secret"},
	}}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "admin", DefaultCluster: "member"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	for _, args := range [][]string{
		{"tenant", "get", "workspace", "-o", "json"},
		{"tenant", "get", "ns", "-o", "json"},
		{"tenant", "get", "cluster", "-o", "json"},
	} {
		command := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
	}
	want := []string{
		"/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
		"/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
		"/kapis/tenant.kubesphere.io/v1beta1/clusters",
	}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestTenantGetRejectsShortWorkspaceFlagAndAliases(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"tenant", "get", "ws"}, want: "unknown command"},
		{args: []string{"tenant", "get", "ns", "-w", "platform"}, want: "unknown shorthand flag"},
		{args: []string{"tenant", "get", "cluster", "-w", "platform"}, want: "unknown shorthand flag"},
		{args: []string{"tenant", "get", "workspace", "one", "two"}, want: "accepts at most 1 arg"},
		{args: []string{"tenant", "get", "ns", "demo"}, want: "accepts 0 arg"},
		{args: []string{"tenant", "get", "workspace", "-o", "wide"}, want: "unsupported output format"},
	}
	for _, test := range tests {
		command := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
		command.SetArgs(test.args)
		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestTenantGetHonorsRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	command := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	command.SetArgs([]string{
		"tenant", "get", "workspace",
		"--endpoint", server.URL,
		"--token", "secret",
		"--request-timeout", "20ms",
	})
	if err := command.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want request timeout")
	}
}
```

- [ ] **Step 3: Run command tests and verify RED**

Run:

```bash
go test ./pkg/cmd -run 'TestRootRegistersTenant|TestTenantGet' -count=1
```

Expected: FAIL because `tenant` is not registered.

- [ ] **Step 4: Implement the command**

Create `pkg/cmd/tenant.go`:

```go
package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	tenantclient "github.com/kubesphere/ksctl/pkg/client/kubesphere/tenant"
	"github.com/spf13/cobra"
	kubesphererest "kubesphere.io/client-go/rest"
)

type tenantRESTClientGetter interface {
	ToRESTConfig() (*kubesphererest.Config, error)
	KubeSphereCluster() (string, error)
}

func newTenantCommand(getter tenantRESTClientGetter) *cobra.Command {
	tenantCommand := &cobra.Command{Use: "tenant", Short: "Inspect KubeSphere tenant resources"}
	get := &cobra.Command{
		Use: "get", Short: "Display KubeSphere tenant resources", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return fmt.Errorf("tenant resource is required") },
	}
	var output string
	get.PersistentFlags().StringVarP(&output, "output", "o", "table", "Output format: table, json, or yaml")

	workspace := &cobra.Command{
		Use: "workspace [NAME]", Aliases: []string{"workspaces"},
		Short: "Display KubeSphere workspaces", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runTenantGet(cmd.Context(), cmd.OutOrStdout(), getter,
				tenantclient.Request{Resource: tenantclient.ResourceWorkspace, Name: name}, output)
		},
	}

	var namespaceWorkspace string
	namespace := &cobra.Command{
		Use: "namespace", Aliases: []string{"namespaces", "ns"},
		Short: "Display KubeSphere namespaces", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTenantGet(cmd.Context(), cmd.OutOrStdout(), getter,
				tenantclient.Request{Resource: tenantclient.ResourceNamespace, Workspace: namespaceWorkspace}, output)
		},
	}
	namespace.Flags().StringVar(&namespaceWorkspace, "workspace", "", "KubeSphere workspace name")

	var clusterWorkspace string
	cluster := &cobra.Command{
		Use: "cluster", Aliases: []string{"clusters"},
		Short: "Display KubeSphere clusters", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTenantGet(cmd.Context(), cmd.OutOrStdout(), getter,
				tenantclient.Request{Resource: tenantclient.ResourceCluster, Workspace: clusterWorkspace}, output)
		},
	}
	cluster.Flags().StringVar(&clusterWorkspace, "workspace", "", "KubeSphere workspace name")

	get.AddCommand(workspace, namespace, cluster)
	tenantCommand.AddCommand(get)
	return tenantCommand
}

func runTenantGet(ctx context.Context, out io.Writer, getter tenantRESTClientGetter,
	request tenantclient.Request, output string) error {
	if err := validateTenantOutput(output); err != nil {
		return err
	}
	if err := validateTenantInput(request); err != nil {
		return err
	}
	if getter == nil {
		return fmt.Errorf("KubeSphere REST client getter is required")
	}
	cluster, err := getter.KubeSphereCluster()
	if err != nil {
		return fmt.Errorf("resolve tenant cluster: %w", err)
	}
	request.Cluster = cluster
	config, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("resolve tenant connection: %w", err)
	}
	restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(config)
	if err != nil {
		return fmt.Errorf("build tenant client: %w", err)
	}
	response, err := tenantclient.New(restClient).Get(ctx, request)
	if err != nil {
		return err
	}
	return printTenantResponse(out, output, request.Resource, response, time.Now())
}

func validateTenantOutput(output string) error {
	switch output {
	case "table", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("unsupported output format %q: must be table, json, or yaml", output)
	}
}

func validateTenantInput(request tenantclient.Request) error {
	for label, value := range map[string]string{
		"workspace name": request.Name,
		"workspace":      request.Workspace,
	} {
		if value == "" {
			continue
		}
		if messages := kubesphererest.IsValidPathSegmentName(value); len(messages) != 0 {
			return fmt.Errorf("invalid %s %q: %v", label, value, messages)
		}
	}
	return nil
}
```

- [ ] **Step 5: Register with the shared root**

In `pkg/cmd/root.go`, add:

```go
cmd.AddCommand(newTenantCommand(kubeSphereGetter))
```

immediately after the existing auth/config/version registrations.

- [ ] **Step 6: Run, format, regress, and commit**

Run:

```bash
go test ./pkg/cmd -run 'TestRootRegistersTenant|TestTenantGet' -count=1
gofmt -w pkg/cmd/tenant.go pkg/cmd/tenant_test.go pkg/cmd/root.go pkg/cmd/root_test.go
go test ./pkg/cmd -count=1
git add pkg/cmd/tenant.go pkg/cmd/tenant_test.go pkg/cmd/root.go pkg/cmd/root_test.go
git commit -m "add tenant get commands"
```

Expected: all `pkg/cmd` tests pass and the commit contains command wiring only.

---

### Task 4: Documentation and Complete Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/cli.md`
- Modify: `docs/design.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces: documented CLI behavior and final verification evidence.

- [ ] **Step 1: Update user documentation**

Add these examples to `README.md` and `docs/cli.md`:

```bash
ksctl tenant get workspace
ksctl tenant get workspace platform
ksctl tenant get ns --workspace platform
ksctl tenant get cluster --workspace platform
```

Document these exact points:

```markdown
- Workspace commands query Fleet-scoped `workspacetemplates`.
- Namespace commands follow `--cluster` or Context `defaultCluster`.
- Cluster commands ignore Cluster selection.
- `--workspace` has no short form.
- Default columns are Workspace `NAME CLUSTERS ADMINISTRATOR AGE`, Namespace
  `NAME STATUS AGE`, and Cluster `NAME PROVIDER VERSION`.
- `-o json` and `-o yaml` preserve the complete KSE response.
```

- [ ] **Step 2: Update architecture and changelog**

Add this pipeline to `docs/design.md`:

```text
Cobra tenant command
  -> KubeSphere connection getter
  -> native tenant client
  -> /kapis/tenant.kubesphere.io/v1beta1
  -> tenant table, JSON, or YAML printer
```

Add under `CHANGELOG.md` Unreleased / Added:

```markdown
- Add native `tenant get` commands for KSE Workspaces, Namespaces, and
  Clusters with optional Workspace scope and table, JSON, or YAML output.
```

- [ ] **Step 3: Verify and commit documentation**

Run:

```bash
rg -n "tenant get|workspacetemplates|tenant\\.kubesphere\\.io|ADMINISTRATOR|PROVIDER" README.md docs/cli.md docs/design.md CHANGELOG.md
git diff --check
git add README.md docs/cli.md docs/design.md CHANGELOG.md
git commit -m "document tenant get commands"
```

Expected: documentation matches the implemented routes and the commit is
documentation-only.

- [ ] **Step 4: Run focused and repository-wide verification**

Run:

```bash
go test ./pkg/client/kubesphere/tenant ./pkg/cmd -count=1
make fmt-check
make mod-check
make verify
git diff --check
git status --short
```

Expected: every command exits 0, both binaries build, normal and race tests
pass, module files stay unchanged, and the worktree has no uncommitted
implementation changes.

- [ ] **Step 5: Smoke-test built command help**

Run:

```bash
./bin/ksctl tenant get --help
./bin/ksctl tenant get workspace --help
./bin/ksctl tenant get ns --help
./bin/ksctl tenant get cluster --help
./bin/ksctl version
```

Expected: Namespace and Cluster help show `--workspace` without `-w`;
Workspace help has no Workspace flag; version prints successfully.
