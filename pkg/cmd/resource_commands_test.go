package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNativeGetThroughKSApiServer(t *testing.T) {
	server := newFakeKSApiServer(t)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"get", "workspaces",
		"--endpoint", server.URL,
		"--token", "secret",
		"--no-interactive",
		"-o", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{`"kind": "List"`, `"kind": "Workspace"`, `"name": "demo"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("get output missing %q:\n%s", want, out.String())
		}
	}
}

func TestNativeGetResolvesShortNameWithCoreDiscoveryFallback(t *testing.T) {
	server := newFallbackDiscoveryKSApiServer(t)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"get", "po", "--all-namespaces",
		"--endpoint", server.URL,
		"--token", "secret",
		"--no-interactive",
		"-o", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "demo-pod"`) {
		t.Fatalf("get po output missing pod:\n%s", out.String())
	}
}

func TestNativeGetResolvesCustomResourceWithCoreDiscoveryFallback(t *testing.T) {
	server := newFallbackDiscoveryKSApiServer(t)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"get", "widgets.example.io",
		"--endpoint", server.URL,
		"--token", "secret",
		"--no-interactive",
		"-o", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "demo-widget"`) {
		t.Fatalf("get custom resource output missing widget:\n%s", out.String())
	}
}

func TestNativeGetResolvesBuiltInResourceWithCoreDiscoveryFallback(t *testing.T) {
	server := newFallbackDiscoveryKSApiServer(t)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"get", "deploy", "--all-namespaces",
		"--endpoint", server.URL,
		"--token", "secret",
		"--no-interactive",
		"-o", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "demo-deployment"`) {
		t.Fatalf("get deploy output missing deployment:\n%s", out.String())
	}
}

