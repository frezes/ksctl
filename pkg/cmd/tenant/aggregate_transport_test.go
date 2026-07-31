package tenant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestAggregatingTransportKeepsSuccessfulClusterAdminRequest(t *testing.T) {
	var requests []string
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.RequestURI())
		return jsonResponse(request, http.StatusOK, podList("admin", "pod-a")), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"tenant-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got, want := requests, []string{
		"/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
	}; !equalStrings(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
	if state.used.Load() {
		t.Fatal("aggregation state used = true, want false")
	}
}

func TestAggregatingTransportFallsBackAfterForbidden(t *testing.T) {
	var requests []string
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.RequestURI())
		switch request.URL.Path {
		case "/proxy/clusters/member/api/v1/pods":
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		case "/proxy/clusters/member/api/v1/namespaces/team-b/pods":
			return jsonResponse(request, http.StatusOK, podList("team-b", "pod-b")), nil
		case "/proxy/clusters/member/api/v1/namespaces/team-a/pods":
			return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
		default:
			t.Fatalf("unexpected request path %q", request.URL.Path)
			return nil, nil
		}
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-b", "team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("items length = %d, want 2", len(list.Items))
	}
	if got, want := []string{
		list.Items[0].Metadata.Namespace + "/" + list.Items[0].Metadata.Name,
		list.Items[1].Metadata.Namespace + "/" + list.Items[1].Metadata.Name,
	}, []string{"team-b/pod-b", "team-a/pod-a"}; !equalStrings(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if got, want := requests, []string{
		"/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
		"/proxy/clusters/member/api/v1/namespaces/team-b/pods?labelSelector=app%3Dweb",
		"/proxy/clusters/member/api/v1/namespaces/team-a/pods?labelSelector=app%3Dweb",
	}; !equalStrings(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if resolver.calls != 1 || resolver.workspace != "" {
		t.Fatalf(
			"resolver = calls %d workspace %q, want calls 1 workspace empty",
			resolver.calls,
			resolver.workspace,
		)
	}
	if !state.used.Load() {
		t.Fatal("aggregation state used = false, want true")
	}
}

func TestAggregatingTransportWorkspaceSkipsGlobalRequest(t *testing.T) {
	var requests []string
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.RequestURI())
		return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateWorkspace, workspace: "workspace-a"}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?fieldSelector=status.phase%3DRunning",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if got, want := requests, []string{
		"/proxy/clusters/member/api/v1/namespaces/team-a/pods?fieldSelector=status.phase%3DRunning",
	}; !equalStrings(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if resolver.calls != 1 || resolver.workspace != "workspace-a" {
		t.Fatalf(
			"resolver = calls %d workspace %q, want calls 1 workspace %q",
			resolver.calls,
			resolver.workspace,
			"workspace-a",
		)
	}
	if !state.used.Load() {
		t.Fatal("aggregation state used = false, want true")
	}
}

func TestAggregatingTransportKeepsClusterScopedAndNamespacedRequestsDirect(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{
			name: "cluster scoped",
			url:  "https://example.test/proxy/clusters/member/api/v1/nodes",
		},
		{
			name: "explicit namespace",
			url: "https://example.test/proxy/clusters/member/api/v1/" +
				"namespaces/team-a/pods",
		},
		{
			name: "named resource",
			url:  "https://example.test/proxy/clusters/member/api/v1/pods/pod-a",
		},
		{
			name: "discovery",
			url:  "https://example.test/proxy/clusters/member/api/v1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests []string
			base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				requests = append(requests, request.URL.RequestURI())
				return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
			})
			resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
			state := &aggregationState{mode: aggregateOnForbidden}
			client := newAggregatingTestClient(t, base, resolver, state)

			response, err := client.Get(test.url)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %v, want one direct request", requests)
			}
			if resolver.calls != 0 {
				t.Fatalf("resolver calls = %d, want 0", resolver.calls)
			}
			if state.used.Load() {
				t.Fatal("aggregation state used = true, want false")
			}
		})
	}
}

func TestAggregatingTransportDoesNotFallbackForNonForbiddenErrors(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusInternalServerError, `{"kind":"Status"}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get("https://example.test/proxy/clusters/member/api/v1/pods")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf(
			"status = %d, want %d",
			response.StatusCode,
			http.StatusInternalServerError,
		)
	}
	if requests != 1 || resolver.calls != 0 || state.used.Load() {
		t.Fatalf(
			"requests = %d, resolver calls = %d, used = %t; want 1, 0, false",
			requests,
			resolver.calls,
			state.used.Load(),
		)
	}
}

func TestAggregatingTransportLeavesNonGetRequestAlone(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Post(
		"https://example.test/proxy/clusters/member/api/v1/pods",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	if requests != 1 || resolver.calls != 0 || state.used.Load() {
		t.Fatalf(
			"requests = %d, resolver calls = %d, used = %t; want 1, 0, false",
			requests,
			resolver.calls,
			state.used.Load(),
		)
	}
}

func TestAggregatingTransportRejectsWorkspaceRootResource(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusOK, `{"kind":"NodeList","items":[]}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateWorkspace, workspace: "platform"}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/nodes",
	)
	if err == nil || !strings.Contains(err.Error(), "cluster-scoped") {
		t.Fatalf("Get() error = %v, want cluster-scoped workspace error", err)
	}
	if response != nil {
		response.Body.Close()
		t.Fatalf("Get() response = %#v, want nil", response)
	}
	if requests != 0 || resolver.calls != 0 {
		t.Fatalf(
			"requests = %d, resolver calls = %d, want 0, 0",
			requests,
			resolver.calls,
		)
	}
}

