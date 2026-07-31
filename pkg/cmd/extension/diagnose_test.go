package extension

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"k8s.io/cli-runtime/pkg/genericiooptions"
	kubesphereextension "kubesphere.io/ksctl/pkg/kubesphere/extension"
)

func TestDiagnosePassesTargetAndPrintsIssuesInOrder(t *testing.T) {
	streams, out, _ := bufferedStreams()
	var gotName string
	var gotOptions kubesphereextension.DiagnoseOptions
	service := &fakeService{
		diagnoseFn: func(
			_ context.Context,
			name string,
			options kubesphereextension.DiagnoseOptions,
		) (kubesphereextension.Diagnosis, error) {
			gotName, gotOptions = name, options
			return kubesphereextension.Diagnosis{Checks: []kubesphereextension.DiagnosticCheck{
				{
					Name:    "extension",
					Status:  kubesphereextension.DiagnosticOK,
					Message: "extension exists",
				},
				{
					Name:    "install-plan",
					Status:  kubesphereextension.DiagnosticInfo,
					Message: "install plan is active",
				},
				{
					Name:    "job",
					Status:  kubesphereextension.DiagnosticWarn,
					Message: "Job was removed by TTL",
				},
				{
					Name:    "workload",
					Status:  kubesphereextension.DiagnosticError,
					Message: "executor is unavailable",
				},
			}}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{
			"extension",
			"diagnose",
			"demo",
			"--target-cluster",
			"member-a",
		},
		streams,
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, kubesphereextension.ErrDiagnosisFailed) {
		t.Fatalf("Execute() error = %v, want ErrDiagnosisFailed", err)
	}
	if gotName != "demo" ||
		gotOptions.TargetCluster != "member-a" {
		t.Fatalf("Diagnose(%q, %#v)", gotName, gotOptions)
	}
	if got, want := out.String(),
		"CHECK     STATUS  MESSAGE\n"+
			"job       WARN    Job was removed by TTL\n"+
			"workload  ERROR   executor is unavailable\n"+
			"Summary: OK=1 INFO=1 WARN=1 ERROR=1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestDiagnoseHealthyDefaultPrintsSummaryOnly(t *testing.T) {
	streams, out, _ := bufferedStreams()
	service := &fakeService{diagnoseFn: func(
		context.Context,
		string,
		kubesphereextension.DiagnoseOptions,
	) (kubesphereextension.Diagnosis, error) {
		return kubesphereextension.Diagnosis{Checks: []kubesphereextension.DiagnosticCheck{
			{Name: "extension", Status: kubesphereextension.DiagnosticOK},
			{Name: "install-plan", Status: kubesphereextension.DiagnosticInfo},
		}}, nil
	}}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "diagnose", "demo"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got, want := out.String(),
		"extension/demo: healthy (2 checks passed)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestDiagnoseVerbosePrintsEveryCheck(t *testing.T) {
	streams, out, _ := bufferedStreams()
	service := &fakeService{diagnoseFn: func(
		context.Context,
		string,
		kubesphereextension.DiagnoseOptions,
	) (kubesphereextension.Diagnosis, error) {
		return kubesphereextension.Diagnosis{Checks: []kubesphereextension.DiagnosticCheck{
			{Name: "extension", Status: kubesphereextension.DiagnosticOK, Message: "exists"},
			{Name: "install-plan", Status: kubesphereextension.DiagnosticInfo, Message: "active"},
			{Name: "job", Status: kubesphereextension.DiagnosticWarn, Message: "expired"},
			{Name: "workload", Status: kubesphereextension.DiagnosticError, Message: "unavailable"},
		}}, nil
	}}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "diagnose", "demo", "--verbose"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, kubesphereextension.ErrDiagnosisFailed) {
		t.Fatalf("Execute() error = %v, want ErrDiagnosisFailed", err)
	}
	if got, want := out.String(),
		"CHECK         STATUS  MESSAGE\n"+
			"extension     OK      exists\n"+
			"install-plan  INFO    active\n"+
			"job           WARN    expired\n"+
			"workload      ERROR   unavailable\n"+
			"Summary: OK=1 INFO=1 WARN=1 ERROR=1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestDiagnosePrintsChecksBeforeDiagnosisFailure(t *testing.T) {
	streams, out, _ := bufferedStreams()
	service := &fakeService{
		diagnoseFn: func(
			context.Context,
			string,
			kubesphereextension.DiagnoseOptions,
		) (kubesphereextension.Diagnosis, error) {
			return kubesphereextension.Diagnosis{Checks: []kubesphereextension.DiagnosticCheck{{
				Name:    "install-plan",
				Status:  kubesphereextension.DiagnosticError,
				Message: "InstallFailed",
			}}}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "diagnose", "demo"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, kubesphereextension.ErrDiagnosisFailed) {
		t.Fatalf("Execute() error = %v, want ErrDiagnosisFailed", err)
	}
	if !strings.Contains(out.String(), "install-plan  ERROR") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestDiagnosePrintsAccumulatedChecksBeforeServiceError(t *testing.T) {
	sentinel := errors.New("pod request failed")
	streams, out, _ := bufferedStreams()
	service := &fakeService{
		diagnoseFn: func(
			context.Context,
			string,
			kubesphereextension.DiagnoseOptions,
		) (kubesphereextension.Diagnosis, error) {
			return kubesphereextension.Diagnosis{Checks: []kubesphereextension.DiagnosticCheck{{
				Name:    "extension",
				Status:  kubesphereextension.DiagnosticOK,
				Message: "extension exists",
			}}}, sentinel
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "diagnose", "demo"},
		streams,
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Execute() error = %v, want sentinel", err)
	}
	if got, want := out.String(),
		"Summary: OK=1 INFO=0 WARN=0 ERROR=0\n"+
			"extension/demo: diagnosis incomplete (1 checks completed)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestDiagnoseValidatesTargetBeforeFactory(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "malformed",
			args: []string{
				"extension",
				"diagnose",
				"demo",
				"--target-cluster",
				"bad/name",
			},
		},
		{
			name: "explicitly empty",
			args: []string{
				"extension",
				"diagnose",
				"demo",
				"--target-cluster=",
			},
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
			if err == nil ||
				!strings.Contains(err.Error(), "invalid target cluster") {
				t.Fatalf("Execute() error = %v", err)
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

func TestDiagnoseWrapsWriterError(t *testing.T) {
	sentinel := errors.New("closed")
	service := &fakeService{
		diagnoseFn: func(
			context.Context,
			string,
			kubesphereextension.DiagnoseOptions,
		) (kubesphereextension.Diagnosis, error) {
			return kubesphereextension.Diagnosis{Checks: []kubesphereextension.DiagnosticCheck{{
				Name:    "extension",
				Status:  kubesphereextension.DiagnosticOK,
				Message: "exists",
			}}}, nil
		},
	}
	err := executeExtensionCommand(
		t,
		[]string{"extension", "diagnose", "demo"},
		genericiooptions.IOStreams{
			In:     strings.NewReader(""),
			Out:    failingWriter{err: sentinel},
			ErrOut: io.Discard,
		},
		func() (Service, error) { return service, nil },
	)
	if !errors.Is(err, sentinel) ||
		!strings.Contains(err.Error(), "write extension diagnosis output") {
		t.Fatalf("Execute() error = %v", err)
	}
}
