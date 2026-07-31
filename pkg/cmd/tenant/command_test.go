package tenant

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/rest"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	kubesphererest "kubesphere.io/client-go/rest"
)

func TestCommandKeepsNativeResourcesAheadOfGenericGet(t *testing.T) {
	namespace := ""
	command := NewCommandWithOptions(CommandOptions{
		DisplayName:      "ksctl",
		KubeSphereGetter: fakeRESTClientGetter{},
		KubernetesGetter: fakeKubernetesRESTClientGetter{},
		Streams: genericiooptions.IOStreams{
			Out:    io.Discard,
			ErrOut: io.Discard,
		},
		Namespace: &namespace,
	})

	get := findCommand(command, "get")
	if get == nil {
		t.Fatal("get command is missing")
	}
	for _, name := range []string{"workspace", "namespace", "cluster"} {
		if findCommand(get, name) == nil {
			t.Fatalf("native child %q is missing", name)
		}
	}
	for _, name := range []string{
		"all-namespaces",
		"selector",
		"field-selector",
		"output",
		"workspace",
		"namespace",
	} {
		if get.Flags().Lookup(name) == nil {
			t.Fatalf("generic get flag --%s is missing", name)
		}
	}
}

func TestCommandRoutesResourcesAndAliases(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		cluster  string
		wantPath string
		response string
		wantOut  string
	}{
		{
			name:     "workspaces plural ignores cluster",
			args:     []string{"get", "workspaces", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
			response: `{"items":[{"metadata":{"name":"platform"}}],"total_count":1}`,
			wantOut:  `"name": "platform"`,
		},
		{
			name:     "named workspace ignores cluster",
			args:     []string{"get", "workspace", "platform", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates/platform",
			response: `{"metadata":{"name":"platform"}}`,
			wantOut:  `"name": "platform"`,
		},
		{
			name:     "namespace alias follows cluster",
			args:     []string{"get", "ns", "-o", "yaml"},
			cluster:  "member",
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantOut:  "name: demo",
		},
		{
			name:     "workspace namespace follows cluster",
			args:     []string{"get", "namespaces", "--workspace", "platform", "-o", "json"},
			cluster:  "member",
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantOut:  `"name": "demo"`,
		},
		{
			name:     "clusters plural ignores cluster",
			args:     []string{"get", "clusters", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantOut:  `"name": "host"`,
		},
		{
			name:     "workspace cluster ignores cluster",
			args:     []string{"get", "cluster", "--workspace", "platform", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantOut:  `"name": "host"`,
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
				if got := r.Header.Get("User-Agent"); got != "ksctl/test" {
					t.Errorf("User-Agent = %q, want ksctl/test", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()

			out := new(bytes.Buffer)
			command := NewCommand(fakeRESTClientGetter{
				config:  &kubesphererest.Config{Host: server.URL, BearerToken: "secret", UserAgent: "ksctl/test"},
				cluster: test.cluster,
			})
			command.SetOut(out)
			command.SetErr(new(bytes.Buffer))
			command.SetArgs(test.args)
			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), test.wantOut) {
				t.Fatalf("output missing %q:\n%s", test.wantOut, out.String())
			}
		})
	}
}

func TestCommandWorkspaceFlagsHaveNoShortForm(t *testing.T) {
	command := NewCommand(fakeRESTClientGetter{})
	get := findCommand(command, "get")
	if get == nil {
		t.Fatal("get command is missing")
	}
	for _, name := range []string{"namespace", "cluster"} {
		resource := findCommand(get, name)
		if resource == nil {
			t.Fatalf("%s command is missing", name)
		}
		flag := resource.Flags().Lookup("workspace")
		if flag == nil || flag.Shorthand != "" {
			t.Fatalf("%s workspace flag = %#v, want long form only", name, flag)
		}
	}
	if findCommand(get, "workspace").Flags().Lookup("workspace") != nil {
		t.Fatal("workspace command unexpectedly accepts --workspace")
	}
}

func TestCommandRejectsUnsupportedInputsBeforeConnection(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"get", "ws"}, want: "unknown command"},
		{args: []string{"get", "ns", "-w", "platform"}, want: "unknown shorthand flag"},
		{args: []string{"get", "cluster", "-w", "platform"}, want: "unknown shorthand flag"},
		{args: []string{"get", "workspace", "one", "two"}, want: "accepts at most 1 arg"},
		{args: []string{"get", "ns", "demo"}, want: `unknown command "demo"`},
		{args: []string{"get", "workspace", "-o", "wide"}, want: "unsupported output format"},
		{args: []string{"get", "ns", "--workspace", "team/member"}, want: "invalid workspace"},
	}
	for _, test := range tests {
		command := NewCommand(fakeRESTClientGetter{})
		command.SetOut(new(bytes.Buffer))
		command.SetErr(new(bytes.Buffer))
		command.SetArgs(test.args)
		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestCommandRejectsInvalidWorkspaceResourceQueriesBeforeRequest(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{
			args: []string{"get", "pods", "--workspace", "platform", "-n", "demo"},
			want: "--workspace and --namespace",
		},
		{
			args: []string{"get", "pods", "--workspace", "platform", "-A"},
			want: "--workspace and --all-namespaces",
		},
		{
			args: []string{"get", "pods", "--workspace", "platform", "-w"},
			want: "watch is not supported",
		},
		{
			args: []string{"get", "--raw", "/api/v1/pods", "--workspace", "platform"},
			want: "--workspace and --raw",
		},
		{
			args: []string{"get", "-f", "pod.yaml", "--workspace", "platform"},
			want: "--workspace and --filename",
		},
		{
			args: []string{"get", "pod/web", "--workspace", "platform"},
			want: "collection queries",
		},
		{
			args: []string{"get", "nodes", "--workspace", "platform"},
			want: "cluster-scoped",
		},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			var requests int
			getter := &transportTestGetter{
				config: &rest.Config{
					Host: "https://example.test",
					Transport: roundTripperFunc(func(
						request *http.Request,
					) (*http.Response, error) {
						requests++
						return jsonResponse(
							request,
							http.StatusInternalServerError,
							`{"kind":"Status"}`,
						), nil
					}),
				},
				mapper: testRESTMapper(),
				loader: defaultClientConfig(),
			}
			command := NewCommandWithOptions(CommandOptions{
				DisplayName:      "ksctl",
				KubeSphereGetter: fakeRESTClientGetter{},
				KubernetesGetter: getter,
				Streams: genericiooptions.IOStreams{
					Out:    io.Discard,
					ErrOut: io.Discard,
				},
				Namespace: new(string),
			})
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs(test.args)

			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
			if requests != 0 {
				t.Fatalf("resource requests = %d, want 0", requests)
			}
		})
	}
}

