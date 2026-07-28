package extension

import (
	"context"
	"errors"
	"reflect"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func dependencyVersion(dependencies ...ExternalDependency) ExtensionVersion {
	return ExtensionVersion{
		Metadata: ObjectMeta{Name: "demo-1-0-0"},
		Spec: ExtensionVersionSpec{
			Version:              "1.0.0",
			ExternalDependencies: dependencies,
		},
	}
}

func TestServiceCheckDependenciesAcceptsInstalledCompatibleExtension(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := planForTest("logging", "1.0.0", "Installed")
	plan.Status.Version = "1.4.2"
	client.planObjects["logging"] = objectForTest(t, plan)

	checks, err := NewService(client).CheckDependencies(
		context.Background(),
		dependencyVersion(ExternalDependency{
			Name:     "logging",
			Version:  "^1.2.0",
			Required: true,
		}),
	)
	if err != nil {
		t.Fatalf("CheckDependencies() error = %v", err)
	}
	if len(checks) != 1 || checks[0].Code != DependencySatisfied ||
		checks[0].ObservedVersion != "1.4.2" {
		t.Fatalf("checks = %#v", checks)
	}
}

func TestServiceCheckDependenciesReportsAllRequiredFailuresInDeclarationOrder(t *testing.T) {
	client := newFakeAPIClient(t)
	failed := planForTest("failed", "1.0.0", "InstallFailed")
	client.planObjects["failed"] = objectForTest(t, failed)
	incompatible := planForTest("incompatible", "1.0.0", "Installed")
	client.planObjects["incompatible"] = objectForTest(t, incompatible)

	checks, err := NewService(client).CheckDependencies(
		context.Background(),
		dependencyVersion(
			ExternalDependency{Name: "missing", Version: ">=1", Required: true},
			ExternalDependency{Name: "failed", Version: ">=1", Required: true},
			ExternalDependency{Name: "incompatible", Version: ">=2", Required: true},
			ExternalDependency{Name: "unsupported", Type: "service", Version: "1.x", Required: true},
		),
	)
	var validation *DependencyValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("CheckDependencies() error = %v, want DependencyValidationError", err)
	}
	var codes []DependencyCode
	var names []string
	for _, check := range checks {
		codes = append(codes, check.Code)
		names = append(names, check.Dependency.Name)
	}
	if want := []string{"missing", "failed", "incompatible", "unsupported"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if want := []DependencyCode{
		DependencyMissing,
		DependencyNotInstalled,
		DependencyIncompatible,
		DependencyUnsupportedType,
	}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
	if len(validation.Failures) != 4 {
		t.Fatalf("failures = %#v", validation.Failures)
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("aggregate error = %v, want recognizable NotFound", err)
	}
}

func TestServiceCheckDependenciesUsesStatusVersionBeforeSpecVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := planForTest("logging", "9.0.0", "Installed")
	plan.Status.Version = "1.3.0"
	client.planObjects["logging"] = objectForTest(t, plan)

	checks, err := NewService(client).CheckDependencies(
		context.Background(),
		dependencyVersion(ExternalDependency{
			Name:     "logging",
			Version:  "1.x",
			Required: true,
		}),
	)
	if err != nil {
		t.Fatalf("CheckDependencies() error = %v", err)
	}
	if checks[0].ObservedVersion != "1.3.0" {
		t.Fatalf("observed version = %q", checks[0].ObservedVersion)
	}
}

func TestServiceCheckDependenciesFallsBackToSpecOnlyForSuccessfulState(t *testing.T) {
	t.Run("successful", func(t *testing.T) {
		client := newFakeAPIClient(t)
		plan := planForTest("logging", "1.5.0", "Upgraded")
		plan.Status.Version = ""
		client.planObjects["logging"] = objectForTest(t, plan)

		checks, err := NewService(client).CheckDependencies(
			context.Background(),
			dependencyVersion(ExternalDependency{
				Name:     "logging",
				Version:  "1.x",
				Required: true,
			}),
		)
		if err != nil {
			t.Fatalf("CheckDependencies() error = %v", err)
		}
		if checks[0].ObservedVersion != "1.5.0" {
			t.Fatalf("observed version = %q", checks[0].ObservedVersion)
		}
	})

	t.Run("failed", func(t *testing.T) {
		client := newFakeAPIClient(t)
		plan := planForTest("logging", "1.5.0", "InstallFailed")
		plan.Status.Version = ""
		client.planObjects["logging"] = objectForTest(t, plan)

		checks, err := NewService(client).CheckDependencies(
			context.Background(),
			dependencyVersion(ExternalDependency{
				Name:     "logging",
				Version:  "1.x",
				Required: true,
			}),
		)
		if err == nil || checks[0].ObservedVersion != "" ||
			checks[0].Code != DependencyNotInstalled {
			t.Fatalf("checks = %#v, error = %v", checks, err)
		}
	})
}

func TestServiceCheckDependenciesDoesNotBlockOptionalFailures(t *testing.T) {
	client := newFakeAPIClient(t)
	checks, err := NewService(client).CheckDependencies(
		context.Background(),
		dependencyVersion(
			ExternalDependency{Name: "missing", Version: ">=1"},
			ExternalDependency{Name: "unknown", Type: "service", Version: "1.x"},
		),
	)
	if err != nil {
		t.Fatalf("CheckDependencies() error = %v", err)
	}
	if len(checks) != 2 || checks[0].Code != DependencyMissing ||
		checks[1].Code != DependencyUnsupportedType {
		t.Fatalf("checks = %#v", checks)
	}
	if len(client.createdPlans) != 0 {
		t.Fatalf("created plans = %#v", client.createdPlans)
	}
}

func TestServiceCheckDependenciesSupportsMastermindsConstraints(t *testing.T) {
	for _, constraint := range []string{
		"^1.2.0",
		">=1.2 <2.0",
		"1.x",
		">=1 || <0.5",
	} {
		t.Run(constraint, func(t *testing.T) {
			client := newFakeAPIClient(t)
			plan := planForTest("logging", "1.4.0", "Installed")
			client.planObjects["logging"] = objectForTest(t, plan)

			checks, err := NewService(client).CheckDependencies(
				context.Background(),
				dependencyVersion(ExternalDependency{
					Name:     "logging",
					Version:  constraint,
					Required: true,
				}),
			)
			if err != nil || checks[0].Code != DependencySatisfied {
				t.Fatalf("constraint %q checks = %#v, error = %v", constraint, checks, err)
			}
		})
	}
}
