package kubernetes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	"github.com/kubesphere/ksctl/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestRESTClientGetterBuildsKubeSphereConfig(t *testing.T) {
	getter := NewRESTClientGetter(&Options{
		Endpoint:              "https://ks.example.com",
		Token:                 "secret",
		Namespace:             "demo",
		RequestTimeout:        "15s",
		InsecureSkipTLSVerify: true,
		NoInteractive:         true,
		UserAgent:             "ksctl/test",
	}, Dependencies{})

	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	if restConfig.Host != "https://ks.example.com" {
		t.Fatalf("Host = %q", restConfig.Host)
	}
	if restConfig.BearerToken != "secret" {
		t.Fatalf("BearerToken = %q", restConfig.BearerToken)
	}
	if !restConfig.Insecure {
		t.Fatal("Insecure = false, want true")
	}
	if restConfig.UserAgent != "ksctl/test" {
		t.Fatalf("UserAgent = %q", restConfig.UserAgent)
	}
	if restConfig.Timeout != 15*time.Second {
		t.Fatalf("Timeout = %v", restConfig.Timeout)
	}

	namespace, explicit, err := getter.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		t.Fatalf("Namespace() error = %v", err)
	}
	if namespace != "demo" || !explicit {
		t.Fatalf("Namespace() = %q, %v", namespace, explicit)
	}
}

func TestRESTClientGetterMapsConfigTLSClientConfig(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	cfg.Fleets["prod"] = config.Fleet{
		Host: "https://ks.example.com",
		TLSClientConfig: config.TLSClientConfig{
			Insecure:   true,
			ServerName: "ks.example.com",
			CAFile:     "/tmp/ca.crt",
			CAData:     "ca-data",
		},
		Users: map[string]config.User{"admin": {BearerToken: "secret"}},
	}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	provider := auth.NewProvider(auth.ProviderOptions{CacheDir: filepath.Join(t.TempDir(), "tokens")})
	getter := NewRESTClientGetter(&Options{
		ConfigPath:    path,
		NoInteractive: true,
	}, Dependencies{TokenProvider: provider})

	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	if restConfig.Host != "https://ks.example.com" || restConfig.BearerToken != "secret" {
		t.Fatalf("restConfig = %#v", restConfig)
	}
	if !restConfig.Insecure || restConfig.ServerName != "ks.example.com" || restConfig.CAFile != "/tmp/ca.crt" || string(restConfig.CAData) != "ca-data" {
		t.Fatalf("TLSClientConfig = %#v", restConfig.TLSClientConfig)
	}
}

