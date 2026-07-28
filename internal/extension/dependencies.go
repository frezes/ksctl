package extension

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type DependencyCode string

const (
	DependencySatisfied         DependencyCode = "Satisfied"
	DependencyMissing           DependencyCode = "Missing"
	DependencyUnavailable       DependencyCode = "Unavailable"
	DependencyDeleting          DependencyCode = "Deleting"
	DependencyNotInstalled      DependencyCode = "NotInstalled"
	DependencyMissingVersion    DependencyCode = "MissingVersion"
	DependencyInvalidConstraint DependencyCode = "InvalidConstraint"
	DependencyInvalidVersion    DependencyCode = "InvalidVersion"
	DependencyIncompatible      DependencyCode = "Incompatible"
	DependencyUnsupportedType   DependencyCode = "UnsupportedType"
)

type DependencyCheck struct {
	Dependency      ExternalDependency
	Code            DependencyCode
	ObservedState   string
	ObservedVersion string
	Cause           error
}

type DependencyValidationError struct {
	Failures []DependencyCheck
}

func (e *DependencyValidationError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		state := valueOrNone(failure.ObservedState)
		version := valueOrNone(failure.ObservedVersion)
		parts = append(parts, fmt.Sprintf(
			"%s requires %q: %s (state=%s version=%s)",
			failure.Dependency.Name,
			failure.Dependency.Version,
			failure.Code,
			state,
			version,
		))
	}
	return "required extension dependencies are not satisfied: " +
		strings.Join(parts, "; ")
}

func (e *DependencyValidationError) Unwrap() []error {
	causes := make([]error, 0, len(e.Failures))
	for _, failure := range e.Failures {
		if failure.Cause != nil {
			causes = append(causes, failure.Cause)
		}
	}
	return causes
}

func valueOrNone(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
}

func (s *Service) CheckDependencies(
	ctx context.Context,
	version ExtensionVersion,
) ([]DependencyCheck, error) {
	checks := make(
		[]DependencyCheck,
		0,
		len(version.Spec.ExternalDependencies),
	)
	failures := make([]DependencyCheck, 0)
	for _, dependency := range version.Spec.ExternalDependencies {
		check := s.checkDependency(ctx, dependency)
		checks = append(checks, check)
		if dependency.Required && check.Code != DependencySatisfied {
			failures = append(failures, check)
		}
	}
	if len(failures) != 0 {
		return checks, &DependencyValidationError{Failures: failures}
	}
	return checks, nil
}

func (s *Service) checkDependency(
	ctx context.Context,
	dependency ExternalDependency,
) DependencyCheck {
	check := DependencyCheck{Dependency: dependency}
	dependencyType := dependency.Type
	if dependencyType == "" {
		dependencyType = "extension"
	}
	if dependencyType != "extension" {
		check.Code = DependencyUnsupportedType
		return check
	}
	if err := validatePathName("extension dependency", dependency.Name); err != nil {
		check.Code = DependencyUnavailable
		check.Cause = err
		return check
	}

	plan, err := s.client.GetInstallPlan(ctx, dependency.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			check.Code = DependencyMissing
		} else {
			check.Code = DependencyUnavailable
		}
		check.Cause = err
		return check
	}
	check.ObservedState = plan.Value.Status.State
	if plan.Value.Metadata.DeletionTimestamp != nil {
		check.Code = DependencyDeleting
		return check
	}
	if !dependencySuccessfulState(plan.Value.Status.State) {
		check.Code = DependencyNotInstalled
		return check
	}

	check.ObservedVersion = plan.Value.Status.Version
	if check.ObservedVersion == "" {
		check.ObservedVersion = plan.Value.Spec.Extension.Version
	}
	if check.ObservedVersion == "" {
		check.Code = DependencyMissingVersion
		return check
	}

	constraint, err := semver.NewConstraint(dependency.Version)
	if err != nil {
		check.Code = DependencyInvalidConstraint
		check.Cause = err
		return check
	}
	observed, err := semver.NewVersion(check.ObservedVersion)
	if err != nil {
		check.Code = DependencyInvalidVersion
		check.Cause = err
		return check
	}
	if !constraint.Check(observed) {
		check.Code = DependencyIncompatible
		return check
	}
	check.Code = DependencySatisfied
	return check
}

func dependencySuccessfulState(state string) bool {
	return state == "Installed" || state == "Upgraded"
}
