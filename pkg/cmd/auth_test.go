package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	authcmd "github.com/kubesphere/ksctl/pkg/cmd/auth"
	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
)

type commandPrompter struct {
	results []string
	prompts []string
	writer  io.Writer
}

func (p *commandPrompter) Available() bool { return true }

func (p *commandPrompter) ReadLine(prompt string) (string, error) {
	return p.read(prompt)
}

func (p *commandPrompter) ReadPassword(prompt string) (string, error) {
	return p.read(prompt)
}

func (p *commandPrompter) read(prompt string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	if _, err := io.WriteString(p.writer, prompt); err != nil {
		return "", err
	}
	result := p.results[0]
	p.results = p.results[1:]
	return result, nil
}

func TestLoginPromptsAndPersistsResolvedDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		if r.Form.Get("username") != "admin" || r.Form.Get("password") != "temporary-password" {
			t.Errorf("form = %#v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"issued-token","expires_in":7200}`))
	}))
	defer server.Close()

	prompter := &commandPrompter{results: []string{server.URL, "admin", "temporary-password", "", ""}}
	cmd := newLoginTestRoot(prompter)
	cmd.SetArgs([]string{"auth", "login"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	fleet := authcmd.DefaultFleetName(server.URL)
	contextName := authcmd.DefaultContextName(fleet, "admin")
	wantErr := "endpoint: username: password: fleet [" + fleet + "]: context [" + contextName + "]: "
	if got := cmd.ErrOrStderr().(*bytes.Buffer).String(); got != wantErr {
		t.Fatalf("stderr = %q, want %q", got, wantErr)
	}
	wantOut := "Logged in to \"" + contextName + "\"\n"
	if got := cmd.OutOrStdout().(*bytes.Buffer).String(); got != wantOut {
		t.Fatalf("stdout = %q, want %q", got, wantOut)
	}
	if got := cmd.OutOrStdout().(*bytes.Buffer).String(); strings.Contains(got, wantErr) {
		t.Fatalf("stdout = %q, contains prompt transcript %q", got, wantErr)
	}
	if got := cmd.ErrOrStderr().(*bytes.Buffer).String(); strings.Contains(got, wantOut) {
		t.Fatalf("stderr = %q, contains success message %q", got, wantOut)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.CurrentContext != contextName || loaded.Contexts[contextName].Fleet != fleet || loaded.Fleets[fleet].Host != server.URL {
		t.Fatalf("config = %#v", loaded)
	}
}

func TestLoginMissingRequiredInputDoesNotReadNonTerminalStdin(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "endpoint", args: []string{"auth", "login"}, want: "error: endpoint is required"},
		{name: "username", args: []string{"auth", "login", "https://ks.example.com"}, want: "error: --username is required"},
		{name: "password", args: []string{"auth", "login", "https://ks.example.com", "--username", "admin"}, want: "error: --password is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := NewRootCommand(IOStreams{In: strings.NewReader("must-not-be-read\n"), Out: io.Discard, ErrOut: io.Discard}, VersionInfo{Version: "dev"})
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if err == nil || err.Error() != test.want {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoginAuthFailureDoesNotRetryOrPersist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()

	prompter := &commandPrompter{results: []string{server.URL, "admin", "temporary-password", "", ""}}
	cmd := newLoginTestRoot(prompter)
	cmd.SetArgs([]string{"auth", "login"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "KubeSphere login failed") {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config file error = %v, want not exist", err)
	}
	fleet := authcmd.DefaultFleetName(server.URL)
	if _, err := tokencache.Load(filepath.Join(home, ".ksctl", "cache", "tokens"), fleet, "admin"); !os.IsNotExist(err) {
		t.Fatalf("Load token cache error = %v, want not exist", err)
	}
}

func TestLoginRejectsMultipleEndpoints(t *testing.T) {
	cmd := NewRootCommand(IOStreams{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "login", "https://one.example.com", "https://two.example.com"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts at most 1 arg(s)") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func newLoginTestRoot(prompter *commandPrompter) *cobra.Command {
	root := &cobra.Command{Use: "ksctl", SilenceErrors: true, SilenceUsage: true}
	root.SetIn(strings.NewReader(""))
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))

	oauth := auth.NewOAuth(clientkubesphere.NewRESTClientFactory(nil))
	authCommand := &cobra.Command{Use: "auth"}
	authCommand.AddCommand(newLoginCommandWithPrompter(
		"ksctl/test",
		oauth,
		func(_ io.Reader, writer io.Writer) authcmd.Prompter {
			prompter.writer = writer
			return prompter
		},
	))
	root.AddCommand(authCommand)
	return root
}

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
	cmd := NewRootCommand(IOStreams{In: strings.NewReader(""), Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
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

	fleetName := authcmd.DefaultFleetName(server.URL)
	contextName := authcmd.DefaultContextName(fleetName, "admin")
	cfg := config.New()
	cfg.Fleets["existing"] = config.Fleet{Host: "https://existing.example.com", Users: map[string]config.User{"admin": {Username: "admin"}}}
	cfg.Contexts[contextName] = config.Context{Fleet: "existing", User: "admin"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	prompter := &commandPrompter{}
	cmd := newLoginTestRoot(prompter)
	cmd.SetArgs([]string{"auth", "login", server.URL, "--username", "admin", "--password", "temporary-password"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(prompter.prompts) != 0 {
		t.Fatalf("prompts = %#v, want none", prompter.prompts)
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
	logoutRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logoutRequests++
		if r.Method != http.MethodGet || r.URL.Path != "/oauth/logout" {
			t.Errorf("request = %s %s, want GET /oauth/logout", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer cached-token" {
			t.Errorf("Authorization = %q, want cached bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: server.URL, Users: map[string]config.User{"local": {
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
	if logoutRequests != 1 {
		t.Fatalf("logout requests = %d, want 1", logoutRequests)
	}
}

func prepareLogoutTest(t *testing.T, host string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: host, Users: map[string]config.User{"admin": {Username: "admin"}}}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "admin"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return filepath.Join(home, ".ksctl", "cache", "tokens")
}

func TestLogoutIgnoresRemoteFailureAndDeletesCache(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "remote failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	cacheDir := prepareLogoutTest(t, server.URL)
	if err := tokencache.Save(cacheDir, "local", "admin", tokencache.NewEntry(tokencache.Response{AccessToken: "cached-token", ExpiresIn: 7200}, time.Now())); err != nil {
		t.Fatalf("Save token error = %v", err)
	}

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("logout requests = %d, want 1", requests)
	}
	if _, err := tokencache.Load(cacheDir, "local", "admin"); !os.IsNotExist(err) {
		t.Fatalf("Load token cache error = %v, want not exist", err)
	}
}

func TestLogoutIgnoresTransportFailureAndDeletesCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host := server.URL
	server.Close()
	cacheDir := prepareLogoutTest(t, host)
	if err := tokencache.Save(cacheDir, "local", "admin", tokencache.NewEntry(tokencache.Response{AccessToken: "cached-token", ExpiresIn: 7200}, time.Now())); err != nil {
		t.Fatalf("Save token error = %v", err)
	}

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := tokencache.Load(cacheDir, "local", "admin"); !os.IsNotExist(err) {
		t.Fatalf("Load token cache error = %v, want not exist", err)
	}
}

func TestLogoutWithoutCacheSkipsRemoteRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	prepareLogoutTest(t, server.URL)
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("logout requests = %d, want 0", requests)
	}
}

func TestLogoutDeletesMalformedCacheWithoutRemoteRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	cacheDir := prepareLogoutTest(t, server.URL)
	path := tokencache.Path(cacheDir, "local", "admin")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("logout requests = %d, want 0", requests)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stat token cache error = %v, want not exist", err)
	}
}

func TestLogoutDeletesUnreadableCacheWithoutRemoteRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	cacheDir := prepareLogoutTest(t, server.URL)
	path := tokencache.Path(cacheDir, "local", "admin")
	if err := tokencache.Save(cacheDir, "local", "admin", tokencache.NewEntry(tokencache.Response{AccessToken: "cached-token", ExpiresIn: 7200}, time.Now())); err != nil {
		t.Fatalf("Save token error = %v", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("logout requests = %d, want 0", requests)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stat token cache error = %v, want not exist", err)
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

func TestAuthWhoAmIPrintsServerUserAndGlobalRole(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		const wantPath = "/kapis/iam.kubesphere.io/v1beta1/users/admin"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if got := r.Header.Get("User-Agent"); got != "ksctl/test" {
			t.Errorf("User-Agent = %q, want ksctl/test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"User","apiVersion":"iam.kubesphere.io/v1beta1","metadata":{"name":"admin","annotations":{"iam.kubesphere.io/globalrole":"platform-admin"}}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	saveWhoAmITestConfig(t, configPath, server.URL, "admin", "secret")

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"auth", "whoami", "--context", "local", "--cluster", "must-not-scope-request"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "Username: admin\nGlobal Role: platform-admin\n"
	if got := out.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestAuthWhoAmIUsesNoneForMissingGlobalRole(t *testing.T) {
	for _, response := range []string{
		`{"metadata":{"name":"alice"}}`,
		`{"metadata":{"name":"alice","annotations":{"iam.kubesphere.io/globalrole":""}}}`,
	} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("KSCTL_CONFIG", configPath)
			saveWhoAmITestConfig(t, configPath, server.URL, "alice", "secret")
			out := new(bytes.Buffer)
			cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
			cmd.SetArgs([]string{"auth", "whoami"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			const want = "Username: alice\nGlobal Role: <none>\n"
			if got := out.String(); got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestAuthWhoAmIReturnsServerAndResponseErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		response   string
		wantError  string
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, response: `{"message":"unauthorized"}`, wantError: "get KubeSphere user"},
		{name: "not found", statusCode: http.StatusNotFound, response: `{"message":"not found"}`, wantError: "get KubeSphere user"},
		{name: "malformed JSON", statusCode: http.StatusOK, response: `{`, wantError: "decode KubeSphere user"},
		{name: "missing name", statusCode: http.StatusOK, response: `{"metadata":{"annotations":{"iam.kubesphere.io/globalrole":"platform-admin"}}}`, wantError: "KubeSphere user response is missing metadata.name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("KSCTL_CONFIG", configPath)
			saveWhoAmITestConfig(t, configPath, server.URL, "admin", "secret")
			cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
			cmd.SetArgs([]string{"auth", "whoami"})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Execute() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestAuthWhoAmIRejectsInvalidUsernameBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	saveWhoAmITestConfig(t, configPath, server.URL, "invalid/name", "secret")
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"auth", "whoami"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `invalid username "invalid/name"`) {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestAuthWhoAmIRequiresContext(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing-config.yaml"))
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"auth", "whoami"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "resolve KubeSphere username") || !strings.Contains(err.Error(), "current-context is not set") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestAuthWhoAmIReturnsOutputError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"name":"admin"}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	saveWhoAmITestConfig(t, configPath, server.URL, "admin", "secret")
	cmd := NewRootCommand(IOStreams{Out: errorWriter{err: errors.New("write failed")}, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "test"})
	cmd.SetArgs([]string{"auth", "whoami"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "write whoami output") || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func saveWhoAmITestConfig(t *testing.T, path, endpoint, username, token string) {
	t.Helper()
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{
		Host: endpoint,
		Users: map[string]config.User{
			"account": {Username: username, BearerToken: token},
		},
	}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "account", DefaultCluster: "member"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
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
