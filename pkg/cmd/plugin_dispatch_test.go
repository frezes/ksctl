package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type recordingPluginHandler struct {
	paths        map[string]string
	lookups      []string
	executedPath string
	executedArgs []string
	executedEnv  []string
	executeErr   error
}

func (h *recordingPluginHandler) Lookup(filename string) (string, bool) {
	h.lookups = append(h.lookups, filename)
	path, ok := h.paths[filename]
	return path, ok
}

func (h *recordingPluginHandler) Execute(path string, args, environment []string) error {
	h.executedPath = path
	h.executedArgs = slices.Clone(args)
	h.executedEnv = slices.Clone(environment)
	return h.executeErr
}

func TestDispatchPluginUsesLongestMatchAndForwardsState(t *testing.T) {
	t.Setenv("KSCTL_PLUGIN_TEST_ENV", "visible")
	handler := &recordingPluginHandler{paths: map[string]string{
		"foo-bar": "/plugins/ksctl-foo-bar",
	}}
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "test"})

	err := dispatchPlugin(root, []string{"ksctl", "foo", "bar", "value", "--mode=fast"}, handler)
	if err != nil {
		t.Fatalf("dispatchPlugin() error = %v", err)
	}
	if got, want := handler.lookups, []string{"foo-bar-value", "foo-bar"}; !slices.Equal(got, want) {
		t.Fatalf("lookups = %v, want %v", got, want)
	}
	if got, want := handler.executedArgs, []string{"value", "--mode=fast"}; !slices.Equal(got, want) {
		t.Fatalf("executed args = %v, want %v", got, want)
	}
	if !slices.Contains(handler.executedEnv, "KSCTL_PLUGIN_TEST_ENV=visible") {
		t.Fatal("environment does not contain test value")
	}
}

func TestDispatchPluginFallsBackAndConvertsDashes(t *testing.T) {
	handler := &recordingPluginHandler{paths: map[string]string{
		"foo_bar": "/plugins/ksctl-foo_bar",
	}}
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "test"})

	if err := dispatchPlugin(root, []string{"ksctl", "foo-bar", "tail"}, handler); err != nil {
		t.Fatalf("dispatchPlugin() error = %v", err)
	}
	if got, want := handler.lookups, []string{"foo_bar-tail", "foo_bar"}; !slices.Equal(got, want) {
		t.Fatalf("lookups = %v, want %v", got, want)
	}
	if got, want := handler.executedArgs, []string{"tail"}; !slices.Equal(got, want) {
		t.Fatalf("executed args = %v, want %v", got, want)
	}
}

func TestDispatchPluginDoesNotOverrideOrExtendBuiltIns(t *testing.T) {
	cases := [][]string{
		{"ksctl", "version"},
		{"ksctl", "version", "extra"},
		{"ksctl", "auth", "missing"},
		{"ksctl", "auth", "login", "one", "two"},
	}
	for _, arguments := range cases {
		handler := &recordingPluginHandler{paths: map[string]string{
			"version":    "/plugins/ksctl-version",
			"auth":       "/plugins/ksctl-auth",
			"auth-login": "/plugins/ksctl-auth-login",
		}}
		root := NewRootCommand(IOStreams{}, VersionInfo{Version: "test"})
		if err := dispatchPlugin(root, arguments, handler); err != nil {
			t.Fatalf("dispatchPlugin(%v) error = %v", arguments, err)
		}
		if len(handler.lookups) != 0 || handler.executedPath != "" {
			t.Fatalf("built-in path triggered plugin handler: %#v", handler)
		}
	}
}

func TestDispatchPluginRejectsFlagsBeforePluginName(t *testing.T) {
	handler := &recordingPluginHandler{paths: map[string]string{"foo": "/plugins/ksctl-foo"}}
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "test"})
	err := dispatchPlugin(root, []string{"ksctl", "--context", "prod", "foo"}, handler)
	if err == nil || !strings.Contains(err.Error(), "flags cannot be placed before plugin name") {
		t.Fatalf("dispatchPlugin() error = %v", err)
	}
}

func TestDispatchPluginSkipsHelpAndCompletionRequests(t *testing.T) {
	for _, commandName := range []string{"help", "__complete", "__completeNoDesc"} {
		handler := &recordingPluginHandler{paths: map[string]string{commandName: "/plugins/ksctl-" + commandName}}
		root := NewRootCommand(IOStreams{}, VersionInfo{Version: "test"})
		if err := dispatchPlugin(root, []string{"ksctl", commandName}, handler); err != nil {
			t.Fatalf("dispatchPlugin(%q) error = %v", commandName, err)
		}
		if len(handler.lookups) != 0 {
			t.Fatalf("request %q triggered plugin lookup: %v", commandName, handler.lookups)
		}
	}
}