func TestNativeGetThroughSpecifiedCluster(t *testing.T) {
	server := newClusterScopedCoreAPIServer(t, "host")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"get", "po", "--all-namespaces",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "host",
		"--no-interactive",
		"-o", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "demo-pod"`) {
		t.Fatalf("get output missing cluster pod:\n%s", out.String())
	}
}

func TestNativeGetCoreResourceThroughClusterWhenScopedDiscoveryFails(t *testing.T) {
	var lock sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		paths = append(paths, r.URL.Path)
		lock.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/clusters/host/api":
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
				Versions: []string{"v1"},
			})
		case "/clusters/host/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
				Groups: []metav1.APIGroup{{
					Name: "apps",
					Versions: []metav1.GroupVersionForDiscovery{{
						GroupVersion: "apps/v1",
						Version:      "v1",
					}},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: "apps/v1",
						Version:      "v1",
					},
				}},
			})
		case "/clusters/host/api/v1":
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		case "/api/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name:         "nodes",
					SingularName: "node",
					Namespaced:   false,
					Kind:         "Node",
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			})
		case "/clusters/host/apis/apps/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{GroupVersion: "apps/v1"})
		case "/clusters/host/api/v1/nodes":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "NodeList",
				"items": []any{map[string]any{
					"apiVersion": "v1",
					"kind":       "Node",
					"metadata":   map[string]any{"name": "host-node"},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"get", "node",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "host",
		"--no-interactive",
		"-o", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "host-node"`) {
		t.Fatalf("get node output missing cluster node:\n%s", out.String())
	}
	lock.Lock()
	defer lock.Unlock()
	for _, want := range []string{
		"/clusters/host/api/v1",
		"/api/v1",
		"/clusters/host/apis/apps/v1",
		"/clusters/host/api/v1/nodes",
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("request paths = %v, want %q", paths, want)
		}
	}
	for _, path := range paths {
		if path != "/api/v1" && !strings.HasPrefix(path, "/clusters/host/") {
			t.Errorf("request path %q is unexpectedly unscoped", path)
		}
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
	cmd.SetArgs([]string{
		"describe", "pod/demo-pod",
		"--namespace", "default",
		"--token", "secret",
		"--show-events=false",
		"--no-interactive",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "demo-pod") {
		t.Fatalf("describe output missing cluster pod:\n%s", out.String())
	}
}

func TestNativeDescribeThroughKSApiServer(t *testing.T) {
	server := newFakeKSApiServer(t)
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{
		"describe", "workspace", "demo",
		"--endpoint", server.URL,
		"--token", "secret",
		"--no-interactive",
		"--show-events=false",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"Name:", "demo", "Labels:", "Owner:", "platform"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("describe output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRootRefreshesExpiredCacheBeforeResourceRequest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)

	var lock sync.Mutex
	refreshRequests := 0
	kubernetesRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "refresh_token" {
				t.Errorf("grant_type = %q, want refresh_token", got)
			}
			if got := r.Form.Get("refresh_token"); got != "expired-refresh-token" {
				t.Errorf("refresh_token = %q, want expired-refresh-token", got)
			}
			lock.Lock()
			refreshRequests++
			lock.Unlock()
			writeAPIJSON(t, w, map[string]any{
				"access_token":  "refreshed-token",
				"refresh_token": "new-refresh-token",
				"token_type":    "bearer",
				"expires_in":    3600,
			})
			return
		}

		if got := r.Header.Get("Authorization"); got != "Bearer refreshed-token" {
			t.Errorf("Authorization = %q, want refreshed token", got)
		}
		lock.Lock()
		kubernetesRequests++
		lock.Unlock()
		switch r.URL.Path {
		case "/clusters/host/api":
			writeAPIJSON(t, w, metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}})
		case "/clusters/host/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
				Groups: []metav1.APIGroup{{
					Name: "tenant.kubesphere.io",
					Versions: []metav1.GroupVersionForDiscovery{{
						GroupVersion: "tenant.kubesphere.io/v1beta1",
						Version:      "v1beta1",
					}},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: "tenant.kubesphere.io/v1beta1",
						Version:      "v1beta1",
					},
				}},
			})
		case "/clusters/host/apis/tenant.kubesphere.io/v1beta1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "tenant.kubesphere.io/v1beta1",
				APIResources: []metav1.APIResource{{
					Name:         "workspaces",
					SingularName: "workspace",
					Namespaced:   false,
					Kind:         "Workspace",
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			})
		case "/clusters/host/apis/tenant.kubesphere.io/v1beta1/workspaces":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "tenant.kubesphere.io/v1beta1",
				"kind":       "WorkspaceList",
				"items":      []any{workspaceObject()},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Clusters["local"] = config.Cluster{Host: server.URL}
	cfg.Users["admin"] = config.User{Username: "admin"}
	cfg.Contexts["local"] = config.Context{Cluster: "local", User: "admin", DefaultCluster: "host"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save config error = %v", err)
	}
	cacheDir := filepath.Join(home, ".ksctl", "cache", "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, time.Now().Add(-time.Hour))); err != nil {
		t.Fatalf("Save token cache error = %v", err)
	}

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"get", "workspaces", "--no-interactive", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `"name": "demo"`) {
		t.Fatalf("get output missing refreshed request result:\n%s", out.String())
	}
	entry, err := tokencache.Load(cacheDir, "local")
	if err != nil {
		t.Fatalf("Load token cache error = %v", err)
	}
	if entry.AccessToken != "refreshed-token" || entry.RefreshToken != "new-refresh-token" {
		t.Fatalf("refreshed cache = %#v", entry)
	}
	lock.Lock()
	defer lock.Unlock()
	if refreshRequests != 1 || kubernetesRequests == 0 {
		t.Fatalf("refresh requests = %d, Kubernetes requests = %d", refreshRequests, kubernetesRequests)
	}
}

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
					Verbs:        metav1.Verbs{"get", "list"},
					ShortNames:   []string{"po"},
				}},
			})
		case "/api/v1/pods":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "PodList",
				"items":      []any{podObject()},
			})
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
		"metadata": map[string]any{
			"name":      "demo-pod",
			"namespace": "default",
		},
		"spec": map[string]any{
			"containers": []any{map[string]any{
				"name":  "demo",
				"image": "nginx:latest",
			}},
		},
		"status": map[string]any{"phase": "Running"},
	}
}

