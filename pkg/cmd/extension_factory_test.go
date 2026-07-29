package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	kubesphererest "kubesphere.io/client-go/rest"
)

type recordingExtensionConfigGetter struct {
	config *kubesphererest.Config
	err    error
	calls  int
}

func (g *recordingExtensionConfigGetter) ToRESTConfig() (*kubesphererest.Config, error) {
	g.calls++
	return g.config, g.err
}

type recordingExtensionClientFactory struct {
	err   error
	calls int
	got   *kubesphererest.Config
}

func (f *recordingExtensionClientFactory) ForConfig(
	config *kubesphererest.Config,
) (kubesphererest.Interface, error) {
	f.calls++
	f.got = config
	return nil, f.err
}

func extensionFactoryRoot(
	streams IOStreams,
	getter extensionRESTConfigGetter,
	factory extensionRESTClientFactory,
) *cobra.Command {
	root := &cobra.Command{
		Use:           "ksctl",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("cluster", "", "")
	root.PersistentFlags().StringP("namespace", "n", "", "")
	root.SetOut(streams.Out)
	root.SetErr(streams.ErrOut)
	root.SetIn(streams.In)
	root.AddCommand(newExtensionCommand("ksctl", streams, getter, factory))
	return root
}

func TestExtensionFactoryStaysLazyForLocalFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "help",
			args: []string{"extension", "install", "--help"},
		},
		{
			name: "argument",
			args: []string{"extension", "install"},
		},
		{
			name: "scope",
			args: []string{"--cluster", "member-a", "extension", "list"},
		},
		{
			name: "input",
			args: []string{"extension", "install", "demo"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			getter := &recordingExtensionConfigGetter{}
			factory := &recordingExtensionClientFactory{}
			var output bytes.Buffer
			root := extensionFactoryRoot(
				IOStreams{
					In:     strings.NewReader(""),
					Out:    &output,
					ErrOut: io.Discard,
				},
				getter,
				factory,
			)
			root.SetArgs(test.args)
			_ = root.Execute()
			if getter.calls != 0 || factory.calls != 0 {
				t.Fatalf(
					"getter calls = %d, factory calls = %d",
					getter.calls,
					factory.calls,
				)
			}
		})
	}
}

func TestExtensionFactoryResolvesHostConfigOnlyOnExecution(t *testing.T) {
	sentinel := errors.New("factory failed")
	config := &kubesphererest.Config{
		Host:        "https://ks.example.com",
		BearerToken: "secret",
	}
	getter := &recordingExtensionConfigGetter{config: config}
	factory := &recordingExtensionClientFactory{err: sentinel}
	var output bytes.Buffer
	root := extensionFactoryRoot(
		IOStreams{
			In:     strings.NewReader(""),
			Out:    &output,
			ErrOut: io.Discard,
		},
		getter,
		factory,
	)
	root.SetArgs([]string{"extension", "list"})

	err := root.Execute()
	if !errors.Is(err, sentinel) {
		t.Fatalf("Execute() error = %v, want sentinel", err)
	}
	if getter.calls != 1 || factory.calls != 1 || factory.got != config {
		t.Fatalf(
			"getter calls = %d, factory calls = %d, config = %#v",
			getter.calls,
			factory.calls,
			factory.got,
		)
	}
	if output.Len() != 0 {
		t.Fatalf("stdout = %q", output.String())
	}
}
