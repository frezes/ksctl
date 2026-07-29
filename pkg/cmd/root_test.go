package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
	"github.com/spf13/cobra"
)

func TestRootVersionPrintsClientAndTargetVersions(t *testing.T) {
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/clusters/member/kapis/version" {
			t.Errorf("path = %q, want /clusters/member/kapis/version", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gitVersion":"v4.2.0","kubernetes":{"gitVersion":"v1.31.0"}}`))
	}))
	defer server.Close()

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "v0.1.0"})
	cmd.SetArgs([]string{"version", "--endpoint", server.URL, "--token", "secret", "--cluster", "member"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "ksctl Version: v0.1.0\nKubeSphere Version: v4.2.0\nKubernetes Version: v1.31.0\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestRootVersionUsesContextDefaultCluster(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/clusters/context-member/kapis/version" {
			t.Errorf("path = %q, want /clusters/context-member/kapis/version", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gitVersion":"v4.2.0","kubernetes":{"gitVersion":"v1.31.0"}}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: server.URL, Users: map[string]config.User{"admin": {BearerToken: "secret"}}}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "admin", DefaultCluster: "context-member"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "v0.1.0"})
	cmd.SetArgs([]string{"version", "--token", "secret"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "ksctl Version: v0.1.0\nKubeSphere Version: v4.2.0\nKubernetes Version: v1.31.0\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestRootVersionUsesUnknownForMissingServerField(t *testing.T) {
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gitVersion":"v4.2.0"}`))
	}))
	defer server.Close()

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"version", "--endpoint", server.URL, "--token", "secret"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "ksctl Version: dev\nKubeSphere Version: v4.2.0\nKubernetes Version: unknown\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestRootVersionUsesUnknownForServerControlCharacters(t *testing.T) {
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gitVersion":"v4.2.0\nforged","kubernetes":{"gitVersion":"\u001b[31mv1.31.0"}}`))
	}))
	defer server.Close()

	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"version", "--endpoint", server.URL, "--token", "secret"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "ksctl Version: dev\nKubeSphere Version: unknown\nKubernetes Version: unknown\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestRootVersionFallsBackToUnknownWithoutServer(t *testing.T) {
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	out := new(bytes.Buffer)
	cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	const want = "ksctl Version: dev\nKubeSphere Version: unknown\nKubernetes Version: unknown\n"
	if got := out.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestRootHelpUsesCommandName(t *testing.T) {
	streams := IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}
	cmd := NewRootCommand(streams, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(streams.Out.(*bytes.Buffer).String(), "ksctl") {
		t.Fatalf("help output should mention ksctl")
	}
}

func TestRootHelpUsesEnglishRegardlessOfLocale(t *testing.T) {
	const helperEnv = "KSCTL_TEST_ENGLISH_HELP"
	if os.Getenv(helperEnv) == "1" {
		out := new(bytes.Buffer)
		cmd := NewRootCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
		cmd.SetArgs([]string{"--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		help := out.String()
		for _, want := range []string{
			"describe    Show details of a specific resource or group of resources",
			"get         Display one or many resources",
		} {
			if !strings.Contains(help, want) {
				t.Fatalf("help does not contain %q: %s", want, help)
			}
		}
		return
	}

	helper := exec.Command(os.Args[0], "-test.run=^TestRootHelpUsesEnglishRegardlessOfLocale$")
	helper.Env = append(os.Environ(),
		helperEnv+"=1",
		"LC_ALL=zh_CN.UTF-8",
		"LC_MESSAGES=zh_CN.UTF-8",
		"LANG=zh_CN.UTF-8",
	)
	if output, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("localized help subprocess failed: %v\n%s", err, output)
	}
}

func TestKubectlPluginHelpUsesDisplayName(t *testing.T) {
	out := new(bytes.Buffer)
	cmd := NewKubectlPluginCommand(IOStreams{Out: out, ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"get", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := out.String()
	if !strings.Contains(help, "Usage:\n  kubectl ks get") || strings.Contains(help, "Usage:\n  kubectl get") {
		t.Fatalf("plugin help = %q", help)
	}
	if !strings.Contains(help, "kubectl ks get pods") || strings.Contains(help, "kubectl get pods") {
		t.Fatalf("plugin examples should use kubectl ks: %q", help)
	}
}

func TestKubectlPluginLogsHelpUsesDisplayName(t *testing.T) {
	out := new(bytes.Buffer)
	cmd := NewKubectlPluginCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "dev"},
	)
	cmd.SetArgs([]string{"logs", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := out.String()
	if !strings.Contains(help, "Usage:\n  kubectl ks logs") ||
		strings.Contains(help, "Usage:\n  kubectl logs") {
		t.Fatalf("plugin logs usage = %q", help)
	}
	if !strings.Contains(help, "kubectl ks logs nginx") ||
		strings.Contains(help, "kubectl logs nginx") {
		t.Fatalf("plugin logs examples should use kubectl ks: %q", help)
	}
}

func TestKubectlPluginTopHelpUsesDisplayNameRecursively(t *testing.T) {
	out := new(bytes.Buffer)
	cmd := NewKubectlPluginCommand(
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "dev"},
	)
	cmd.SetArgs([]string{"top", "pod", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := out.String()
	if !strings.Contains(help, "Usage:\n  kubectl ks top pod") ||
		strings.Contains(help, "Usage:\n  kubectl top pod") {
		t.Fatalf("plugin top pod usage = %q", help)
	}
	if !strings.Contains(help, "kubectl ks top pod") ||
		strings.Contains(help, "kubectl top pod -l") {
		t.Fatalf("plugin top pod examples should use kubectl ks: %q", help)
	}
}

func TestRootRegistersNativeResourceCommands(t *testing.T) {
	cmd := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})

	getCommand := findSubcommand(cmd, "get")
	if getCommand == nil {
		t.Fatal("get command is not registered")
	}
	if findSubcommand(cmd, "describe") == nil {
		t.Fatal("describe command is not registered")
	}
	if findSubcommand(cmd, "list") != nil {
		t.Fatal("list command is registered")
	}
	logsCommand := findSubcommand(cmd, "logs")
	if logsCommand == nil {
		t.Fatal("logs command is not registered")
	}
	for _, name := range []string{"follow", "previous", "container", "tail"} {
		if logsCommand.Flags().Lookup(name) == nil {
			t.Errorf("logs flag --%s is not registered", name)
		}
	}
	topCommand := findSubcommand(cmd, "top")
	if topCommand == nil {
		t.Fatal("top command is not registered")
	}
	topPod := findSubcommand(topCommand, "pod")
	if topPod == nil || topPod.Flags().Lookup("containers") == nil {
		t.Fatal("top pod --containers is not registered")
	}
	topNode := findSubcommand(topCommand, "node")
	if topNode == nil || topNode.Flags().Lookup("show-capacity") == nil {
		t.Fatal("top node --show-capacity is not registered")
	}
	for _, name := range []string{"output", "watch", "watch-only", "selector"} {
		if getCommand.Flags().Lookup(name) == nil {
			t.Errorf("get flag --%s is not registered", name)
		}
	}
	if describeCommand := findSubcommand(cmd, "describe"); describeCommand != nil {
		if describeCommand.Flags().Lookup("show-events") == nil {
			t.Error("describe flag --show-events is not registered")
		}
	}
}

func TestPluginListReportsLogsAndTopAsBuiltInConflicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test plugin fixtures use Unix executable mode bits")
	}
	directory := t.TempDir()
	for _, name := range []string{"ksctl-logs", "ksctl-top"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("plugin"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	root := NewRootCommand(
		IOStreams{Out: out, ErrOut: errOut},
		VersionInfo{Version: "test"},
	)
	root.SetArgs([]string{"plugin", "list", "--name-only"})
	t.Setenv("PATH", directory)

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "2 plugin warnings") {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{
		`ksctl-logs overwrites existing command: "ksctl logs"`,
		`ksctl-top overwrites existing command: "ksctl top"`,
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want %q", errOut.String(), want)
		}
	}
}

func TestRootRegistersPluginListCommand(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	plugin := findSubcommand(root, "plugin")
	if plugin == nil || findSubcommand(plugin, "list") == nil {
		t.Fatal("plugin list command is not registered")
	}
	if findSubcommand(plugin, "list").Flags().Lookup("name-only") == nil {
		t.Fatal("plugin list --name-only is not registered")
	}
}

func TestRootRegistersExtensionCommandsForBothEntrypoints(t *testing.T) {
	for _, root := range []*cobra.Command{
		NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}),
		NewKubectlPluginCommand(IOStreams{}, VersionInfo{Version: "dev"}),
	} {
		extension := findSubcommand(root, "extension")
		if extension == nil {
			t.Fatalf("%s does not register extension", root.DisplayName())
		}
		for _, name := range []string{
			"list",
			"show",
			"versions",
			"status",
			"install",
			"upgrade",
			"configure",
			"uninstall",
			"diagnose",
		} {
			if findSubcommand(extension, name) == nil {
				t.Fatalf(
					"%s extension does not register %s",
					root.DisplayName(),
					name,
				)
			}
		}
		if findSubcommand(extension, "logs") != nil {
			t.Fatalf("%s extension unexpectedly registers logs", root.DisplayName())
		}
	}
}

