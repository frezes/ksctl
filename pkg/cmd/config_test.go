package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
)

func TestConfigCurrentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "prod"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "current-context"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(out.String()) != "prod" {
		t.Fatalf("current context = %q", out.String())
	}
}

func TestConfigUseContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "use-context", "prod"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentContext != "prod" {
		t.Fatalf("current context = %q", loaded.CurrentContext)
	}
}

func TestConfigViewRedactsSecretsByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{
		Host:            "https://prod.example.com",
		TLSClientConfig: config.TLSClientConfig{KeyData: "private-key"},
		Users: map[string]config.User{"admin": {
			BearerToken: "secret-token",
			Password:    "secret-password",
		}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "view"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, secret := range []string{"private-key", "secret-token", "secret-password"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("config view exposed %q: %s", secret, out.String())
		}
	}
	if strings.Count(out.String(), "<redacted>") != 3 {
		t.Fatalf("config view output = %s", out.String())
	}
}

func TestConfigViewRawShowsStoredSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{
		Host:            "https://prod.example.com",
		TLSClientConfig: config.TLSClientConfig{KeyData: "private-key"},
		Users: map[string]config.User{"admin": {
			BearerToken: "secret-token",
			Password:    "secret-password",
		}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KSCTL_CONFIG", path)

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"config", "view", "--raw"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, secret := range []string{"private-key", "secret-token", "secret-password"} {
		if !strings.Contains(out.String(), secret) {
			t.Fatalf("raw config view omitted %q: %s", secret, out.String())
		}
	}
}

func TestConfigGenerateKubeconfig(t *testing.T) {
	const response = "apiVersion: v1\nkind: Config\ncurrent-context: generated"

	for _, test := range []struct {
		name           string
		username       string
		defaultCluster string
		clusterFlag    string
		wantPath       string
	}{
		{
			name:           "explicit cluster overrides context default",
			username:       "alice",
			defaultCluster: "context-member",
			clusterFlag:    "flag-member",
			wantPath:       "/clusters/flag-member/kapis/resources.kubesphere.io/v1alpha2/users/alice/kubeconfig",
		},
		{
			name:           "context default cluster",
			username:       "alice",
			defaultCluster: "context-member",
			wantPath:       "/clusters/context-member/kapis/resources.kubesphere.io/v1alpha2/users/alice/kubeconfig",
		},
		{
			name:     "unscoped host",
			username: "alice",
			wantPath: "/kapis/resources.kubesphere.io/v1alpha2/users/alice/kubeconfig",
		},
		{
			name:     "user key fallback",
			wantPath: "/kapis/resources.kubesphere.io/v1alpha2/users/account/kubeconfig",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KS_ENDPOINT", "")
			t.Setenv("KS_TOKEN", "")
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				if r.URL.Path != test.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, test.wantPath)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer secret" {
					t.Errorf("Authorization = %q, want Bearer secret", got)
				}
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("KSCTL_CONFIG", configPath)
			saveKubeconfigTestConfig(t, configPath, server.URL, test.username, test.defaultCluster, "secret")

			out := new(bytes.Buffer)
			cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
			args := []string{"config", "generate", "kubeconfig"}
			if test.clusterFlag != "" {
				args = append(args, "--cluster", test.clusterFlag)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got := out.String(); got != response {
				t.Fatalf("stdout = %q, want %q", got, response)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestConfigGenerateKubeconfigRejectsPositionalArguments(t *testing.T) {
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"config", "generate", "kubeconfig", "unexpected"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown command "unexpected"`) {
		t.Fatalf("Execute() error = %v, want unknown command", err)
	}
}

func TestConfigGenerateKubeconfigRejectsInvalidIdentityOrClusterBeforeRequest(t *testing.T) {
	for _, test := range []struct {
		name           string
		username       string
		defaultCluster string
		configure      func(*config.Config)
		args           []string
		want           string
	}{
		{
			name: "missing current context",
			configure: func(cfg *config.Config) {
				cfg.CurrentContext = ""
				cfg.Contexts = map[string]config.Context{}
			},
			want: "current-context is not set",
		},
		{
			name:     "invalid username",
			username: "team/alice",
			want:     "invalid username",
		},
		{
			name:     "invalid cluster",
			username: "alice",
			args:     []string{"--cluster", "team/member"},
			want:     "invalid cluster",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KS_ENDPOINT", "")
			t.Setenv("KS_TOKEN", "")
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				http.NotFound(w, r)
			}))
			defer server.Close()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("KSCTL_CONFIG", configPath)
			cfg := saveKubeconfigTestConfig(t, configPath, server.URL, test.username, test.defaultCluster, "secret")
			if test.configure != nil {
				test.configure(cfg)
				if err := config.Save(configPath, cfg); err != nil {
					t.Fatalf("Save() error = %v", err)
				}
			}

			cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
			args := []string{"config", "generate", "kubeconfig"}
			if test.name == "missing current context" {
				args = append(args, "--endpoint", server.URL, "--token", "secret")
			}
			args = append(args, test.args...)
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
			if requests != 0 {
				t.Fatalf("requests = %d, want 0", requests)
			}
		})
	}
}

func TestConfigGenerateKubeconfigReturnsServerErrorWithoutOutput(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	saveKubeconfigTestConfig(t, configPath, server.URL, "alice", "", "secret")
	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"config", "generate", "kubeconfig"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "get kubeconfig for user") {
		t.Fatalf("Execute() error = %v, want kubeconfig request error", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestConfigGenerateKubeconfigReturnsOutputError(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("kubeconfig"))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	saveKubeconfigTestConfig(t, configPath, server.URL, "alice", "", "secret")
	cmd := NewRootCommand(IOStreams{Out: errorWriter{err: errors.New("write failed")}, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"config", "generate", "kubeconfig"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "write kubeconfig") || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("Execute() error = %v, want output error", err)
	}
}

func TestConfigGenerateKubeconfigRefreshesExpiredToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)

	var lock sync.Mutex
	refreshRequests := 0
	kubeconfigRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"refreshed-token","refresh_token":"new-refresh-token","token_type":"bearer","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/clusters/host/kapis/resources.kubesphere.io/v1alpha2/users/alice/kubeconfig" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer refreshed-token" {
			t.Errorf("Authorization = %q, want refreshed token", got)
		}
		lock.Lock()
		kubeconfigRequests++
		lock.Unlock()
		_, _ = w.Write([]byte("refreshed-kubeconfig"))
	}))
	defer server.Close()

	saveKubeconfigTestConfig(t, configPath, server.URL, "alice", "host", "")
	cacheDir := filepath.Join(home, ".ksctl", "cache", "tokens")
	if err := tokencache.Save(cacheDir, "local", "account", tokencache.NewEntry(tokencache.Response{
		AccessToken: "expired-token", RefreshToken: "expired-refresh-token", ExpiresIn: 1,
	}, time.Now().Add(-time.Hour))); err != nil {
		t.Fatalf("Save token cache error = %v", err)
	}

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"config", "generate", "kubeconfig"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.String() != "refreshed-kubeconfig" {
		t.Fatalf("stdout = %q", out.String())
	}
	entry, err := tokencache.Load(cacheDir, "local", "account")
	if err != nil {
		t.Fatalf("Load token cache error = %v", err)
	}
	if entry.AccessToken != "refreshed-token" || entry.RefreshToken != "new-refresh-token" {
		t.Fatalf("refreshed cache = %#v", entry)
	}
	lock.Lock()
	defer lock.Unlock()
	if refreshRequests != 1 || kubeconfigRequests != 1 {
		t.Fatalf("refresh requests = %d, kubeconfig requests = %d", refreshRequests, kubeconfigRequests)
	}
}

func saveKubeconfigTestConfig(t *testing.T, path, host, username, defaultCluster, token string) *config.Config {
	t.Helper()
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: host, Users: map[string]config.User{
		"account": {Username: username, BearerToken: token},
	}}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "account", DefaultCluster: defaultCluster}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return cfg
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
