package tenant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	kubesphererest "kubesphere.io/client-go/rest"
)

func TestNamespaceResolverUsesClusterAndWorkspaceScope(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got, want := r.URL.Path, "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[
			{"metadata":{"name":"team-b"}},
			{"metadata":{"name":"team-a"}},
			{"metadata":{"name":"team-b"}}
		]}`)
	}))
	defer server.Close()

	resolver := newNamespaceResolver(fakeRESTClientGetter{
		config: &kubesphererest.Config{
			Host:        server.URL,
			BearerToken: "secret",
		},
		cluster: "member",
	})
	got, err := resolver.Namespaces(context.Background(), "platform")
	if err != nil {
		t.Fatalf("Namespaces() error = %v", err)
	}
	want := []string{"team-b", "team-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Namespaces() = %v, want %v", got, want)
	}

	got[0] = "mutated"
	again, err := resolver.Namespaces(context.Background(), "platform")
	if err != nil {
		t.Fatalf("cached Namespaces() error = %v", err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("cached Namespaces() = %v, want independent %v", again, want)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one cached request", requests)
	}
}

func TestNamespaceResolverCachesScopesSeparately(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[]}`)
	}))
	defer server.Close()

	resolver := newNamespaceResolver(fakeRESTClientGetter{
		config:  &kubesphererest.Config{Host: server.URL},
		cluster: "member",
	})
	for _, workspace := range []string{"", "platform", "", "platform"} {
		if _, err := resolver.Namespaces(context.Background(), workspace); err != nil {
			t.Fatalf("Namespaces(%q) error = %v", workspace, err)
		}
	}

	want := []string{
		"/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
		"/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestNamespaceResolverRejectsMissingOrInvalidName(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "missing name",
			response: `{"items":[{"metadata":{}}]}`,
			want:     "missing metadata.name",
		},
		{
			name:     "invalid name",
			response: `{"items":[{"metadata":{"name":"team/member"}}]}`,
			want:     `invalid namespace "team/member"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()

			resolver := newNamespaceResolver(fakeRESTClientGetter{
				config: &kubesphererest.Config{Host: server.URL},
			})
			_, err := resolver.Namespaces(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Namespaces() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNamespaceResolverCachesErrors(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	resolver := newNamespaceResolver(fakeRESTClientGetter{
		config: &kubesphererest.Config{Host: server.URL},
	})
	for range 2 {
		_, err := resolver.Namespaces(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "resolve tenant namespaces") {
			t.Fatalf("Namespaces() error = %v, want resolver context", err)
		}
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want cached error from one request", requests)
	}
}
