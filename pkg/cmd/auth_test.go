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
	cmd.SetArgs([]string{"auth", "login", server.URL, "--username", "admin", "--password", "temporary-password", "--context", "local"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), `Logged in to "local"`) {
		t.Fatalf("output = %q", out.String())
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.CurrentContext != "local" || loaded.Clusters["local"].Host != server.URL {
		t.Fatalf("config = %#v", loaded)
	}
	user := loaded.Users["admin"]
	if user.Username != "admin" || user.BearerToken != "" || user.BearerTokenFile != "" {
		t.Fatalf("user = %#v", user)
	}
	if loaded.Contexts["local"].User != "admin" {
		t.Fatalf("context user = %q", loaded.Contexts["local"].User)
	}
	if strings.Contains(readFile(t, configPath), "temporary-password") || strings.Contains(readFile(t, configPath), "issued-token") {
		t.Fatalf("config contains sensitive data:\n%s", readFile(t, configPath))
	}

	entry, err := tokencache.Load(filepath.Join(home, ".ksctl", "cache", "tokens"), "local")
	if err != nil {
		t.Fatalf("Load token cache error = %v", err)
	}
	if entry.AccessToken != "issued-token" || entry.RefreshToken != "refresh-token" || entry.ExpiresIn != 7200 {
		t.Fatalf("token entry = %#v", entry)
	}
}

func TestLogoutDeletesTokenCacheAndClearsLegacyToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Clusters["local"] = config.Cluster{Host: "https://ks.example.com"}
	cfg.Users["local"] = config.User{
		Username:        "admin",
		BearerToken:     "legacy-token",
		BearerTokenFile: "/tmp/legacy-token",
	}
	cfg.Contexts["local"] = config.Context{Cluster: "local", User: "local"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	cacheDir := filepath.Join(home, ".ksctl", "cache", "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
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
	if _, err := tokencache.Load(cacheDir, "local"); !os.IsNotExist(err) {
		t.Fatalf("Load token cache error = %v, want not exist", err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	user := loaded.Users["local"]
	if user.BearerToken != "" || user.BearerTokenFile != "" || user.Username != "admin" {
		t.Fatalf("user = %#v", user)
	}
	if loaded.CurrentContext != "local" {
		t.Fatalf("current context = %q", loaded.CurrentContext)
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
