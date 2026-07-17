package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestRootRegistersNestedAuthCommands(t *testing.T) {
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "dev"})
	auth := findSubcommand(root, "auth")
	if auth == nil {
		t.Fatal("auth command is not registered")
	}
	for _, name := range []string{"login", "logout"} {
		if findSubcommand(auth, name) == nil {
			t.Fatalf("auth %s command is not registered", name)
		}
		if findSubcommand(root, name) != nil {
			t.Fatalf("top-level %s command is registered", name)
		}
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
