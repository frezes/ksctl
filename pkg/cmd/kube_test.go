package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRootGetAndKubeGetProduceEquivalentResults(t *testing.T) {
	server := newClusterScopedCoreAPIServer(t, "member")
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	var outputs []string
	for _, prefix := range [][]string{{"get"}, {"kube", "get"}} {
		out := new(bytes.Buffer)
		command := NewRootCommand(
			IOStreams{Out: out, ErrOut: new(bytes.Buffer)},
			VersionInfo{Version: "test"},
		)
		args := append([]string{}, prefix...)
		args = append(args,
			"pods",
			"--all-namespaces",
			"--endpoint", server.URL,
			"--token", "secret",
			"--cluster", "member",
			"-o", "json",
		)
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		outputs = append(outputs, out.String())
	}
	if outputs[0] != outputs[1] {
		t.Fatalf("root get output differs from kube get:\nroot:\n%s\nkube:\n%s", outputs[0], outputs[1])
	}
}

func TestKubeRequestTimeoutLimitsRawGet(t *testing.T) {
	const helperEnv = "KSCTL_TEST_KUBE_REQUEST_TIMEOUT"
	if os.Getenv(helperEnv) == "1" {
		command := NewRootCommand(
			IOStreams{Out: os.Stdout, ErrOut: os.Stderr},
			VersionInfo{Version: "test"},
		)
		command.SetArgs([]string{
			"kube", "get",
			"--raw=/slow",
			"--request-timeout=20ms",
			"--endpoint", os.Getenv("KSCTL_TEST_KUBE_REQUEST_TIMEOUT_ENDPOINT"),
			"--token", "secret",
		})
		_ = command.Execute()
		return
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	start := time.Now()
	helper := exec.Command(os.Args[0], "-test.run=^TestKubeRequestTimeoutLimitsRawGet$")
	helper.Env = append(
		os.Environ(),
		helperEnv+"=1",
		"KSCTL_TEST_KUBE_REQUEST_TIMEOUT_ENDPOINT="+server.URL,
		"KSCTL_CONFIG="+filepath.Join(t.TempDir(), "config.yaml"),
	)
	output, err := helper.CombinedOutput()
	if err == nil {
		t.Fatalf("timeout helper succeeded, output:\n%s", output)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request timeout took %s, want less than one second", elapsed)
	}
	if !strings.Contains(strings.ToLower(string(output)), "timeout") &&
		!strings.Contains(strings.ToLower(string(output)), "deadline exceeded") {
		t.Fatalf("timeout helper output = %q, want timeout error", output)
	}
}

func TestKubeRegistersCompleteOperationCommandSet(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	kube := findSubcommand(root, "kube")
	if kube == nil {
		t.Fatal("kube command is not registered")
	}

	want := []string{
		"annotate",
		"api-resources",
		"api-versions",
		"apply",
		"attach",
		"auth",
		"autoscale",
		"certificate",
		"cluster-info",
		"cordon",
		"cp",
		"create",
		"debug",
		"delete",
		"describe",
		"diff",
		"drain",
		"edit",
		"events",
		"exec",
		"explain",
		"expose",
		"get",
		"kustomize",
		"label",
		"logs",
		"patch",
		"port-forward",
		"proxy",
		"replace",
		"rollout",
		"run",
		"scale",
		"set",
		"taint",
		"top",
		"uncordon",
		"wait",
	}
	for _, name := range want {
		if findSubcommand(kube, name) == nil {
			t.Errorf("kube %s is not registered", name)
		}
	}
	for _, name := range []string{
		"alpha",
		"completion",
		"config",
		"kuberc",
		"options",
		"plugin",
		"version",
	} {
		if findSubcommand(kube, name) != nil {
			t.Errorf("excluded kube %s is registered", name)
		}
	}
}

func TestKubeRejectsExcludedCommandPaths(t *testing.T) {
	for _, name := range []string{
		"alpha",
		"completion",
		"config",
		"kuberc",
		"options",
		"plugin",
		"version",
	} {
		t.Run(name, func(t *testing.T) {
			root := NewRootCommand(
				IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
				VersionInfo{Version: "dev"},
			)
			root.SetArgs([]string{"kube", name, "--help"})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), `unknown command "`+name+`"`) {
				t.Fatalf("Execute() error = %v, want unknown command %q", err, name)
			}
		})
	}
}