func TestRESTClientGetterReturnsResolvedKubeSphereCluster(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	cfg.Fleets["prod"] = config.Fleet{Host: "https://ks.example.com", Users: map[string]config.User{"admin": {BearerToken: "secret"}}}
	cfg.Contexts["prod"] = config.Context{
		Fleet:          "prod",
		User:           "admin",
		DefaultCluster: "member-from-context",
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, test := range []struct {
		name        string
		clusterFlag string
		want        string
	}{
		{name: "context default", want: "member-from-context"},
		{name: "flag override", clusterFlag: "member-from-flag", want: "member-from-flag"},
	} {
		t.Run(test.name, func(t *testing.T) {
			getter := NewRESTClientGetter(&Options{
				ConfigPath:    path,
				Cluster:       test.clusterFlag,
				NoInteractive: true,
			}, Dependencies{})

			got, err := getter.KubeSphereCluster()
			if err != nil {
				t.Fatalf("KubeSphereCluster() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("KubeSphereCluster() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRESTClientGetterScopesClientConfigsToResolvedCluster(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	cfg.Fleets["prod"] = config.Fleet{Host: "https://ks.example.com/proxy/", Users: map[string]config.User{"admin": {BearerToken: "secret"}}}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin", DefaultCluster: "context-member"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, test := range []struct {
		name        string
		clusterFlag string
		want        string
	}{
		{name: "context default", want: "https://ks.example.com/proxy/clusters/context-member"},
		{name: "flag override", clusterFlag: "flag-member", want: "https://ks.example.com/proxy/clusters/flag-member"},
	} {
		t.Run(test.name, func(t *testing.T) {
			getter := NewRESTClientGetter(&Options{
				ConfigPath:    configPath,
				Token:         "secret",
				Cluster:       test.clusterFlag,
				NoInteractive: true,
			}, Dependencies{})
			restConfig, err := getter.ToRESTConfig()
			if err != nil {
				t.Fatalf("ToRESTConfig() error = %v", err)
			}
			if restConfig.Host != test.want {
				t.Fatalf("Host = %q, want %q", restConfig.Host, test.want)
			}
			rawConfig, err := getter.ToRawKubeConfigLoader().RawConfig()
			if err != nil {
				t.Fatalf("RawConfig() error = %v", err)
			}
			if got := rawConfig.Clusters[clientConfigName].Server; got != test.want {
				t.Fatalf("raw cluster server = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRESTClientGetterRejectsInvalidClusterPathSegment(t *testing.T) {
	for _, cluster := range []string{"..", "team/member", "team%2Fmember"} {
		t.Run(cluster, func(t *testing.T) {
			getter := NewRESTClientGetter(&Options{
				Endpoint:      "https://ks.example.com/proxy",
				Token:         "secret",
				Cluster:       cluster,
				NoInteractive: true,
			}, Dependencies{})

			_, err := getter.ToRESTConfig()
			if err == nil || !strings.Contains(err.Error(), "invalid cluster") {
				t.Fatalf("ToRESTConfig() error = %v, want invalid cluster error", err)
			}
		})
	}
}

func TestRESTClientGetterRejectsInvalidClusterBeforeResolvingToken(t *testing.T) {
	for _, test := range []struct {
		name      string
		options   Options
		configure func(*config.Config)
	}{
		{
			name: "explicit cluster",
			options: Options{
				Endpoint: "https://ks.example.com",
				Cluster:  "team/member",
			},
		},
		{
			name: "context default cluster",
			configure: func(cfg *config.Config) {
				cfg.CurrentContext = "local"
				cfg.Fleets["local"] = config.Fleet{
					Host:  "https://ks.example.com",
					Users: map[string]config.User{"admin": {}},
				}
				cfg.Contexts["local"] = config.Context{
					Fleet:          "local",
					User:           "admin",
					DefaultCluster: "team/member",
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := config.New()
			if test.configure != nil {
				test.configure(cfg)
			}
			if err := config.Save(path, cfg); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			test.options.ConfigPath = path

			provider := &recordingTokenProvider{}
			getter := NewRESTClientGetter(&test.options, Dependencies{TokenProvider: provider})
			_, err := getter.ToRESTConfig()
			if err == nil || !strings.Contains(err.Error(), "invalid cluster") {
				t.Fatalf("ToRESTConfig() error = %v, want invalid cluster", err)
			}
			if provider.calls != 0 {
				t.Fatalf("Token() calls = %d, want 0", provider.calls)
			}
		})
	}
}

func TestRESTClientGetterCachesDiscoveryAndPreservesAPIPaths(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			writeJSON(t, w, metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{"v1"}})
		case "/apis":
			writeJSON(t, w, metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}})
		case "/api/v1":
			writeJSON(t, w, metav1.APIResourceList{GroupVersion: "v1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	getter := NewRESTClientGetter(&Options{
		Endpoint:      server.URL,
		Token:         "secret",
		NoInteractive: true,
	}, Dependencies{})

	first, err := getter.ToDiscoveryClient()
	if err != nil {
		t.Fatalf("ToDiscoveryClient() error = %v", err)
	}
	second, err := getter.ToDiscoveryClient()
	if err != nil {
		t.Fatalf("ToDiscoveryClient() second error = %v", err)
	}
	if first != second {
		t.Fatal("ToDiscoveryClient() did not return the cached client")
	}
	if _, err := first.ServerGroups(); err != nil {
		t.Fatalf("ServerGroups() error = %v", err)
	}
	if !slices.Contains(paths, "/api") || !slices.Contains(paths, "/apis") {
		t.Fatalf("discovery paths = %v", paths)
	}
	for _, path := range paths {
		if path != "/api" && path != "/apis" && !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/apis/") {
			t.Fatalf("unexpected translated path %q", path)
		}
	}
}

func TestRESTMapperFallsBackToCoreV1Discovery(t *testing.T) {
	var pathsMu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, r.URL.Path)
		pathsMu.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/api/v1":
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name:         "nodes",
					SingularName: "node",
					Kind:         "Node",
					Namespaced:   false,
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			})
		case "/api", "/apis":
			http.Redirect(w, r, "/login?referer="+r.URL.Path, http.StatusFound)
		case "/login":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>KubeSphere</title>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	getter := NewRESTClientGetter(&Options{
		Endpoint:      server.URL,
		Token:         "secret",
		NoInteractive: true,
	}, Dependencies{})

	cachedClient, err := getter.ToDiscoveryClient()
	if err != nil {
		t.Fatalf("ToDiscoveryClient() error = %v", err)
	}
	_, resources, err := cachedClient.ServerGroupsAndResources()
	if err != nil {
		t.Fatalf("ServerGroupsAndResources() error = %v", err)
	}
	if len(resources) != 1 || resources[0].GroupVersion != "v1" {
		t.Fatalf("resources = %#v", resources)
	}

	mapper, err := getter.ToRESTMapper()
	if err != nil {
		t.Fatalf("ToRESTMapper() error = %v", err)
	}
	mapping, err := mapper.RESTMapping(schema.GroupKind{Kind: "Node"})
	if err != nil {
		t.Fatalf("RESTMapping(Node) error = %v", err)
	}
	if mapping.Resource != (schema.GroupVersionResource{Version: "v1", Resource: "nodes"}) {
		t.Fatalf("Resource = %v", mapping.Resource)
	}
	pathsMu.Lock()
	defer pathsMu.Unlock()
	if !slices.Contains(paths, "/api/v1") {
		t.Fatalf("discovery paths = %v, want /api/v1 fallback", paths)
	}
}

func TestRESTClientGetterUsesInjectedTransportAsTLSOwner(t *testing.T) {
	transport := &recordingTransport{}
	getter := NewRESTClientGetter(&Options{
		Endpoint:              "https://ks.example.com",
		Token:                 "secret",
		InsecureSkipTLSVerify: true,
		NoInteractive:         true,
	}, Dependencies{Transport: transport})

	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	if restConfig.Transport != transport {
		t.Fatalf("Transport = %#v, want injected transport", restConfig.Transport)
	}
	if !reflect.DeepEqual(restConfig.TLSClientConfig, rest.TLSClientConfig{}) {
		t.Fatalf("TLSClientConfig = %#v, want empty because transport owns TLS", restConfig.TLSClientConfig)
	}

	client, err := getter.ToDiscoveryClient()
	if err != nil {
		t.Fatalf("ToDiscoveryClient() error = %v", err)
	}
	if _, err := client.ServerGroups(); err != nil {
		t.Fatalf("ServerGroups() error = %v", err)
	}
	if !slices.Contains(transport.Paths(), "/api") || !slices.Contains(transport.Paths(), "/apis") {
		t.Fatalf("transport paths = %v, want /api and /apis", transport.Paths())
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("Encode() error = %v", err)
	}
}

type recordingTransport struct {
	mu    sync.Mutex
	paths []string
}

type recordingTokenProvider struct {
	calls int
}

func (p *recordingTokenProvider) Token(context.Context, auth.Resolved, auth.TokenOptions) (string, error) {
	p.calls++
	return "secret", nil
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.paths = append(t.paths, request.URL.Path)
	t.mu.Unlock()

	var body string
	switch request.URL.Path {
	case "/api":
		body = `{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`
	case "/apis":
		body = `{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`
	case "/api/v1":
		body = `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"v1","resources":[]}`
	default:
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("not found")),
			Request:    request,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (t *recordingTransport) Paths() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.paths)
}