func TestAggregatingTransportFailsWholeRequestWhenNamespaceFails(t *testing.T) {
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/proxy/clusters/member/api/v1/pods":
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		case strings.Contains(request.URL.Path, "/team-a/"):
			return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
		default:
			return jsonResponse(request, http.StatusInternalServerError, `{"kind":"Status"}`), nil
		}
	})
	client := newAggregatingTestClient(
		t,
		base,
		&fakeNamespaceResolver{namespaces: []string{"team-a", "team-b"}},
		&aggregationState{mode: aggregateOnForbidden},
	)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods",
	)
	if err == nil || !strings.Contains(err.Error(), `namespace "team-b"`) {
		t.Fatalf("Get() error = %v, want namespace failure", err)
	}
	if response != nil {
		response.Body.Close()
		t.Fatalf("Get() response = %#v, want nil", response)
	}
}

func TestAggregatingRESTClientGetterDelegatesDiscoveryMapperAndLoader(t *testing.T) {
	mapper := testRESTMapper()
	loader := clientcmd.NewDefaultClientConfig(
		clientcmdapi.Config{},
		&clientcmd.ConfigOverrides{},
	)
	delegate := &transportTestGetter{
		config: &rest.Config{Host: "https://example.test"},
		mapper: mapper,
		loader: loader,
	}
	getter := newAggregatingRESTClientGetter(
		delegate,
		&fakeNamespaceResolver{},
		&aggregationState{},
	)

	if got, err := getter.ToDiscoveryClient(); err != nil || got != nil {
		t.Fatalf("ToDiscoveryClient() = %#v, %v, want nil, nil", got, err)
	}
	if got, err := getter.ToRESTMapper(); err != nil || got != mapper {
		t.Fatalf("ToRESTMapper() = %#v, %v, want mapper, nil", got, err)
	}
	if got := getter.ToRawKubeConfigLoader(); got != loader {
		t.Fatalf("ToRawKubeConfigLoader() = %#v, want loader", got)
	}
}

func newAggregatingTestClient(
	t *testing.T,
	base http.RoundTripper,
	resolver namespaceResolver,
	state *aggregationState,
) *http.Client {
	t.Helper()
	getter := newAggregatingRESTClientGetter(
		&transportTestGetter{
			config: &rest.Config{
				Host:      "https://example.test/proxy/clusters/member",
				Transport: base,
			},
			mapper: testRESTMapper(),
		},
		resolver,
		state,
	)
	config, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		t.Fatalf("HTTPClientFor() error = %v", err)
	}
	return client
}

func testRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(
		schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
		meta.RESTScopeNamespace,
	)
	mapper.Add(
		schema.GroupVersionKind{Version: "v1", Kind: "Node"},
		meta.RESTScopeRoot,
	)
	return mapper
}

type fakeNamespaceResolver struct {
	namespaces []string
	err        error
	calls      int
	workspace  string
}

func (r *fakeNamespaceResolver) Namespaces(
	_ context.Context,
	workspace string,
) ([]string, error) {
	r.calls++
	r.workspace = workspace
	return r.namespaces, r.err
}

type transportTestGetter struct {
	config *rest.Config
	mapper meta.RESTMapper
	loader clientcmd.ClientConfig
	err    error
}

func (g *transportTestGetter) ToRESTConfig() (*rest.Config, error) {
	if g.err != nil {
		return nil, g.err
	}
	return rest.CopyConfig(g.config), nil
}

func (*transportTestGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return nil, nil
}

func (g *transportTestGetter) ToRESTMapper() (meta.RESTMapper, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.mapper, nil
}

func (g *transportTestGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return g.loader
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(
	request *http.Request,
	status int,
	body string,
) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: request,
	}
}

func podList(namespace, name string) string {
	return `{
		"apiVersion":"v1",
		"kind":"PodList",
		"metadata":{"continue":"","resourceVersion":"1"},
		"items":[{"apiVersion":"v1","kind":"Pod","metadata":{
			"namespace":` + quoteJSON(namespace) + `,
			"name":` + quoteJSON(name) + `
		}}]
	}`
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
