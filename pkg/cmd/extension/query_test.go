package extension

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kubesphereextension "kubesphere.io/ksctl/pkg/kubesphere/extension"
)

func executeExtensionCommand(
	t *testing.T,
	args []string,
	streams genericiooptions.IOStreams,
	factory ServiceFactory,
) error {
	t.Helper()
	root := &cobra.Command{
		Use:           "ksctl",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("cluster", "", "")
	root.SetOut(streams.Out)
	root.SetErr(streams.ErrOut)
	root.SetIn(streams.In)
	root.AddCommand(NewCommand("ksctl", streams, factory))
	root.SetArgs(args)
	return root.Execute()
}

func bufferedStreams() (genericiooptions.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return genericiooptions.IOStreams{
		In:     strings.NewReader(""),
		Out:    out,
		ErrOut: errOut,
	}, out, errOut
}

func TestListPassesFlagsAndPrintsTable(t *testing.T) {
	streams, out, _ := bufferedStreams()
	var gotOptions kubesphereextension.ListOptions
	service := &fakeService{
		listFn: func(
			_ context.Context,
			options kubesphereextension.ListOptions,
		) (kubesphereextension.ListResult, error) {
			gotOptions = options
			return kubesphereextension.ListResult{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension",
			"list",
			"--category",
			"observability",
			"--installed",
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := (kubesphereextension.ListOptions{
		Category:      "observability",
		InstalledOnly: true,
	}); !reflect.DeepEqual(gotOptions, want) {
		t.Fatalf("options = %#v, want %#v", gotOptions, want)
	}
	if got, want := out.String(),
		"NAME  CATEGORY  RECOMMENDED  INSTALLED  STATE\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestShowPassesOpaqueExactVersion(t *testing.T) {
	streams, _, _ := bufferedStreams()
	var gotName, gotVersion string
	service := &fakeService{
		showFn: func(
			_ context.Context,
			name string,
			version string,
		) (kubesphereextension.ShowResult, error) {
			gotName, gotVersion = name, version
			return kubesphereextension.ShowResult{
				SelectedVersion: &kubesphereextension.Object[kubesphereextension.ExtensionVersion]{
					Value: kubesphereextension.ExtensionVersion{
						Metadata: kubesphereextension.ObjectMeta{Name: "demo-v1"},
						Spec: kubesphereextension.ExtensionVersionSpec{
							Version: version,
						},
					},
				},
			}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "show", "demo", "--version", "v1.0.0+build", "-o", "wide"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotName != "demo" || gotVersion != "v1.0.0+build" {
		t.Fatalf("Show(%q, %q)", gotName, gotVersion)
	}
}

func TestShowRejectsExplicitEmptyVersionBeforeFactory(t *testing.T) {
	streams, out, _ := bufferedStreams()
	called := false
	err := executeExtensionCommand(
		t,
		[]string{"extension", "show", "demo", "--version="},
		streams,
		func() (Service, error) {
			called = true
			return &fakeService{}, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatalf("Execute() error = %v, want --version validation", err)
	}
	if called {
		t.Fatal("service factory was called")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestVersionsAndStatusPassNames(t *testing.T) {
	t.Run("versions", func(t *testing.T) {
		streams, out, _ := bufferedStreams()
		var gotName string
		service := &fakeService{
			versionsFn: func(
				_ context.Context,
				name string,
			) (kubesphereextension.VersionsResult, error) {
				gotName = name
				return kubesphereextension.VersionsResult{}, nil
			},
		}
		err := executeExtensionCommand(
			t,
			[]string{"extension", "versions", "demo"},
			streams,
			func() (Service, error) { return service, nil },
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if gotName != "demo" ||
			out.String() != "VERSION  MODE  KS-VERSION  KUBE-VERSION  NAMESPACE\n" {
			t.Fatalf("name = %q, output = %q", gotName, out.String())
		}
	})

	t.Run("status list", func(t *testing.T) {
		streams, out, _ := bufferedStreams()
		var gotName string
		service := &fakeService{
			statusFn: func(
				_ context.Context,
				name string,
			) (kubesphereextension.StatusResult, error) {
				gotName = name
				list := kubesphereextension.List[kubesphereextension.InstallPlan]{}
				return kubesphereextension.StatusResult{List: &list}, nil
			},
		}
		err := executeExtensionCommand(
			t,
			[]string{"extension", "status"},
			streams,
			func() (Service, error) { return service, nil },
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if gotName != "" ||
			out.String() != "NAME  VERSION  ENABLED  STATE  NAMESPACE  JOB\n" {
			t.Fatalf("name = %q, output = %q", gotName, out.String())
		}
	})
}

func TestStatusWatchValidationHappensBeforeFactory(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "requires name",
			args: []string{"extension", "status", "--watch"},
			want: "--watch requires NAME",
		},
		{
			name: "structured output",
			args: []string{"extension", "status", "demo", "--watch", "-o", "json"},
			want: "table output",
		},
		{
			name: "timeout without watch",
			args: []string{"extension", "status", "demo", "--wait-timeout", "1m"},
			want: "--wait-timeout requires --watch",
		},
		{
			name: "nonpositive timeout",
			args: []string{"extension", "status", "demo", "--watch", "--wait-timeout", "0"},
			want: "positive",
		},
		{
			name: "invalid output",
			args: []string{"extension", "status", "-o", "xml"},
			want: "unsupported output",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			streams, out, _ := bufferedStreams()
			called := false
			err := executeExtensionCommand(
				t,
				test.args,
				streams,
				func() (Service, error) {
					called = true
					return &fakeService{}, nil
				},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
			if called {
				t.Fatal("service factory was called")
			}
			if out.Len() != 0 {
				t.Fatalf("stdout = %q", out.String())
			}
		})
	}
}

func TestStatusWatchPrintsOneHeaderAndInitialDistinctStates(t *testing.T) {
	streams, out, _ := bufferedStreams()
	service := &fakeService{
		watchFn: func(
			_ context.Context,
			name string,
			options kubesphereextension.PollOptions,
		) (kubesphereextension.Object[kubesphereextension.InstallPlan], error) {
			if name != "demo" || options.Timeout != 2*time.Minute {
				t.Fatalf("Watch(%q, timeout=%s)", name, options.Timeout)
			}
			for _, event := range []kubesphereextension.StateEvent{
				{State: ""},
				{
					State:           "Preparing",
					TargetNamespace: "demo-system",
					JobName:         "install-demo",
				},
				{
					State:           "Installed",
					TargetNamespace: "demo-system",
					JobName:         "install-demo",
				},
			} {
				if err := options.OnState(event); err != nil {
					return kubesphereextension.Object[kubesphereextension.InstallPlan]{}, err
				}
			}
			return kubesphereextension.Object[kubesphereextension.InstallPlan]{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension",
			"status",
			"demo",
			"--watch",
			"--wait-timeout",
			"2m",
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := out.String(),
		"STATE      NAMESPACE    JOB\n"+
			"<pending>  <none>       <none>\n"+
			"Preparing  demo-system  install-demo\n"+
			"Installed  demo-system  install-demo\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if strings.Count(out.String(), "STATE") != 1 {
		t.Fatalf("watch header repeated: %q", out.String())
	}
}

func TestQueryServiceErrorsEmitNoStdout(t *testing.T) {
	sentinel := errors.New("server unavailable")
	for _, test := range []struct {
		name    string
		args    []string
		service *fakeService
	}{
		{
			name: "list",
			args: []string{"extension", "list"},
			service: &fakeService{listFn: func(
				context.Context,
				kubesphereextension.ListOptions,
			) (kubesphereextension.ListResult, error) {
				return kubesphereextension.ListResult{}, sentinel
			}},
		},
		{
			name: "show",
			args: []string{"extension", "show", "demo"},
			service: &fakeService{showFn: func(
				context.Context,
				string,
				string,
			) (kubesphereextension.ShowResult, error) {
				return kubesphereextension.ShowResult{}, sentinel
			}},
		},
		{
			name: "versions",
			args: []string{"extension", "versions", "demo"},
			service: &fakeService{versionsFn: func(
				context.Context,
				string,
			) (kubesphereextension.VersionsResult, error) {
				return kubesphereextension.VersionsResult{}, sentinel
			}},
		},
		{
			name: "status",
			args: []string{"extension", "status"},
			service: &fakeService{statusFn: func(
				context.Context,
				string,
			) (kubesphereextension.StatusResult, error) {
				return kubesphereextension.StatusResult{}, sentinel
			}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			streams, out, _ := bufferedStreams()
			err := executeExtensionCommand(
				t,
				test.args,
				streams,
				func() (Service, error) { return test.service, nil },
			)
			if !errors.Is(err, sentinel) {
				t.Fatalf("Execute() error = %v, want sentinel", err)
			}
			if out.Len() != 0 {
				t.Fatalf("stdout = %q", out.String())
			}
		})
	}
}

func TestQueryWrapsOutputWriterFailures(t *testing.T) {
	sentinel := errors.New("closed")
	streams := genericiooptions.IOStreams{
		In:     strings.NewReader(""),
		Out:    failingWriter{err: sentinel},
		ErrOut: io.Discard,
	}
	service := &fakeService{
		listFn: func(
			context.Context,
			kubesphereextension.ListOptions,
		) (kubesphereextension.ListResult, error) {
			return kubesphereextension.ListResult{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "list"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, sentinel) ||
		!strings.Contains(err.Error(), "write extension list output") {
		t.Fatalf("Execute() error = %v", err)
	}
}
