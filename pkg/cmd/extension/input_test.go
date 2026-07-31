package extension

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	kubesphereextension "kubesphere.io/ksctl/pkg/kubesphere/extension"
)

func writeInputFile(t testing.TB, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return path
}

func TestInputReadsFileAndStdinWithoutChangingBytes(t *testing.T) {
	fileValue := "key: value\r\nsecond: true\n"
	path := writeInputFile(t, "config.yaml", fileValue)

	got, err := readInput(path, strings.NewReader("ignored"))
	if err != nil {
		t.Fatalf("readInput(file) error = %v", err)
	}
	if got != fileValue {
		t.Fatalf("readInput(file) = %q, want %q", got, fileValue)
	}

	stdinValue := "stdin: exact\r\n"
	got, err = readInput("-", strings.NewReader(stdinValue))
	if err != nil {
		t.Fatalf("readInput(stdin) error = %v", err)
	}
	if got != stdinValue {
		t.Fatalf("readInput(stdin) = %q, want %q", got, stdinValue)
	}
}

func TestInputOverrideSplitsAtFirstEquals(t *testing.T) {
	path := writeInputFile(t, "values=production.yaml", "enabled: true\n")
	streams, _, _ := bufferedStreams()
	var got kubesphereextension.InstallOptions
	service := &fakeService{
		installFn: func(
			_ context.Context,
			_ string,
			options kubesphereextension.InstallOptions,
		) (kubesphereextension.Operation, error) {
			got = options
			return kubesphereextension.Operation{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension",
			"install",
			"demo",
			"--version",
			"1.2.1",
			"--clusters",
			"member-a",
			"--override",
			"member-a=" + path,
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Overrides["member-a"] != "enabled: true\n" {
		t.Fatalf("options = %#v", got)
	}
}

func TestInputValidationFailsBeforeFactoryAndDoesNotLeakContent(t *testing.T) {
	secret := "password: top-secret"
	secretPath := writeInputFile(t, "secret.yaml", secret)
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "malformed override",
			args: []string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--clusters", "member-a",
				"--override", "member-a",
			},
			want: "CLUSTER=FILE",
		},
		{
			name: "empty override cluster",
			args: []string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--clusters", "member-a",
				"--override", "=" + secretPath,
			},
			want: "CLUSTER",
		},
		{
			name: "duplicate override",
			args: []string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--clusters", "member-a",
				"--override", "member-a=" + secretPath,
				"--override", "member-a=" + secretPath,
			},
			want: "more than once",
		},
		{
			name: "multiple stdin consumers",
			args: []string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--config", "-",
				"--clusters", "member-a",
				"--override", "member-a=-",
			},
			want: "stdin",
		},
		{
			name: "unreadable file",
			args: []string{
				"extension", "install", "demo",
				"--version", "1.2.1",
				"--config", missingPath,
			},
			want: "open configuration input",
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
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaked input content: %v", err)
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

func TestInputRejectsSetAndRemoveForSameCluster(t *testing.T) {
	path := writeInputFile(t, "values.yaml", "enabled: true\n")
	streams, _, _ := bufferedStreams()
	called := false
	err := executeExtensionCommand(
		t,
		[]string{
			"extension", "configure", "demo",
			"--clusters", "member-a",
			"--override", "member-a=" + path,
			"--remove-override", "member-a",
		},
		streams,
		func() (Service, error) {
			called = true
			return &fakeService{}, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "both set and removed") {
		t.Fatalf("Execute() error = %v", err)
	}
	if called {
		t.Fatal("service factory was called")
	}
}

func TestInputPlanChangesUseFlagPresence(t *testing.T) {
	path := writeInputFile(t, "values.yaml", "enabled: true\n")
	streams, _, _ := bufferedStreams()
	var got kubesphereextension.PlanChanges
	service := &fakeService{
		configureFn: func(
			_ context.Context,
			_ string,
			changes kubesphereextension.PlanChanges,
		) (kubesphereextension.Operation, error) {
			got = changes
			return kubesphereextension.Operation{}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension", "configure", "demo",
			"--config", path,
			"--clusters", "member-a,member-b",
			"--remove-override", "member-b",
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := kubesphereextension.PlanChanges{
		Config: kubesphereextension.StringChange{
			Mode:  kubesphereextension.Replace,
			Value: "enabled: true\n",
		},
		Scheduling: kubesphereextension.SchedulingChange{
			Mode:            kubesphereextension.Replace,
			Clusters:        []string{"member-a", "member-b"},
			SetOverrides:    map[string]string{},
			RemoveOverrides: []string{"member-b"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %#v, want %#v", got, want)
	}
}