func TestKubePreservesRepresentativeKubectlSubcommandsAndFlags(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	kube := findSubcommand(root, "kube")

	for parent, children := range map[string][]string{
		"auth":    {"can-i", "reconcile", "whoami"},
		"rollout": {"history", "pause", "restart", "resume", "status", "undo"},
		"set":     {"env", "image", "resources", "selector", "serviceaccount", "subject"},
	} {
		command := findSubcommand(kube, parent)
		if command == nil {
			t.Fatalf("kube %s is not registered", parent)
		}
		for _, child := range children {
			if findSubcommand(command, child) == nil {
				t.Errorf("kube %s %s is not registered", parent, child)
			}
		}
	}

	for path, flags := range map[string][]string{
		"apply": {"server-side"},
		"drain": {"ignore-daemonsets"},
		"exec":  {"stdin", "tty"},
	} {
		command := findSubcommand(kube, path)
		if command == nil {
			t.Fatalf("kube %s is not registered", path)
		}
		for _, flag := range flags {
			if command.Flags().Lookup(flag) == nil {
				t.Errorf("kube %s --%s is not registered", path, flag)
			}
		}
	}
}

func TestKubeHelpGroupsOperationCommands(t *testing.T) {
	out := new(bytes.Buffer)
	root := NewRootCommand(
		IOStreams{Out: out, ErrOut: out},
		VersionInfo{Version: "dev"},
	)
	root.SetArgs([]string{"kube", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{
		"Basic Commands (Beginner):",
		"Deploy Commands:",
		"Cluster Management Commands:",
		"Troubleshooting and Debugging Commands:",
		"Advanced Commands:",
		"Settings Commands:",
		"Discovery Commands:",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("kube help missing %q:\n%s", want, out.String())
		}
	}
}

func TestKubeHelpOmitsExcludedOptionsHintAndUsesEntrypointDisplayName(t *testing.T) {
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
			want: `Use "ksctl kube <command> --help"`,
		},
		{
			name: "alternate entrypoint",
			newRoot: func() *cobra.Command {
				return newTestEntrypointCommand(t, IOStreams{}, VersionInfo{Version: "dev"})
			},
			want: `Use "fixture entrypoint kube <command> --help"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := new(bytes.Buffer)
			root := test.newRoot()
			root.SetOut(out)
			root.SetErr(out)
			root.SetArgs([]string{"kube", "--help"})
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), test.want) {
				t.Fatalf("help = %q, want %q", out.String(), test.want)
			}
			if strings.Contains(out.String(), "kube options") {
				t.Fatalf("help advertises excluded options command: %s", out.String())
			}
		})
	}
}

func TestKubeOperationHelpUsesEntrypointDisplayName(t *testing.T) {
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
				{"apply", "--help"},
				{"rollout", "status", "--help"},
			} {
				out := new(bytes.Buffer)
				root := test.newRoot()
				root.SetOut(out)
				root.SetErr(out)
				root.SetArgs(append([]string{"kube"}, path...))
				if err := root.Execute(); err != nil {
					t.Fatalf("Execute(%v) error = %v", path, err)
				}
				if !strings.Contains(out.String(), test.want) {
					t.Fatalf("help = %q, want %q", out.String(), test.want)
				}
				if strings.Contains(out.String(), "kubectl apply") ||
					strings.Contains(out.String(), "kubectl rollout") {
					t.Fatalf("help contains stale kubectl invocation: %s", out.String())
				}
			}
		})
	}
}

func TestKubeDeleteRoutesMutationThroughSpecifiedCluster(t *testing.T) {
	const prefix = "/clusters/member"
	var lock sync.Mutex
	var deletePaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		if !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("request path %q is not scoped to %q", r.URL.Path, prefix)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, prefix)
		switch path {
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
					Verbs:        metav1.Verbs{"delete", "get", "list"},
					ShortNames:   []string{"po"},
				}},
			})
		case "/api/v1/namespaces/default/pods/demo-pod":
			switch r.Method {
			case http.MethodGet:
				writeAPIJSON(t, w, podObject())
			case http.MethodDelete:
				lock.Lock()
				deletePaths = append(deletePaths, r.URL.Path)
				lock.Unlock()
				writeAPIJSON(t, w, podObject())
			default:
				t.Errorf("method = %s, want GET or DELETE", r.Method)
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	command := NewRootCommand(
		IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
		VersionInfo{Version: "test"},
	)
	command.SetArgs([]string{
		"kube", "delete", "pod/demo-pod",
		"--namespace", "default",
		"--wait=false",
		"--endpoint", server.URL,
		"--token", "secret",
		"--cluster", "member",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	lock.Lock()
	defer lock.Unlock()
	want := prefix + "/api/v1/namespaces/default/pods/demo-pod"
	if len(deletePaths) != 1 || deletePaths[0] != want {
		t.Fatalf("DELETE paths = %v, want [%s]", deletePaths, want)
	}
}