func TestConditionalWriterBuffersOnlyAggregatedOutput(t *testing.T) {
	var delegate bytes.Buffer
	state := &aggregationState{}
	writer := &conditionalWriter{delegate: &delegate, state: state}

	if _, err := writer.Write([]byte("direct")); err != nil {
		t.Fatalf("Write(direct) error = %v", err)
	}
	if got := delegate.String(); got != "direct" {
		t.Fatalf("delegate after direct write = %q, want %q", got, "direct")
	}

	state.used.Store(true)
	if _, err := writer.Write([]byte(" aggregate")); err != nil {
		t.Fatalf("Write(aggregate) error = %v", err)
	}
	if got := delegate.String(); got != "direct" {
		t.Fatalf("delegate before commit = %q, want %q", got, "direct")
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if got := delegate.String(); got != "direct aggregate" {
		t.Fatalf("delegate after commit = %q, want %q", got, "direct aggregate")
	}
}

func TestCommandAdministratorAllNamespacesUsesNativeKubectl(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()
		if r.URL.Path != "/clusters/member/api/v1/pods" {
			return jsonResponse(r, http.StatusNotFound, `{"kind":"Status"}`), nil
		}
		return jsonResponse(r, http.StatusOK, podList("admin", "pod-a")), nil
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{"get", "pods", "-A", "-o", "json"},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := podNames(t, out.Bytes()), []string{"admin/pod-a"}; !equalStrings(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 ||
		strings.Contains(requests[0], "/kapis/tenant.kubesphere.io/") {
		t.Fatalf("requests = %v, want one native Kubernetes request", requests)
	}
}

func TestCommandTenantAllNamespacesFallsBackAndPrintsOneTable(t *testing.T) {
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces: []string{"team-b", "team-a"},
		globalCode: http.StatusForbidden,
		table:      true,
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{"get", "pods", "-A"},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	lines := strings.Fields(out.String())
	if got, want := lines, []string{
		"NAMESPACE", "NAME",
		"team-b", "pod-b",
		"team-a", "pod-a",
	}; !equalStrings(got, want) {
		t.Fatalf("table fields = %v, want %v\n%s", got, want, out.String())
	}
}

func TestCommandWorkspacePrintsJSONFromOnlyWorkspaceNamespaces(t *testing.T) {
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces: []string{"team-a", "team-b"},
		globalCode: http.StatusOK,
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{"get", "pods", "--workspace", "platform", "-o", "json"},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := podNames(t, out.Bytes()), []string{
		"team-a/pod-a",
		"team-b/pod-b",
	}; !equalStrings(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
}

func TestCommandAggregateFailurePrintsNoPartialOutput(t *testing.T) {
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces:    []string{"team-a", "team-b"},
		globalCode:    http.StatusForbidden,
		failNamespace: "team-b",
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{"get", "pods", "-A", "-o", "json"},
	)
	if err == nil || !strings.Contains(err.Error(), `namespace "team-b"`) {
		t.Fatalf("Execute() error = %v, want team-b failure", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestCommandTenantWatchDiscardsBufferedInitialList(t *testing.T) {
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces: []string{"team-a"},
		globalCode: http.StatusForbidden,
		table:      true,
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{"get", "pods", "-A", "--watch"},
	)
	if err == nil || !strings.Contains(err.Error(), "multi-namespace watch is not supported") {
		t.Fatalf("Execute() error = %v, want tenant watch rejection", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestCommandGetsCRDByQualifiedResourceNames(t *testing.T) {
	groupVersion := schema.GroupVersion{Group: "example.io", Version: "v1"}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{groupVersion})
	mapper.Add(
		groupVersion.WithKind("Widget"),
		meta.RESTScopeNamespace,
	)
	discovery := testCachedDiscovery(&metav1.APIResourceList{
		GroupVersion: groupVersion.String(),
		APIResources: []metav1.APIResource{{
			Name:         "widgets",
			SingularName: "widget",
			Namespaced:   true,
			Kind:         "Widget",
		}},
	})

	tests := []struct {
		name     string
		args     []string
		table    bool
		wantName string
	}{
		{
			name:     "group qualified table",
			args:     []string{"get", "widgets.example.io", "-A"},
			table:    true,
			wantName: "widget-a",
		},
		{
			name: "version and group qualified yaml",
			args: []string{
				"get",
				"widgets.v1.example.io",
				"--workspace",
				"platform",
				"-o",
				"yaml",
			},
			wantName: "widget-a",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				mu    sync.Mutex
				paths []string
			)
			transport := roundTripperFunc(func(
				request *http.Request,
			) (*http.Response, error) {
				mu.Lock()
				paths = append(paths, request.URL.Path)
				mu.Unlock()
				switch {
				case strings.Contains(
					request.URL.Path,
					"/kapis/tenant.kubesphere.io/v1beta1/",
				):
					return jsonResponse(
						request,
						http.StatusOK,
						`{"items":[{"metadata":{"name":"team-a"}}]}`,
					), nil
				case request.URL.Path ==
					"/clusters/member/apis/example.io/v1/widgets":
					return jsonResponse(
						request,
						http.StatusForbidden,
						`{"kind":"Status"}`,
					), nil
				case request.URL.Path ==
					"/clusters/member/apis/example.io/v1/namespaces/team-a/widgets":
					if test.table {
						return jsonResponse(
							request,
							http.StatusOK,
							widgetTable("widget-a"),
						), nil
					}
					return jsonResponse(
						request,
						http.StatusOK,
						widgetList("team-a", "widget-a"),
					), nil
				default:
					return jsonResponse(
						request,
						http.StatusNotFound,
						`{"kind":"Status"}`,
					), nil
				}
			})

			out, _, err := executeGenericTenantCommandWithSchema(
				transport,
				test.args,
				mapper,
				discovery,
			)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if test.table {
				if got, want := strings.Fields(out.String()), []string{
					"NAMESPACE", "NAME", "team-a", test.wantName,
				}; !equalStrings(got, want) {
					t.Fatalf("table fields = %v, want %v", got, want)
				}
			} else {
				var document map[string]any
				if err := yaml.Unmarshal(out.Bytes(), &document); err != nil {
					t.Fatalf("yaml.Unmarshal() error = %v", err)
				}
				if document["kind"] != "List" {
					t.Fatalf("kind = %#v, want kubectl List", document["kind"])
				}
				if !strings.Contains(out.String(), "kind: Widget") ||
					!strings.Contains(out.String(), "name: "+test.wantName) {
					t.Fatalf("YAML output missing widget name:\n%s", out.String())
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Contains(
				paths,
				"/clusters/member/apis/example.io/v1/namespaces/team-a/widgets",
			) {
				t.Fatalf("paths = %v, want cluster-prefixed CRD request", paths)
			}
			if !slices.ContainsFunc(paths, func(path string) bool {
				return strings.HasPrefix(
					path,
					"/clusters/member/kapis/tenant.kubesphere.io/",
				)
			}) {
				t.Fatalf("paths = %v, want cluster-prefixed tenant namespace request", paths)
			}
		})
	}
}

func TestCommandPreservesSelectorsForEveryTenantNamespace(t *testing.T) {
	var (
		mu      sync.Mutex
		queries = make(map[string]url.Values)
	)
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces: []string{"team-a", "team-b"},
		globalCode: http.StatusForbidden,
		observe: func(request *http.Request) {
			namespace := namespaceFromPath(request.URL.Path)
			if namespace == "" {
				return
			}
			mu.Lock()
			queries[namespace] = request.URL.Query()
			mu.Unlock()
		},
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{
			"get", "pods", "-A", "-o", "json",
			"-l", "app=web",
			"--field-selector", "status.phase=Running",
		},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := len(podNames(t, out.Bytes())); got != 2 {
		t.Fatalf("items length = %d, want 2", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, namespace := range []string{"team-a", "team-b"} {
		query := queries[namespace]
		if query.Get("labelSelector") != "app=web" {
			t.Fatalf("%s labelSelector = %q", namespace, query.Get("labelSelector"))
		}
		if query.Get("fieldSelector") != "status.phase=Running" {
			t.Fatalf("%s fieldSelector = %q", namespace, query.Get("fieldSelector"))
		}
	}
}

func TestCommandSortsMergedResourcesBeforeCustomColumnsOutput(t *testing.T) {
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces: []string{"team-z", "team-a"},
		globalCode: http.StatusForbidden,
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{
			"get", "pods", "-A",
			"--sort-by=.metadata.name",
			"-o", "custom-columns=NAME:.metadata.name,NS:.metadata.namespace",
		},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := strings.Fields(out.String()), []string{
		"NAME", "NS",
		"pod-a", "team-a",
		"pod-z", "team-z",
	}; !equalStrings(got, want) {
		t.Fatalf("custom columns fields = %v, want %v\n%s", got, want, out.String())
	}
}

func TestCommandWideOutputIncludesPriorityColumns(t *testing.T) {
	transport := newTenantCommandTransport(t, tenantCommandServerOptions{
		namespaces: []string{"team-a"},
		globalCode: http.StatusForbidden,
		wideTable:  true,
	})

	out, _, err := executeGenericTenantCommand(
		transport,
		[]string{"get", "pods", "-A", "-o", "wide"},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := strings.Fields(out.String()), []string{
		"NAMESPACE", "NAME", "DETAIL",
		"team-a", "pod-a", "ready",
	}; !equalStrings(got, want) {
		t.Fatalf("wide fields = %v, want %v\n%s", got, want, out.String())
	}
}

func defaultClientConfig(serverURLs ...string) clientcmd.ClientConfig {
	serverURL := "https://example.test"
	if len(serverURLs) != 0 {
		serverURL = serverURLs[0]
	}
	return clientcmd.NewDefaultClientConfig(
		clientcmdapi.Config{
			CurrentContext: "test",
			Contexts: map[string]*clientcmdapi.Context{
				"test": {
					Cluster:   "test",
					Namespace: "default",
				},
			},
			Clusters: map[string]*clientcmdapi.Cluster{
				"test": {Server: serverURL},
			},
		},
		&clientcmd.ConfigOverrides{},
	)
}

func executeGenericTenantCommand(
	transport http.RoundTripper,
	args []string,
) (*bytes.Buffer, *bytes.Buffer, error) {
	return executeGenericTenantCommandWithSchema(
		transport,
		args,
		testRESTMapper(),
		testCachedDiscovery(),
	)
}

func executeGenericTenantCommandWithSchema(
	transport http.RoundTripper,
	args []string,
	mapper meta.RESTMapper,
	discoveryClient discovery.CachedDiscoveryInterface,
) (*bytes.Buffer, *bytes.Buffer, error) {
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	namespace := "context-default"
	command := NewCommandWithOptions(CommandOptions{
		DisplayName: "ksctl",
		KubeSphereGetter: fakeRESTClientGetter{
			config: &kubesphererest.Config{
				Host:      "https://example.test",
				Transport: transport,
			},
			cluster: "member",
		},
		KubernetesGetter: &transportTestGetter{
			config: &rest.Config{
				Host:      "https://example.test/clusters/member",
				Transport: transport,
			},
			mapper:    mapper,
			loader:    defaultClientConfig("https://example.test/clusters/member"),
			discovery: discoveryClient,
		},
		Streams: genericiooptions.IOStreams{
			Out:    out,
			ErrOut: errOut,
		},
		Namespace: &namespace,
	})
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetOut(out)
	command.SetErr(errOut)
	command.SetArgs(args)
	return out, errOut, command.Execute()
}

func testCachedDiscovery(
	resources ...*metav1.APIResourceList,
) discovery.CachedDiscoveryInterface {
	client := &fakediscovery.FakeDiscovery{Fake: &clientgotesting.Fake{}}
	if len(resources) == 0 {
		resources = []*metav1.APIResourceList{{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "pods", SingularName: "pod", Namespaced: true, Kind: "Pod"},
				{Name: "nodes", SingularName: "node", Namespaced: false, Kind: "Node"},
			},
		}}
	}
	client.Resources = resources
	return memory.NewMemCacheClient(client)
}

type tenantCommandServerOptions struct {
	namespaces    []string
	globalCode    int
	table         bool
	wideTable     bool
	failNamespace string
	observe       func(*http.Request)
}

func newTenantCommandTransport(
	t *testing.T,
	options tenantCommandServerOptions,
) http.RoundTripper {
	t.Helper()
	return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if options.observe != nil {
			options.observe(r)
		}
		switch {
		case r.URL.Path ==
			"/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces",
			r.URL.Path ==
				"/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces":
			objects := make([]string, len(options.namespaces))
			for index, namespace := range options.namespaces {
				objects[index] = `{"metadata":{"name":` + quoteJSON(namespace) + `}}`
			}
			return jsonResponse(
				r,
				http.StatusOK,
				fmt.Sprintf(`{"items":[%s]}`, strings.Join(objects, ",")),
			), nil
		case r.URL.Path == "/clusters/member/api/v1/pods":
			return jsonResponse(r, options.globalCode, `{"kind":"Status"}`), nil
		case strings.Contains(r.URL.Path, "/api/v1/namespaces/"):
			namespace := namespaceFromPath(r.URL.Path)
			if namespace == options.failNamespace {
				return jsonResponse(
					r,
					http.StatusInternalServerError,
					`{"kind":"Status"}`,
				), nil
			}
			if options.table || options.wideTable {
				table := podTable(namespace, "pod-"+strings.TrimPrefix(namespace, "team-"))
				if options.wideTable {
					table = podWideTable(
						namespace,
						"pod-"+strings.TrimPrefix(namespace, "team-"),
					)
				}
				return jsonResponse(
					r,
					http.StatusOK,
					table,
				), nil
			}
			return jsonResponse(
				r,
				http.StatusOK,
				podList(namespace, "pod-"+strings.TrimPrefix(namespace, "team-")),
			), nil
		default:
			return jsonResponse(r, http.StatusNotFound, `{"kind":"Status"}`), nil
		}
	})
}

func podTable(namespace, name string) string {
	return `{
		"apiVersion":"meta.k8s.io/v1",
		"kind":"Table",
		"metadata":{"resourceVersion":"1"},
		"columnDefinitions":[{"name":"Name","type":"string","priority":0}],
		"rows":[{"cells":[` + quoteJSON(name) + `]}]
	}`
}

func podWideTable(namespace, name string) string {
	return `{
		"apiVersion":"meta.k8s.io/v1",
		"kind":"Table",
		"metadata":{"resourceVersion":"1"},
		"columnDefinitions":[
			{"name":"Name","type":"string","priority":0},
			{"name":"Detail","type":"string","priority":1}
		],
		"rows":[{"cells":[` + quoteJSON(name) + `,"ready"]}]
	}`
}

func widgetList(namespace, name string) string {
	return `{
		"apiVersion":"example.io/v1",
		"kind":"WidgetList",
		"metadata":{"resourceVersion":"1"},
		"items":[{"apiVersion":"example.io/v1","kind":"Widget","metadata":{
			"namespace":` + quoteJSON(namespace) + `,
			"name":` + quoteJSON(name) + `
		}}]
	}`
}

func widgetTable(name string) string {
	return `{
		"apiVersion":"meta.k8s.io/v1",
		"kind":"Table",
		"metadata":{"resourceVersion":"1"},
		"columnDefinitions":[{"name":"Name","type":"string","priority":0}],
		"rows":[{"cells":[` + quoteJSON(name) + `]}]
	}`
}

func findCommand(command interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	for _, child := range command.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

type fakeRESTClientGetter struct {
	config  *kubesphererest.Config
	cluster string
	err     error
}

type fakeKubernetesRESTClientGetter struct{}

func (fakeKubernetesRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return nil, nil
}

func (fakeKubernetesRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return nil, nil
}

func (fakeKubernetesRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return nil, nil
}

func (fakeKubernetesRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return nil
}

func (g fakeRESTClientGetter) ToRESTConfig() (*kubesphererest.Config, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.config, nil
}

func (g fakeRESTClientGetter) KubeSphereCluster() (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.cluster, nil
}
