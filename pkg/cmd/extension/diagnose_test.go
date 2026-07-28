package extension

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestDiagnosePassesTargetAndPrintsChecksInOrder(t *testing.T) {
	streams, out, _ := bufferedStreams()
	var gotName string
	var gotOptions internalextension.DiagnoseOptions
	service := &fakeService{
		diagnoseFn: func(
			_ context.Context,
			name string,
			options internalextension.DiagnoseOptions,
		) (internalextension.Diagnosis, error) {
			gotName, gotOptions = name, options
			return internalextension.Diagnosis{Checks: []internalextension.DiagnosticCheck{
				{
					Name:    "extension",
					Status:  internalextension.DiagnosticOK,
					Message: "extension exists",
				},
				{
					Name:    "job",
					Status:  internalextension.DiagnosticWarn,
					Message: "Job was removed by TTL",
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
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotName != "demo" ||
		gotOptions.TargetCluster != "member-a" {
		t.Fatalf("Diagnose(%q, %#v)", gotName, gotOptions)
	}
	if got, want := out.String(),
		"CHECK      STATUS  MESSAGE\n"+
			"extension  OK      extension exists\n"+
			"job        WARN    Job was removed by TTL\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestDiagnosePrintsChecksBeforeDiagnosisFailure(t *testing.T) {
	streams, out, _ := bufferedStreams()
	service := &fakeService{
		diagnoseFn: func(
			context.Context,
			string,
			internalextension.DiagnoseOptions,
		) (internalextension.Diagnosis, error) {
			return internalextension.Diagnosis{Checks: []internalextension.DiagnosticCheck{{
				Name:    "install-plan",
				Status:  internalextension.DiagnosticError,
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
	if !errors.Is(err, internalextension.ErrDiagnosisFailed) {
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
			internalextension.DiagnoseOptions,
		) (internalextension.Diagnosis, error) {
			return internalextension.Diagnosis{Checks: []internalextension.DiagnosticCheck{{
				Name:    "extension",
				Status:  internalextension.DiagnosticOK,
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
	if !strings.Contains(out.String(), "extension  OK") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestDiagnoseValidatesTargetBeforeFactory(t *testing.T) {
	streams, out, _ := bufferedStreams()
	called := false
	err := executeExtensionCommand(
		t,
		[]string{
			"extension",
			"diagnose",
			"demo",
			"--target-cluster",
			"bad/name",
		},
		streams,
		func() (Service, error) {
			called = true
			return &fakeService{}, nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid target cluster") {
		t.Fatalf("Execute() error = %v", err)
	}
	if called {
		t.Fatal("service factory was called")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestDiagnoseWrapsWriterError(t *testing.T) {
	sentinel := errors.New("closed")
	service := &fakeService{
		diagnoseFn: func(
			context.Context,
			string,
			internalextension.DiagnoseOptions,
		) (internalextension.Diagnosis, error) {
			return internalextension.Diagnosis{Checks: []internalextension.DiagnosticCheck{{
				Name:    "extension",
				Status:  internalextension.DiagnosticOK,
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
