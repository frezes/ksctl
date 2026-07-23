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
			name:     "workspace template list ignores invalid cluster",
			request:  Request{Resource: ResourceWorkspace, Cluster: "team/member"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
			response: `{"items":[{"metadata":{"name":"platform"}}],"total_count":1}`,
			wantList: true,
		},
		{
			name:     "named workspace template ignores invalid cluster",
			request:  Request{Resource: ResourceWorkspace, Name: "platform", Cluster: "team/member"},
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
			name:     "cluster list ignores invalid cluster",
			request:  Request{Resource: ResourceCluster, Cluster: "team/member"},
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantList: true,
		},
		{
			name:     "workspace cluster list ignores invalid cluster",
			request:  Request{Resource: ResourceCluster, Workspace: "platform", Cluster: "team/member"},
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
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()

			restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(
				&kubesphererest.Config{Host: server.URL, BearerToken: "secret"},
			)
			if err != nil {
				t.Fatalf("ForConfig() error = %v", err)
			}
			response, err := NewClient(restClient).Get(context.Background(), test.request)
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
		name    string
		request Request
		want    string
	}{
		{name: "unknown resource", request: Request{Resource: "widget"}, want: "unsupported tenant resource"},
		{name: "workspace scope on workspace", request: Request{Resource: ResourceWorkspace, Workspace: "platform"}, want: "workspace scope is not supported"},
		{name: "name on namespace", request: Request{Resource: ResourceNamespace, Name: "demo"}, want: "resource name is only supported"},
		{name: "invalid name", request: Request{Resource: ResourceWorkspace, Name: "team/member"}, want: "invalid workspace name"},
		{name: "invalid workspace", request: Request{Resource: ResourceNamespace, Workspace: ".."}, want: "invalid workspace"},
		{name: "invalid cluster", request: Request{Resource: ResourceNamespace, Cluster: "team/member"}, want: "invalid cluster"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClient(restClient).Get(context.Background(), test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Get() error = %v, want %q", err, test.want)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestClientGetRejectsMalformedResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{name: "invalid JSON", response: `{`, want: "decode tenant workspace response"},
		{name: "missing items", response: `{"totalItems":0}`, want: `missing "items" array`},
		{name: "non-object item", response: `{"items":["bad"]}`, want: "item 0 is not an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()
			restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(
				&kubesphererest.Config{Host: server.URL},
			)
			if err != nil {
				t.Fatalf("ForConfig() error = %v", err)
			}
			_, err = NewClient(restClient).Get(context.Background(), Request{Resource: ResourceWorkspace})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Get() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestClientGetRejectsNilRESTClient(t *testing.T) {
	_, err := NewClient(nil).Get(context.Background(), Request{Resource: ResourceWorkspace})
	if err == nil || !strings.Contains(err.Error(), "REST client is required") {
		t.Fatalf("Get() error = %v, want REST client is required", err)
	}
}
