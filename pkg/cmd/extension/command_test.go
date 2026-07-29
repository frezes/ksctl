package extension

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func commandNames(command *cobra.Command) []string {
	names := make([]string, 0, len(command.Commands()))
	for _, child := range command.Commands() {
		if !child.IsAvailableCommand() {
			continue
		}
		names = append(names, child.Name())
	}
	return names
}

func TestNewCommandRegistersExtensionSurfaceWithoutLogs(t *testing.T) {
	command := NewCommand(
		"ksctl",
		genericiooptions.IOStreams{
			In:     strings.NewReader(""),
			Out:    io.Discard,
			ErrOut: io.Discard,
		},
		func() (Service, error) { return &fakeService{}, nil },
	)
	want := []string{
		"configure",
		"diagnose",
		"install",
		"list",
		"show",
		"status",
		"uninstall",
		"upgrade",
		"versions",
	}
	if got := commandNames(command); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	if command.CommandPath() == "logs" {
		t.Fatal("logs command unexpectedly registered")
	}
}

func TestNewCommandHelpUsesParentDisplayName(t *testing.T) {
	for _, parent := range []string{"ksctl", "kubectl ks"} {
		t.Run(parent, func(t *testing.T) {
			var output bytes.Buffer
			root := &cobra.Command{
				Use:           strings.ReplaceAll(parent, " ", "-"),
				SilenceUsage:  true,
				SilenceErrors: true,
			}
			if parent != root.Use {
				root.Annotations = map[string]string{
					cobra.CommandDisplayNameAnnotation: parent,
				}
			}
			streams := genericiooptions.IOStreams{
				In:     strings.NewReader(""),
				Out:    &output,
				ErrOut: &output,
			}
			root.SetOut(&output)
			root.SetErr(&output)
			root.AddCommand(NewCommand(parent, streams, func() (Service, error) {
				return &fakeService{}, nil
			}))
			root.SetArgs([]string{"extension", "list", "--help"})

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(output.String(), parent+" extension list") {
				t.Fatalf("help = %q, want display name %q", output.String(), parent)
			}
		})
	}
}

func TestScopeRejectsExplicitClusterAndNamespaceBeforeFactory(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "cluster",
			args: []string{"--cluster", "member-a", "extension", "list"},
			want: "--cluster is not supported",
		},
		{
			name: "namespace",
			args: []string{"--namespace", "project-a", "extension", "list"},
			want: "--namespace is not supported",
		},
		{
			name: "namespace shorthand",
			args: []string{"-n", "project-a", "extension", "list"},
			want: "--namespace is not supported",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			root := scopeTestRoot(t, func() (Service, error) {
				called = true
				return &fakeService{}, nil
			})
			root.SetArgs(test.args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
			if called {
				t.Fatal("service factory was called")
			}
		})
	}
}

func TestExtensionRejectsUnsafeVerbosityBeforeFactory(t *testing.T) {
	called := false
	root := scopeTestRoot(t, func() (Service, error) {
		called = true
		return &fakeService{}, nil
	})
	root.PersistentFlags().Int("v", 0, "")
	root.SetArgs([]string{"--v=8", "extension", "list"})

	err := root.Execute()
	if err == nil ||
		!strings.Contains(err.Error(), "--v=7 or lower") ||
		!strings.Contains(err.Error(), "expose extension configuration") {
		t.Fatalf("Execute() error = %v, want safe verbosity rejection", err)
	}
	if called {
		t.Fatal("service factory was called")
	}
}

func TestNamedCommandsRejectEmptyNameBeforeFactory(t *testing.T) {
	for _, args := range [][]string{
		{"extension", "show", ""},
		{"extension", "versions", ""},
		{"extension", "status", ""},
		{"extension", "install", "", "--version", "1.0.0"},
		{"extension", "upgrade", "", "--version", "1.1.0"},
		{"extension", "configure", "", "--clear-config"},
		{"extension", "uninstall", ""},
		{"extension", "diagnose", ""},
	} {
		t.Run(strings.Join(args[:2], "_"), func(t *testing.T) {
			called := false
			root := scopeTestRoot(t, func() (Service, error) {
				called = true
				return &fakeService{}, nil
			})
			root.SetArgs(args)

			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "non-empty") {
				t.Fatalf("Execute(%q) error = %v", args, err)
			}
			if called {
				t.Fatalf("Execute(%q) called service factory", args)
			}
		})
	}
}

func scopeTestRoot(t *testing.T, factory ServiceFactory) *cobra.Command {
	t.Helper()
	root := &cobra.Command{
		Use:           "ksctl",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("cluster", "", "")
	root.PersistentFlags().StringP("namespace", "n", "", "")
	streams := genericiooptions.IOStreams{
		In:     strings.NewReader(""),
		Out:    io.Discard,
		ErrOut: io.Discard,
	}
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.AddCommand(NewCommand("ksctl", streams, factory))
	return root
}