func TestExtensionHelpUsesEntrypointDisplayName(t *testing.T) {
	for _, test := range []struct {
		name string
		root *cobra.Command
		want string
	}{
		{
			name: "standalone",
			root: NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}),
			want: "ksctl extension install",
		},
		{
			name: "kubectl plugin",
			root: NewKubectlPluginCommand(
				IOStreams{},
				VersionInfo{Version: "dev"},
			),
			want: "kubectl ks extension install",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			test.root.SetOut(&output)
			test.root.SetErr(&output)
			test.root.SetArgs([]string{
				"extension",
				"install",
				"--help",
			})
			if err := test.root.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(output.String(), test.want) {
				t.Fatalf("help = %q, want %q", output.String(), test.want)
			}
		})
	}
}

func TestPluginHelpUsesEntrypointDisplayName(t *testing.T) {
	for _, test := range []struct {
		name string
		root *cobra.Command
		want string
	}{
		{name: "standalone", root: NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}), want: "ksctl plugin list"},
		{name: "kubectl", root: NewKubectlPluginCommand(IOStreams{}, VersionInfo{Version: "dev"}), want: "kubectl ks plugin list"},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := new(bytes.Buffer)
			test.root.SetOut(out)
			test.root.SetArgs([]string{"plugin", "list", "--help"})
			if err := test.root.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), test.want) {
				t.Fatalf("help = %q, want %q", out.String(), test.want)
			}
		})
	}
}