func newFakeKSApiServer(t *testing.T) *httptest.Server {
	t.Helper()

	var lock sync.Mutex
	var paths []string
	t.Cleanup(func() {
		lock.Lock()
		defer lock.Unlock()
		for _, path := range paths {
			if path != "/api" && path != "/apis" && !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/apis/") {
				t.Errorf("request used non-Kubernetes API path %q", path)
			}
		}
	})

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		paths = append(paths, r.URL.Path)
		lock.Unlock()

		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api":
			writeAPIJSON(t, w, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"},
			})
		case "/apis":
			writeAPIJSON(t, w, metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
				Groups: []metav1.APIGroup{{
					Name: "tenant.kubesphere.io",
					Versions: []metav1.GroupVersionForDiscovery{{
						GroupVersion: "tenant.kubesphere.io/v1beta1",
						Version:      "v1beta1",
					}},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: "tenant.kubesphere.io/v1beta1",
						Version:      "v1beta1",
					},
				}},
			})
		case "/apis/tenant.kubesphere.io/v1beta1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "tenant.kubesphere.io/v1beta1",
				APIResources: []metav1.APIResource{{
					Name:         "workspaces",
					SingularName: "workspace",
					Namespaced:   false,
					Kind:         "Workspace",
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			})
		case "/apis/tenant.kubesphere.io/v1beta1/workspaces":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "tenant.kubesphere.io/v1beta1",
				"kind":       "WorkspaceList",
				"metadata":   map[string]any{"resourceVersion": "1"},
				"items":      []any{workspaceObject()},
			})
		case "/apis/tenant.kubesphere.io/v1beta1/workspaces/demo":
			writeAPIJSON(t, w, workspaceObject())
		default:
			http.NotFound(w, r)
		}
	}))
}

func newFallbackDiscoveryKSApiServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>KubeSphere</title>"))
		case "/api/v1":
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
		case "/apis":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>KubeSphere</title>"))
		case "/apis/apiextensions.k8s.io/v1/customresourcedefinitions":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinitionList",
				"metadata":   map[string]any{},
				"items": []any{map[string]any{
					"metadata": map[string]any{"name": "widgets.example.io"},
					"spec": map[string]any{
						"group": "example.io",
						"names": map[string]any{
							"plural":     "widgets",
							"singular":   "widget",
							"kind":       "Widget",
							"listKind":   "WidgetList",
							"shortNames": []string{"wd"},
						},
						"scope": "Cluster",
						"versions": []any{map[string]any{
							"name":    "v1",
							"served":  true,
							"storage": true,
						}},
					},
				}},
			})
		case "/apis/apiextensions.k8s.io/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "apiextensions.k8s.io/v1",
				APIResources: []metav1.APIResource{{
					Name:         "customresourcedefinitions",
					SingularName: "customresourcedefinition",
					Namespaced:   false,
					Kind:         "CustomResourceDefinition",
					Verbs:        metav1.Verbs{"get", "list"},
					ShortNames:   []string{"crd", "crds"},
				}},
			})
		case "/apis/apps/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "apps/v1",
				APIResources: []metav1.APIResource{{
					Name:         "deployments",
					SingularName: "deployment",
					Namespaced:   true,
					Kind:         "Deployment",
					Verbs:        metav1.Verbs{"get", "list"},
					ShortNames:   []string{"deploy"},
				}},
			})
		case "/apis/example.io/v1":
			writeAPIJSON(t, w, metav1.APIResourceList{
				GroupVersion: "example.io/v1",
				APIResources: []metav1.APIResource{{
					Name:         "widgets",
					SingularName: "widget",
					Namespaced:   false,
					Kind:         "Widget",
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			})
		case "/api/v1/pods":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "PodList",
				"items": []any{map[string]any{
					"apiVersion": "v1",
					"kind":       "Pod",
					"metadata": map[string]any{
						"name":      "demo-pod",
						"namespace": "default",
					},
				}},
			})
		case "/apis/example.io/v1/widgets":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "example.io/v1",
				"kind":       "WidgetList",
				"items": []any{map[string]any{
					"apiVersion": "example.io/v1",
					"kind":       "Widget",
					"metadata": map[string]any{
						"name": "demo-widget",
					},
				}},
			})
		case "/apis/apps/v1/deployments":
			writeAPIJSON(t, w, map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "DeploymentList",
				"items": []any{map[string]any{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]any{
						"name":      "demo-deployment",
						"namespace": "default",
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func workspaceObject() map[string]any {
	return map[string]any{
		"apiVersion": "tenant.kubesphere.io/v1beta1",
		"kind":       "Workspace",
		"metadata": map[string]any{
			"name":   "demo",
			"labels": map[string]any{"environment": "test"},
		},
		"spec": map[string]any{"owner": "platform"},
	}
}

func writeAPIJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("Encode() error = %v", err)
	}
}
