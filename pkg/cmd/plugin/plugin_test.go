package plugin

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type acceptingVerifier struct{}

func (acceptingVerifier) verify(string) []error { return nil }

func writeCandidate(t *testing.T, directory, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("plugin"), mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func TestListPluginsPreservesPATHOrderAndDeduplicates(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	firstAlpha := writeCandidate(t, first, "ksctl-alpha", 0o755)
	firstBeta := writeCandidate(t, first, "ksctl-beta", 0o755)
	secondAlpha := writeCandidate(t, second, "ksctl-alpha", 0o755)
	writeCandidate(t, second, "other-tool", 0o755)

	o := &listOptions{
		verifier:    acceptingVerifier{},
		pluginPaths: []string{first, first, "", " ", second},
		IOStreams:   genericiooptions.IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)},
	}
	plugins, errs := o.listPlugins()
	if len(errs) != 0 {
		t.Fatalf("listPlugins() errors = %v", errs)
	}
	want := []string{firstAlpha, firstBeta, secondAlpha}
	if !reflect.DeepEqual(plugins, want) {
		t.Fatalf("plugins = %v, want %v", plugins, want)
	}
}

func TestPluginListNameOnlyAndWarnings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode-bit warning assertions are Unix-specific")
	}
	first := t.TempDir()
	second := t.TempDir()
	writeCandidate(t, first, "ksctl-alpha", 0o755)
	writeCandidate(t, first, "ksctl-disabled", 0o644)
	writeCandidate(t, first, "ksctl-version", 0o755)
	writeCandidate(t, first, "ksctl-auth-login", 0o755)
	writeCandidate(t, second, "ksctl-alpha", 0o755)

	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	root := &cobra.Command{Use: "ksctl"}
	root.AddCommand(&cobra.Command{Use: "version"})
	auth := &cobra.Command{Use: "auth"}
	auth.AddCommand(&cobra.Command{Use: "login"})
	root.AddCommand(auth)
	root.AddCommand(NewCommand("ksctl", genericiooptions.IOStreams{Out: out, ErrOut: errOut}))
	root.SetArgs([]string{"plugin", "list", "--name-only"})
	t.Setenv("PATH", first+string(os.PathListSeparator)+second)

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "4 plugin warnings were found") {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, name := range []string{"ksctl-alpha", "ksctl-disabled", "ksctl-version", "ksctl-auth-login"} {
		if !strings.Contains(out.String(), name+"\n") {
			t.Fatalf("stdout missing %q: %q", name, out.String())
		}
	}
	for _, want := range []string{"is not executable", "overwrites existing command", "is shadowed"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr missing %q: %q", want, errOut.String())
		}
	}
}

func TestPluginListPrintsFullPathsByDefault(t *testing.T) {
	directory := t.TempDir()
	pluginPath := writeCandidate(t, directory, "ksctl-alpha", 0o755)
	out := new(bytes.Buffer)
	root := &cobra.Command{Use: "ksctl"}
	root.AddCommand(NewCommand("ksctl", genericiooptions.IOStreams{Out: out, ErrOut: new(bytes.Buffer)}))
	root.SetArgs([]string{"plugin", "list"})
	t.Setenv("PATH", directory)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), pluginPath+"\n") {
		t.Fatalf("stdout = %q, want full path %q", out.String(), pluginPath)
	}
}

func TestPluginListReturnsErrorWhenEmpty(t *testing.T) {
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	root := &cobra.Command{Use: "ksctl"}
	root.AddCommand(NewCommand("ksctl", genericiooptions.IOStreams{Out: out, ErrOut: errOut}))
	root.SetArgs([]string{"plugin", "list"})
	t.Setenv("PATH", t.TempDir())

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unable to find any ksctl plugins") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestPluginListSkipsMissingPATHEntry(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	valid := t.TempDir()
	pluginPath := writeCandidate(t, valid, "ksctl-alpha", 0o755)
	errOut := new(bytes.Buffer)
	o := &listOptions{
		verifier:    acceptingVerifier{},
		pluginPaths: []string{missing, valid},
		IOStreams:   genericiooptions.IOStreams{Out: new(bytes.Buffer), ErrOut: errOut},
	}
	plugins, errs := o.listPlugins()
	if len(errs) != 0 || !reflect.DeepEqual(plugins, []string{pluginPath}) {
		t.Fatalf("plugins = %v, errors = %v", plugins, errs)
	}
	if !strings.Contains(errOut.String(), "Unable to read directory") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}
