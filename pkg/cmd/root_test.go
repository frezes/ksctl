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
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"kubesphere.io/ksctl/pkg/config"
)

const (
	testEntrypointUse         = "fixture-entrypoint"
	testEntrypointDisplayName = "fixture entrypoint"
)

func newTestEntrypointCommand(
	t *testing.T,
	streams IOStreams,
	info VersionInfo,
) *cobra.Command {
	t.Helper()
	command, err := NewEntrypointCommandWithArgs(
		testEntrypointUse,
		testEntrypointDisplayName,
		streams,
		info,
		[]string{testEntrypointUse},
	)
	if err != nil {
		t.Fatalf("NewEntrypointCommandWithArgs() error = %v", err)
	}
	return command
}

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
		cmd.SetArgs([]string{"kube", "--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		help := out.String()
		for _, want := range []string{
			"Show details of a specific resource or group of resources",
			"Display one or many resources",
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

func TestEntrypointHelpUsesDisplayName(t *testing.T) {
	out := new(bytes.Buffer)
	cmd := newTestEntrypointCommand(
		t,
		IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "dev"},
	)
	cmd.SetArgs([]string{"get", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	help := out.String()
	if !strings.Contains(help, "Usage:\n  fixture entrypoint get") {
		t.Fatalf("entrypoint help = %q", help)
	}
	if !strings.Contains(help, "fixture entrypoint get pods") {
		t.Fatalf("entrypoint examples = %q", help)
	}
}

func TestKubeResourceHelpUsesEntrypointDisplayName(t *testing.T) {
	for _, test := range []struct {
		name    string
		newRoot func() *cobra.Command
		want    string
	}{
		{
			name: "standalone",
			newRoot: func() *cobra.Command {
				return NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: "ksctl kube",
		},
		{
			name: "alternate entrypoint",
			newRoot: func() *cobra.Command {
				return newTestEntrypointCommand(t, IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: "fixture entrypoint kube",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, path := range [][]string{
				{"get", "--help"},
				{"logs", "--help"},
				{"top", "pod", "--help"},
			} {
				out := new(bytes.Buffer)
				root := test.newRoot()
				root.SetOut(out)
				root.SetErr(out)
				root.SetArgs(append([]string{"kube"}, path...))
				if err := root.Execute(); err != nil {
					t.Fatalf("Execute(%v) error = %v", path, err)
				}
				if !strings.Contains(out.String(), test.want+" "+strings.Join(path[:len(path)-1], " ")) {
					t.Fatalf("help = %q, want display path %q", out.String(), test.want)
				}
			}
		})
	}
}

func TestRootRegistersKubeResourceCommands(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})

	rootGet := findSubcommand(root, "get")
	if rootGet == nil {
		t.Fatal("root get command is not registered")
	}
	for _, name := range []string{"describe", "logs", "top"} {
		if findSubcommand(root, name) != nil {
			t.Errorf("root %s command is registered", name)
		}
	}

	kube := findSubcommand(root, "kube")
	if kube == nil {
		t.Fatal("kube command is not registered")
	}
	kubeGet := findSubcommand(kube, "get")
	if kubeGet == nil {
		t.Fatal("kube get command is not registered")
	}
	for _, name := range []string{"output", "watch", "watch-only", "selector"} {
		if rootGet.Flags().Lookup(name) == nil {
			t.Errorf("root get flag --%s is not registered", name)
		}
		if kubeGet.Flags().Lookup(name) == nil {
			t.Errorf("kube get flag --%s is not registered", name)
		}
	}

	describe := findSubcommand(kube, "describe")
	if describe == nil || describe.Flags().Lookup("show-events") == nil {
		t.Fatal("kube describe --show-events is not registered")
	}
	logs := findSubcommand(kube, "logs")
	if logs == nil {
		t.Fatal("kube logs command is not registered")
	}
	for _, name := range []string{"follow", "previous", "container", "tail"} {
		if logs.Flags().Lookup(name) == nil {
			t.Errorf("kube logs flag --%s is not registered", name)
		}
	}
	top := findSubcommand(kube, "top")
	if top == nil {
		t.Fatal("kube top command is not registered")
	}
	topPod := findSubcommand(top, "pod")
	if topPod == nil || topPod.Flags().Lookup("containers") == nil {
		t.Fatal("kube top pod --containers is not registered")
	}
	topNode := findSubcommand(top, "node")
	if topNode == nil || topNode.Flags().Lookup("show-capacity") == nil {
		t.Fatal("kube top node --show-capacity is not registered")
	}
}

func TestPluginListReportsRootGetAndKubeAsBuiltInConflicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test plugin fixtures use Unix executable mode bits")
	}
	directory := t.TempDir()
	for _, name := range []string{"ksctl-get", "ksctl-kube", "ksctl-logs", "ksctl-top"} {
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
		`ksctl-get overwrites existing command: "ksctl get"`,
		`ksctl-kube overwrites existing command: "ksctl kube"`,
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want %q", errOut.String(), want)
		}
	}
	for _, unwanted := range []string{"ksctl-logs overwrites", "ksctl-top overwrites"} {
		if strings.Contains(errOut.String(), unwanted) {
			t.Fatalf("stderr = %q, does not want %q", errOut.String(), unwanted)
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

func TestRootRegistersExtensionCommands(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
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
			name: "alternate entrypoint",
			root: newTestEntrypointCommand(
				t,
				IOStreams{},
				VersionInfo{Version: "dev"},
			),
			want: "fixture entrypoint extension install",
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
		{
			name: "alternate entrypoint",
			root: newTestEntrypointCommand(
				t,
				IOStreams{},
				VersionInfo{Version: "dev"},
			),
			want: "fixture entrypoint plugin list",
		},
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
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
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

func TestRootRegistersTenantGetCommands(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
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
	for _, name := range []string{"all-namespaces", "selector", "field-selector", "output", "workspace", "namespace"} {
		if get.Flags().Lookup(name) == nil {
			t.Fatalf("tenant get flag --%s is not registered", name)
		}
	}
	if findSubcommand(root, "get").Flags().Lookup("workspace") != nil {
		t.Fatal("top-level get unexpectedly has --workspace")
	}
	if findSubcommand(findSubcommand(root, "kube"), "get").Flags().Lookup("workspace") != nil {
		t.Fatal("kube get unexpectedly has --workspace")
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

func TestRootTenantArbitraryGetUsesResolvedCluster(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCluster string
	}{
		{
			name:        "context default cluster",
			args:        []string{"tenant", "get", "pods", "-A", "-o", "json"},
			wantCluster: "context-member",
		},
		{
			name: "explicit cluster overrides context",
			args: []string{
				"--cluster", "flag-member",
				"tenant", "get", "pods", "-A", "-o", "json",
			},
			wantCluster: "flag-member",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KS_ENDPOINT", "")
			t.Setenv("KS_TOKEN", "")
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			t.Setenv("KSCTL_CONFIG", configPath)

			var (
				pathsMu sync.Mutex
				paths   []string
			)
			server := httptest.NewServer(http.HandlerFunc(func(
				w http.ResponseWriter,
				request *http.Request,
			) {
				pathsMu.Lock()
				paths = append(paths, request.URL.Path)
				pathsMu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				clusterPrefix := "/clusters/" + test.wantCluster
				switch request.URL.Path {
				case clusterPrefix + "/api":
					_, _ = io.WriteString(
						w,
						`{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`,
					)
				case clusterPrefix + "/apis":
					_, _ = io.WriteString(
						w,
						`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`,
					)
				case clusterPrefix + "/api/v1":
					_, _ = io.WriteString(w, `{
						"kind":"APIResourceList",
						"apiVersion":"v1",
						"groupVersion":"v1",
						"resources":[{
							"name":"pods",
							"singularName":"pod",
							"namespaced":true,
							"kind":"Pod",
							"verbs":["get","list","watch"]
						}]
					}`)
				case clusterPrefix + "/api/v1/pods":
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `{"kind":"Status","status":"Failure"}`)
				case clusterPrefix +
					"/kapis/tenant.kubesphere.io/v1beta1/namespaces":
					_, _ = io.WriteString(
						w,
						`{"items":[{"metadata":{"name":"team-a"}}]}`,
					)
				case clusterPrefix + "/api/v1/namespaces/team-a/pods":
					_, _ = io.WriteString(w, `{
						"apiVersion":"v1",
						"kind":"PodList",
						"metadata":{"resourceVersion":"1"},
						"items":[{
							"apiVersion":"v1",
							"kind":"Pod",
							"metadata":{"namespace":"team-a","name":"pod-a"}
						}]
					}`)
				default:
					http.NotFound(w, request)
				}
			}))
			defer server.Close()

			saveKubeconfigTestConfig(
				t,
				configPath,
				server.URL,
				"alice",
				"context-member",
				"secret",
			)
			out := new(bytes.Buffer)
			command := NewRootCommand(
				IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
				VersionInfo{Version: "test"},
			)
			command.SetArgs(test.args)
			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), `"name": "pod-a"`) {
				t.Fatalf("output missing pod-a:\n%s", out.String())
			}
			pathsMu.Lock()
			defer pathsMu.Unlock()
			for _, want := range []string{
				"/clusters/" + test.wantCluster +
					"/kapis/tenant.kubesphere.io/v1beta1/namespaces",
				"/clusters/" + test.wantCluster +
					"/api/v1/namespaces/team-a/pods",
			} {
				if !slices.Contains(paths, want) {
					t.Fatalf("paths = %v, want %q", paths, want)
				}
			}
		})
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

func TestRootAndKubeConnectionFlagScopes(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	for _, name := range []string{"endpoint", "token", "context", "cluster", "v"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("persistent flag --%s is not registered", name)
		}
	}
	for _, name := range []string{
		"insecure-skip-tls-verify",
		"namespace",
		"no-interactive",
		"request-timeout",
		"workspace",
	} {
		if root.PersistentFlags().Lookup(name) != nil {
			t.Errorf("root persistent flag --%s is registered", name)
		}
	}

	kube := findSubcommand(root, "kube")
	if kube == nil {
		t.Fatal("kube command is not registered")
	}
	namespace := kube.PersistentFlags().Lookup("namespace")
	if namespace == nil || namespace.Shorthand != "n" {
		t.Fatalf("kube --namespace = %#v, want persistent -n flag", namespace)
	}
	if kube.PersistentFlags().Lookup("request-timeout") == nil {
		t.Fatal("kube persistent --request-timeout is not registered")
	}

	rootGet := findSubcommand(root, "get")
	namespace = rootGet.LocalNonPersistentFlags().Lookup("namespace")
	if namespace == nil || namespace.Shorthand != "n" {
		t.Fatalf("root get --namespace = %#v, want command-local -n flag", namespace)
	}
	if rootGet.Flags().Lookup("request-timeout") != nil {
		t.Fatal("root get exposes --request-timeout")
	}

	for _, path := range [][]string{
		{"get"},
		{"describe"},
		{"logs"},
		{"top", "pod"},
	} {
		command := kube
		for _, name := range path {
			command = findSubcommand(command, name)
			if command == nil {
				t.Fatalf("kube %s is not registered", strings.Join(path, " "))
			}
		}
		if command.InheritedFlags().Lookup("namespace") == nil {
			t.Errorf("kube %s does not inherit --namespace", strings.Join(path, " "))
		}
		if command.InheritedFlags().Lookup("request-timeout") == nil {
			t.Errorf("kube %s does not inherit --request-timeout", strings.Join(path, " "))
		}
	}
}

func TestRootRegistersAPICommand(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
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