func TestRootRegistersNestedAuthCommands(t *testing.T) {
	for _, root := range []*cobra.Command{
		NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}),
		NewKubectlPluginCommand(IOStreams{}, VersionInfo{Version: "dev"}),
	} {
		auth := findSubcommand(root, "auth")
		if auth == nil {
			t.Fatal("auth command is not registered")
		}
		for _, name := range []string{"login", "logout", "whoami"} {
			if findSubcommand(auth, name) == nil {
				t.Fatalf("auth %s command is not registered", name)
			}
			if findSubcommand(root, name) != nil {
				t.Fatalf("top-level %s command is registered", name)
			}
		}
	}
}

func TestRootRegistersTenantGetCommands(t *testing.T) {
	for _, root := range []*cobra.Command{
		NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}),
		NewKubectlPluginCommand(IOStreams{}, VersionInfo{Version: "dev"}),
	} {
		tenant := findSubcommand(root, "tenant")
		if tenant == nil {
			t.Fatal("tenant command is not registered")
		}
		get := findSubcommand(tenant, "get")
		if get == nil {
			t.Fatal("tenant get command is not registered")
		}
		for _, name := range []string{"workspace", "namespace", "cluster"} {
			if findSubcommand(get, name) == nil {
				t.Fatalf("tenant get %s command is not registered", name)
			}
		}
	}
}

