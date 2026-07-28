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

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestLifecycleFlagValidationBeforeFactory(t *testing.T) {
	configPath := writeInputFile(t, "config.yaml", "key: value\n")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "install version required",
			args: []string{"extension", "install", "demo"},
			want: "--version",
		},
		{
			name: "install whitespace version",
			args: []string{"extension", "install", "demo", "--version", "  "},
			want: "--version",
		},
		{
			name: "upgrade version required",
			args: []string{"extension", "upgrade", "demo"},
			want: "--version",
		},
		{
			name: "configure change required",
			args: []string{"extension", "configure", "demo"},
			want: "at least one",
		},
		{
			name: "config clear conflict",
			args: []string{
				"extension", "configure", "demo",
				"--config", configPath,
				"--clear-config",
			},
			want: "none of the others",
		},
		{
			name: "clear scheduling clusters conflict",
			args: []string{
				"extension", "configure", "demo",
				"--clusters", "member-a",
				"--clear-cluster-scheduling",
			},
			want: "none of the others",
		},
		{
			name: "clear scheduling override conflict",
			args: []string{
				"extension", "configure", "demo",
				"--override", "member-a=" + configPath,
				"--clear-cluster-scheduling",
			},
			want: "none of the others",
		},
		{
			name: "clear scheduling remove conflict",
			args: []string{
				"extension", "configure", "demo",
				"--remove-override", "member-a",
				"--clear-cluster-scheduling",
			},
			want: "none of the others",
		},
		{
			name: "wait timeout without wait",
			args: []string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--wait-timeout", "1m",
			},
			want: "--wait-timeout requires --wait",
		},
		{
			name: "zero wait timeout",
			args: []string{
				"extension", "uninstall", "demo",
				"--wait",
				"--wait-timeout", "0",
			},
			want: "positive",
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

func TestInstallExposesNoClearFlags(t *testing.T) {
	command := NewCommand(
		"ksctl",
		genericiooptions.IOStreams{
			In:     strings.NewReader(""),
			Out:    io.Discard,
			ErrOut: io.Discard,
		},
		func() (Service, error) { return &fakeService{}, nil },
	)
	install, _, err := command.Find([]string{"install"})
	if err != nil {
		t.Fatalf("Find(install) error = %v", err)
	}
	for _, name := range []string{
		"clear-config",
		"remove-override",
		"clear-cluster-scheduling",
	} {
		if install.Flags().Lookup(name) != nil {
			t.Fatalf("install unexpectedly exposes --%s", name)
		}
	}
}

func TestInstallDefaultsToAsyncAndPassesExactInputs(t *testing.T) {
	configPath := writeInputFile(t, "config.yaml", "key: value\r\n")
	overridePath := writeInputFile(t, "override.yaml", "key: member\n")
	streams, out, errOut := bufferedStreams()
	var gotName string
	var gotOptions internalextension.InstallOptions
	waitCalled := false
	service := &fakeService{
		installFn: func(
			_ context.Context,
			name string,
			options internalextension.InstallOptions,
		) (internalextension.Operation, error) {
			gotName, gotOptions = name, options
			return internalextension.Operation{
				Kind:          internalextension.OperationInstall,
				Name:          name,
				TargetVersion: options.Version,
			}, nil
		},
		waitFn: func(
			context.Context,
			internalextension.Operation,
			internalextension.PollOptions,
		) (internalextension.WaitResult, error) {
			waitCalled = true
			return internalextension.WaitResult{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension", "install", "demo",
			"--version", "v1.2.1+build",
			"--config", configPath,
			"--clusters", "member-a,member-b",
			"--override", "member-a=" + overridePath,
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	config := "key: value\r\n"
	want := internalextension.InstallOptions{
		Version:   "v1.2.1+build",
		Config:    &config,
		Clusters:  []string{"member-a", "member-b"},
		Overrides: map[string]string{"member-a": "key: member\n"},
	}
	if gotName != "demo" || !reflect.DeepEqual(gotOptions, want) {
		t.Fatalf("Install(%q, %#v), want %#v", gotName, gotOptions, want)
	}
	if waitCalled {
		t.Fatal("Wait was called without --wait")
	}
	if out.String() != "extension/demo install requested\n" {
		t.Fatalf("stdout = %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestUpgradeAndConfigurePassPlanChanges(t *testing.T) {
	t.Run("upgrade clear fields", func(t *testing.T) {
		streams, out, _ := bufferedStreams()
		var got internalextension.UpgradeOptions
		service := &fakeService{
			upgradeFn: func(
				_ context.Context,
				_ string,
				options internalextension.UpgradeOptions,
			) (internalextension.Operation, error) {
				got = options
				return internalextension.Operation{}, nil
			},
		}
		err := executeExtensionCommand(
			t,
			[]string{
				"extension", "upgrade", "demo",
				"--version", "2.0.0",
				"--clear-config",
				"--clear-cluster-scheduling",
			},
			streams,
			func() (Service, error) { return service, nil },
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		want := internalextension.UpgradeOptions{
			Version: "2.0.0",
			Changes: internalextension.PlanChanges{
				Config: internalextension.StringChange{
					Mode: internalextension.Clear,
				},
				Scheduling: internalextension.SchedulingChange{
					Mode:            internalextension.Clear,
					SetOverrides:    map[string]string{},
					RemoveOverrides: []string{},
				},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("options = %#v, want %#v", got, want)
		}
		if out.String() != "extension/demo upgrade requested\n" {
			t.Fatalf("stdout = %q", out.String())
		}
	})

	t.Run("configure remove override", func(t *testing.T) {
		streams, out, _ := bufferedStreams()
		var got internalextension.PlanChanges
		service := &fakeService{
			configureFn: func(
				_ context.Context,
				_ string,
				changes internalextension.PlanChanges,
			) (internalextension.Operation, error) {
				got = changes
				return internalextension.Operation{}, nil
			},
		}
		err := executeExtensionCommand(
			t,
			[]string{
				"extension", "configure", "demo",
				"--remove-override", "member-a",
			},
			streams,
			func() (Service, error) { return service, nil },
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if !reflect.DeepEqual(
			got.Scheduling.RemoveOverrides,
			[]string{"member-a"},
		) {
			t.Fatalf("changes = %#v", got)
		}
		if out.String() != "extension/demo configuration requested\n" {
			t.Fatalf("stdout = %q", out.String())
		}
	})
}

func TestUninstallIsDirectAndAsyncByDefault(t *testing.T) {
	streams, out, _ := bufferedStreams()
	var gotName string
	service := &fakeService{
		uninstallFn: func(
			_ context.Context,
			name string,
		) (internalextension.Operation, error) {
			gotName = name
			return internalextension.Operation{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "uninstall", "demo"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotName != "demo" {
		t.Fatalf("Uninstall(%q)", gotName)
	}
	if out.String() != "extension/demo uninstall requested\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestLifecycleWaitIsExplicitAndPrintsProgressThenSuccess(t *testing.T) {
	for _, test := range []struct {
		name        string
		args        []string
		operation   internalextension.OperationKind
		finalOutput string
		service     func(*testing.T) *fakeService
	}{
		{
			name:        "install",
			args:        []string{"extension", "install", "demo", "--version", "1.2.1"},
			operation:   internalextension.OperationInstall,
			finalOutput: "extension/demo installed\n",
			service: func(t *testing.T) *fakeService {
				return &fakeService{installFn: func(
					context.Context,
					string,
					internalextension.InstallOptions,
				) (internalextension.Operation, error) {
					return internalextension.Operation{
						Kind: internalextension.OperationInstall,
						Name: "demo",
					}, nil
				}}
			},
		},
		{
			name:        "upgrade",
			args:        []string{"extension", "upgrade", "demo", "--version", "1.2.1"},
			operation:   internalextension.OperationUpgrade,
			finalOutput: "extension/demo upgraded\n",
			service: func(t *testing.T) *fakeService {
				return &fakeService{upgradeFn: func(
					context.Context,
					string,
					internalextension.UpgradeOptions,
				) (internalextension.Operation, error) {
					return internalextension.Operation{
						Kind: internalextension.OperationUpgrade,
						Name: "demo",
					}, nil
				}}
			},
		},
		{
			name:        "configure",
			args:        []string{"extension", "configure", "demo", "--clear-config"},
			operation:   internalextension.OperationConfigure,
			finalOutput: "extension/demo configured\n",
			service: func(t *testing.T) *fakeService {
				return &fakeService{configureFn: func(
					context.Context,
					string,
					internalextension.PlanChanges,
				) (internalextension.Operation, error) {
					return internalextension.Operation{
						Kind: internalextension.OperationConfigure,
						Name: "demo",
					}, nil
				}}
			},
		},
		{
			name:        "uninstall",
			args:        []string{"extension", "uninstall", "demo"},
			operation:   internalextension.OperationUninstall,
			finalOutput: "extension/demo uninstalled\n",
			service: func(t *testing.T) *fakeService {
				return &fakeService{uninstallFn: func(
					context.Context,
					string,
				) (internalextension.Operation, error) {
					return internalextension.Operation{
						Kind: internalextension.OperationUninstall,
						Name: "demo",
					}, nil
				}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			streams, out, errOut := bufferedStreams()
			service := test.service(t)
			service.waitFn = func(
				_ context.Context,
				operation internalextension.Operation,
				options internalextension.PollOptions,
			) (internalextension.WaitResult, error) {
				if operation.Kind != test.operation ||
					options.Timeout != 2*time.Minute {
					t.Fatalf(
						"Wait(operation=%#v, timeout=%s)",
						operation,
						options.Timeout,
					)
				}
				for _, event := range []internalextension.StateEvent{
					{State: ""},
					{State: "Installing"},
					{State: "Installed"},
				} {
					if err := options.OnState(event); err != nil {
						return internalextension.WaitResult{}, err
					}
				}
				return internalextension.WaitResult{}, nil
			}
			args := append(
				append([]string(nil), test.args...),
				"--wait",
				"--wait-timeout",
				"2m",
			)
			err := executeExtensionCommand(
				t,
				args,
				streams,
				func() (Service, error) { return service, nil },
			)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if out.String() != test.finalOutput ||
				strings.Contains(out.String(), "requested") {
				t.Fatalf("stdout = %q", out.String())
			}
			if got, want := errOut.String(),
				"extension/demo state: <pending>\n"+
					"extension/demo state: Installing\n"+
					"extension/demo state: Installed\n"; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestLifecycleServiceErrorsEmitNoSuccessOutput(t *testing.T) {
	sentinel := errors.New("submission failed")
	streams, out, _ := bufferedStreams()
	service := &fakeService{
		installFn: func(
			context.Context,
			string,
			internalextension.InstallOptions,
		) (internalextension.Operation, error) {
			return internalextension.Operation{}, sentinel
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "install", "demo", "--version", "1.2.1"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestLifecycleWrapsOutputAndProgressWriterFailures(t *testing.T) {
	sentinel := errors.New("closed")
	t.Run("async output", func(t *testing.T) {
		service := &fakeService{installFn: func(
			context.Context,
			string,
			internalextension.InstallOptions,
		) (internalextension.Operation, error) {
			return internalextension.Operation{}, nil
		}}
		err := executeExtensionCommand(
			t,
			[]string{"extension", "install", "demo", "--version", "1.2.1"},
			genericiooptions.IOStreams{
				In:     strings.NewReader(""),
				Out:    failingWriter{err: sentinel},
				ErrOut: io.Discard,
			},
			func() (Service, error) { return service, nil },
		)
		if !errors.Is(err, sentinel) ||
			!strings.Contains(err.Error(), "write extension install output") {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	t.Run("wait progress", func(t *testing.T) {
		var out bytes.Buffer
		service := &fakeService{
			installFn: func(
				context.Context,
				string,
				internalextension.InstallOptions,
			) (internalextension.Operation, error) {
				return internalextension.Operation{
					Kind: internalextension.OperationInstall,
					Name: "demo",
				}, nil
			},
			waitFn: func(
				_ context.Context,
				_ internalextension.Operation,
				options internalextension.PollOptions,
			) (internalextension.WaitResult, error) {
				return internalextension.WaitResult{}, options.OnState(
					internalextension.StateEvent{State: "Installing"},
				)
			},
		}
		err := executeExtensionCommand(
			t,
			[]string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--wait",
			},
			genericiooptions.IOStreams{
				In:     strings.NewReader(""),
				Out:    &out,
				ErrOut: failingWriter{err: sentinel},
			},
			func() (Service, error) { return service, nil },
		)
		if !errors.Is(err, sentinel) ||
			!strings.Contains(err.Error(), "write extension install progress") {
			t.Fatalf("Execute() error = %v", err)
		}
		if out.Len() != 0 {
			t.Fatalf("stdout = %q", out.String())
		}
	})
}
