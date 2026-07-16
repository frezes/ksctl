package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
)

func TestLoginWritesConfigAndTokenCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{
		Host:            "https://old.example.com",
		TLSClientConfig: config.TLSClientConfig{ServerName: "ks.example.com"},
		Users: map[string]config.User{
			"admin":    {Username: "configured-admin", BearerToken: "manual-token", BearerTokenFile: "/tmp/manual-token", Password: "manual-password"},
			"readonly": {Username: "viewer", BearerToken: "readonly-token"},
		},
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("path = %q, want /oauth/token", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		for key, want := range map[string]string{
			"grant_type":    "password",
			"client_id":     "kubesphere",
			"client_secret": "kubesphere",
			"username":      "admin",
			"password":      "temporary-password",
		} {
			if got := r.Form.Get(key); got != want {
				t.Errorf("form[%q] = %q, want %q", key, got, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"issued-token","refresh_token":"refresh-token","token_type":"bearer","expires_in":7200}`))
	}))
	defer server.Close()

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "login", server.URL, "--username", "admin", "--password", "temporary-password", "--fleet", "prod", "--context", "prod-admin"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `Logged in to "prod-admin"`) {
		t.Fatalf("output = %q", out.String())
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.CurrentContext != "prod-admin" || loaded.Fleets["prod"].Host != server.URL {
		t.Fatalf("config = %#v", loaded)
	}
	if loaded.Fleets["prod"].TLSClientConfig.ServerName != "ks.example.com" || loaded.Fleets["prod"].Users["readonly"].BearerToken != "readonly-token" {
		t.Fatalf("fleet merge lost data: %#v", loaded.Fleets["prod"])
	}
	user := loaded.Fleets["prod"].Users["admin"]
	if user.Username != "admin" || user.BearerToken != "manual-token" || user.BearerTokenFile != "/tmp/manual-token" || user.Password != "manual-password" {
		t.Fatalf("user = %#v", user)
	}
	if loaded.Contexts["prod-admin"].Fleet != "prod" || loaded.Contexts["prod-admin"].User != "admin" {
		t.Fatalf("context = %#v", loaded.Contexts["prod-admin"])
	}
	if strings.Contains(readFile(t, configPath), "temporary-password") || strings.Contains(readFile(t, configPath), "issued-token") {
		t.Fatalf("config contains sensitive data:\n%s", readFile(t, configPath))
	}

	entry, err := tokencache.Load(filepath.Join(home, ".ksctl", "cache", "tokens"), "prod", "admin")
	if err != nil {
		t.Fatalf("Load token cache error = %v", err)
	}
	if entry.AccessToken != "issued-token" || entry.RefreshToken != "refresh-token" || entry.ExpiresIn != 7200 {
		t.Fatalf("token entry = %#v", entry)
	}
}

func TestLoginDerivesFleetAndContextWithoutUsingExistingContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"issued-token","expires_in":7200}`))
	}))
	defer server.Close()

	fleetName := defaultLoginFleetName(server.URL)
	contextName := tokencache.SafeName(fleetName + "-admin")
	cfg := config.New()
	cfg.Fleets["existing"] = config.Fleet{Host: "https://existing.example.com", Users: map[string]config.User{"admin": {Username: "admin"}}}
	cfg.Contexts[contextName] = config.Context{Fleet: "existing", User: "admin"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "login", server.URL, "--username", "admin", "--password", "temporary-password"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.CurrentContext != contextName || loaded.Contexts[contextName].Fleet != fleetName || loaded.Fleets[fleetName].Host != server.URL {
		t.Fatalf("config = %#v", loaded)
	}
}

func TestLogoutDeletesTokenCacheAndPreservesConfiguredCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: "https://ks.example.com", Users: map[string]config.User{"local": {
		Username: "admin", BearerToken: "manual-token",
		BearerTokenFile: "/tmp/manual-token", Password: "manual-password",
	}}}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "local"}
	cfg.Contexts["local-cluster-a"] = config.Context{Fleet: "local", User: "local", DefaultCluster: "cluster-a"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	before := readFile(t, configPath)
	cacheDir := filepath.Join(home, ".ksctl", "cache", "tokens")
	if err := tokencache.Save(cacheDir, "local", "local", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "cached-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    7200,
	}, time.Now())); err != nil {
		t.Fatalf("Save token error = %v", err)
	}

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout", "local"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `Logged out from "local"`) {
		t.Fatalf("output = %q", out.String())
	}
	if _, err := tokencache.Load(cacheDir, "local", "local"); !os.IsNotExist(err) {
		t.Fatalf("Load token cache error = %v, want not exist", err)
	}
	if after := readFile(t, configPath); after != before {
		t.Fatalf("config changed during logout:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestLogoutReturnsBrokenContextReferenceErrors(t *testing.T) {
	for _, test := range []struct {
		name, want string
		configure  func(*config.Config)
	}{
		{name: "missing context", want: "no context exists with the name: local"},
		{name: "missing fleet", want: "no fleet exists with the name: missing", configure: func(cfg *config.Config) {
			cfg.Contexts["local"] = config.Context{Fleet: "missing", User: "admin"}
		}},
		{name: "missing user", want: "no user exists with the name: missing in fleet: prod", configure: func(cfg *config.Config) {
			cfg.Fleets["prod"] = config.Fleet{Host: "https://prod.example.com"}
			cfg.Contexts["local"] = config.Context{Fleet: "prod", User: "missing"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("KSCTL_CONFIG", configPath)
			cfg := config.New()
			cfg.CurrentContext = "local"
			if test.configure != nil {
				test.configure(cfg)
			}
			if err := config.Save(configPath, cfg); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
			cmd.SetArgs([]string{"auth", "logout"})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoginCommandDoesNotDefineInsecureFlag(t *testing.T) {
	cmd := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	auth := findSubcommand(cmd, "auth")
	if auth == nil {
		t.Fatal("auth command is not registered")
	}
	login := findSubcommand(auth, "login")
	if login == nil {
		t.Fatal("login command is not registered")
	}
	if login.Flags().Lookup("insecure-skip-tls-verify") != nil {
		t.Fatal("login defines --insecure-skip-tls-verify")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(data)
}