func TestDispatchPluginPropagatesExecutionError(t *testing.T) {
	want := errors.New("execute failed")
	handler := &recordingPluginHandler{
		paths:      map[string]string{"foo": "/plugins/ksctl-foo"},
		executeErr: want,
	}
	root := NewRootCommand(IOStreams{}, VersionInfo{Version: "test"})
	if err := dispatchPlugin(root, []string{"ksctl", "foo"}, handler); !errors.Is(err, want) {
		t.Fatalf("dispatchPlugin() error = %v, want %v", err, want)
	}
}

func TestDispatchPluginLeavesMissingPluginToCobra(t *testing.T) {
	handler := &recordingPluginHandler{paths: map[string]string{}}
	out := new(bytes.Buffer)
	root := NewRootCommand(IOStreams{Out: out, ErrOut: out}, VersionInfo{Version: "test"})
	if err := dispatchPlugin(root, []string{"ksctl", "missing"}, handler); err != nil {
		t.Fatalf("dispatchPlugin() error = %v", err)
	}
	root.SetArgs([]string{"missing"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestDefaultPluginHandlerExecutesForBothEntrypoints(t *testing.T) {
	const helperEnvironment = "KSCTL_PLUGIN_HELPER_PROCESS"
	if os.Getenv(helperEnvironment) == "1" {
		arguments := []string{os.Getenv("KSCTL_PLUGIN_ARGV0"), "probe", "first", "--second=two"}
		streams := IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
		var root *cobra.Command
		var err error
		if os.Getenv("KSCTL_PLUGIN_ENTRYPOINT") == "kubectl" {
			root, err = NewKubectlPluginCommandWithArgs(streams, VersionInfo{Version: "test"}, arguments)
		} else {
			root, err = NewRootCommandWithArgs(streams, VersionInfo{Version: "test"}, arguments)
		}
		if err == nil {
			err = root.Execute()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if runtime.GOOS == "windows" {
		t.Skip("release integration test uses a POSIX executable script")
	}

	pluginDirectory := t.TempDir()
	pluginPath := filepath.Join(pluginDirectory, "ksctl-probe")
	plugin := "#!/bin/sh\nIFS= read -r input\nprintf 'arg1=%s\\narg2=%s\\nenv=%s\\nstdin=%s\\n' \"$1\" \"$2\" \"$KSCTL_PLUGIN_VISIBLE\" \"$input\"\nprintf 'stderr=%s\\n' \"$KSCTL_PLUGIN_VISIBLE\" >&2\nexit 7\n"
	if err := os.WriteFile(pluginPath, []byte(plugin), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	for _, entrypoint := range []string{"ksctl", "kubectl"} {
		t.Run(entrypoint, func(t *testing.T) {
			helper := exec.Command(os.Args[0], "-test.run=^TestDefaultPluginHandlerExecutesForBothEntrypoints$")
			helper.Stdin = strings.NewReader("from-stdin\n")
			helper.Env = []string{
				helperEnvironment + "=1",
				"KSCTL_PLUGIN_ENTRYPOINT=" + entrypoint,
				"KSCTL_PLUGIN_ARGV0=" + entrypoint,
				"KSCTL_PLUGIN_VISIBLE=visible",
				"PATH=" + pluginDirectory + string(os.PathListSeparator) + os.Getenv("PATH"),
			}
			stdout := new(bytes.Buffer)
			stderr := new(bytes.Buffer)
			helper.Stdout = stdout
			helper.Stderr = stderr
			err := helper.Run()
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 7 {
				t.Fatalf("helper error = %v, stdout = %q, stderr = %q", err, stdout, stderr)
			}
			if got, want := stdout.String(), "arg1=first\narg2=--second=two\nenv=visible\nstdin=from-stdin\n"; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if got, want := stderr.String(), "stderr=visible\n"; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestPluginFilenamePrefix(t *testing.T) {
	if got, want := pluginFilenamePrefixes, []string{"ksctl"}; !slices.Equal(got, want) {
		t.Fatalf("plugin filename prefixes = %v, want %v", got, want)
	}
}

var _ pluginHandler = (*recordingPluginHandler)(nil)