func TestRootTenantGetUsesContextDefaultClusterOnlyForNamespaces(t *testing.T) {
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"totalItems":0}`))
	}))
	defer server.Close()

	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{
		Host:  server.URL,
		Users: map[string]config.User{"admin": {BearerToken: "secret"}},
	}
	cfg.Contexts["local"] = config.Context{
		Fleet:          "local",
		User:           "admin",
		DefaultCluster: "member",
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	for _, args := range [][]string{
		{"tenant", "get", "workspace", "-o", "json"},
		{"tenant", "get", "ns", "-o", "json"},
		{"tenant", "get", "cluster", "-o", "json"},
	} {
		command := NewRootCommand(
			IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
			VersionInfo{Version: "test"},
		)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
	}

	want := []string{
		"/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
		"/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
		"/kapis/tenant.kubesphere.io/v1beta1/clusters",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestRootTenantFleetResourcesIgnoreInvalidExplicitCluster(t *testing.T) {
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"totalItems":0}`))
	}))
	defer server.Close()

	for _, resource := range []string{"workspace", "cluster"} {
		command := NewRootCommand(
			IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
			VersionInfo{Version: "test"},
		)
		command.SetArgs([]string{
			"--endpoint", server.URL,
			"--token", "secret",
			"--cluster", "team/member",
			"tenant", "get", resource, "-o", "json",
		})
		if err := command.Execute(); err != nil {
			t.Fatalf("tenant get %s error = %v, want invalid cluster ignored", resource, err)
		}
	}

	namespace := NewRootCommand(
		IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	namespace.SetArgs([]string{
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "team/member",
		"tenant", "get", "ns", "-o", "json",
	})
	if err := namespace.Execute(); err == nil || !strings.Contains(err.Error(), "invalid cluster") {
		t.Fatalf("tenant get ns error = %v, want invalid cluster", err)
	}

	want := []string{
		"/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
		"/kapis/tenant.kubesphere.io/v1beta1/clusters",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestRootConnectionFlags(t *testing.T) {
	cmd := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	for _, name := range []string{
		"endpoint",
		"token",
		"context",
		"cluster",
		"namespace",
		"request-timeout",
		"v",
	} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("persistent flag --%s is not registered", name)
		}
	}
	for _, name := range []string{"insecure-skip-tls-verify", "no-interactive", "workspace"} {
		if cmd.PersistentFlags().Lookup(name) != nil {
			t.Errorf("persistent flag --%s is registered", name)
		}
	}
}

func TestRootRegistersAPICommandForBothEntrypoints(t *testing.T) {
	for _, root := range []*cobra.Command{
		NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"}),
		NewKubectlPluginCommand(IOStreams{}, VersionInfo{Version: "dev"}),
	} {
		api := findSubcommand(root, "api")
		if api == nil {
			t.Fatalf("%s does not register api", root.DisplayName())
		}
		for _, name := range []string{"method", "data"} {
			if api.Flags().Lookup(name) == nil {
				t.Errorf("%s api flag --%s is not registered", root.DisplayName(), name)
			}
		}
	}
}

func TestRootAPIDoesNotApplyClusterScope(t *testing.T) {
	tests := []struct {
		name     string
		setUp    func(t *testing.T, host string) []string
		wantAuth string
	}{
		{
			name: "explicit cluster",
			setUp: func(t *testing.T, host string) []string {
				t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
				return []string{
					"api", "/kapis/example.io/v1/items?limit=1",
					"--endpoint", host,
					"--token", "flag-token",
					"--cluster", "flag-member",
				}
			},
			wantAuth: "Bearer flag-token",
		},
		{
			name: "context default cluster",
			setUp: func(t *testing.T, host string) []string {
				configPath := filepath.Join(t.TempDir(), "config.yaml")
				saveKubeconfigTestConfig(t, configPath, host, "alice", "context-member", "context-token")
				t.Setenv("KSCTL_CONFIG", configPath)
				return []string{"api", "/kapis/example.io/v1/items?limit=1"}
			},
			wantAuth: "Bearer context-token",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				requests <- request.Clone(request.Context())
				_, _ = w.Write([]byte("ok"))
			}))
			t.Cleanup(server.Close)

			out := new(bytes.Buffer)
			command := NewRootCommand(
				IOStreams{Out: out, ErrOut: io.Discard},
				VersionInfo{Version: "test"},
			)
			command.SetArgs(test.setUp(t, server.URL))

			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			request := <-requests
			if request.URL.RequestURI() != "/kapis/example.io/v1/items?limit=1" {
				t.Fatalf("request URI = %q", request.URL.RequestURI())
			}
			if got := request.Header.Get("Authorization"); got != test.wantAuth {
				t.Fatalf("Authorization = %q, want %q", got, test.wantAuth)
			}
			if out.String() != "ok" {
				t.Fatalf("output = %q, want ok", out.String())
			}
		})
	}
}

func TestRootAcceptsVerbosityFlag(t *testing.T) {
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("KS_ENDPOINT", "")
	t.Setenv("KS_TOKEN", "")
	streams := IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}
	cmd := NewRootCommand(streams, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"-v=8", "version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(streams.Out.(*bytes.Buffer).String(), "ksctl Version: dev") {
		t.Fatalf("version output = %q", streams.Out.(*bytes.Buffer).String())
	}
}

func findSubcommand(root *cobra.Command, name string) *cobra.Command {
	for _, command := range root.Commands() {
		if command.Name() == name {
			return command
		}
	}
	return nil
}
