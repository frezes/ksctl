package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type OperationKind string

const (
	OperationInstall   OperationKind = "install"
	OperationUpgrade   OperationKind = "upgrade"
	OperationConfigure OperationKind = "configure"
	OperationUninstall OperationKind = "uninstall"
)

type Operation struct {
	Kind          OperationKind
	Name          string
	TargetVersion string
	Baseline      Object[InstallPlan]
	expectation   waitExpectation
}

type InstallOptions struct {
	Version   string
	Config    *string
	Clusters  []string
	Overrides map[string]string
}

type UpgradeOptions struct {
	Version string
	Changes PlanChanges
}

func (s *Service) Install(
	ctx context.Context,
	name string,
	options InstallOptions,
) (Operation, error) {
	if err := validatePathName("extension", name); err != nil {
		return Operation{}, err
	}
	if strings.TrimSpace(options.Version) == "" {
		return Operation{}, fmt.Errorf("exact extension version is required")
	}
	if _, err := s.client.GetExtension(ctx, name); err != nil {
		return Operation{}, fmt.Errorf("get extension %q: %w", name, err)
	}
	version, err := s.exactVersion(ctx, name, options.Version)
	if err != nil {
		return Operation{}, err
	}

	config := ""
	if options.Config != nil {
		config, err = NormalizeYAML("global configuration", *options.Config)
		if err != nil {
			return Operation{}, err
		}
	}
	scheduling, err := BuildInstallScheduling(
		version.Value.Spec.InstallationMode,
		options.Clusters,
		options.Overrides,
	)
	if err != nil {
		return Operation{}, err
	}
	if _, err := s.CheckDependencies(ctx, version.Value); err != nil {
		return Operation{}, err
	}

	if _, err := s.client.GetInstallPlan(ctx, name); err == nil {
		return Operation{}, fmt.Errorf(
			"install plan %q already exists; use extension upgrade or extension configure",
			name,
		)
	} else if !apierrors.IsNotFound(err) {
		return Operation{}, fmt.Errorf("check install plan %q: %w", name, err)
	}

	plan := InstallPlan{
		APIVersion: "kubesphere.io/v1alpha1",
		Kind:       "InstallPlan",
		Metadata:   ObjectMeta{Name: name},
		Spec: InstallPlanSpec{
			Extension:         ExtensionRef{Name: name, Version: options.Version},
			Enabled:           true,
			UpgradeStrategy:   "Manual",
			Config:            config,
			ClusterScheduling: scheduling,
		},
	}
	created, err := s.client.CreateInstallPlan(ctx, plan)
	if err != nil {
		return Operation{}, fmt.Errorf("install extension %q: %w", name, err)
	}
	return Operation{
		Kind:          OperationInstall,
		Name:          name,
		TargetVersion: options.Version,
		Baseline:      created,
		expectation: installWaitExpectation(
			created.Value,
			options.Version,
			scheduling,
		),
	}, nil
}

func (s *Service) Upgrade(
	ctx context.Context,
	name string,
	options UpgradeOptions,
) (Operation, error) {
	if err := validatePathName("extension", name); err != nil {
		return Operation{}, err
	}
	if strings.TrimSpace(options.Version) == "" {
		return Operation{}, fmt.Errorf("exact extension version is required")
	}
	current, err := s.client.GetInstallPlan(ctx, name)
	if err != nil {
		return Operation{}, fmt.Errorf("get install plan %q: %w", name, err)
	}
	if err := ensurePlanIdentity(name, current.Value); err != nil {
		return Operation{}, err
	}
	target, err := s.exactVersion(ctx, name, options.Version)
	if err != nil {
		return Operation{}, err
	}
	if _, err := s.CheckDependencies(ctx, target.Value); err != nil {
		return Operation{}, err
	}
	if err := ensureMutable(current.Value); err != nil {
		return Operation{}, err
	}
	if current.Value.Spec.Extension.Version == options.Version {
		return Operation{}, fmt.Errorf(
			"extension %q already targets version %q; use extension configure",
			name,
			options.Version,
		)
	}

	specPatch, summary, err := BuildSpecPatch(
		current.Value,
		target.Value.Spec.InstallationMode,
		options.Changes,
	)
	if err != nil {
		return Operation{}, err
	}
	if err := requireFailedPlanConfigChange(current.Value, summary); err != nil {
		return Operation{}, err
	}
	if current.Value.Metadata.ResourceVersion == "" {
		return Operation{}, fmt.Errorf(
			"install plan %q response is missing metadata.resourceVersion",
			name,
		)
	}
	specPatch["extension"] = map[string]any{"version": options.Version}
	patch, err := encodePlanPatch(
		current.Value.Metadata.ResourceVersion,
		specPatch,
	)
	if err != nil {
		return Operation{}, fmt.Errorf("encode install plan %q patch: %w", name, err)
	}
	updated, err := s.client.PatchInstallPlan(ctx, name, patch)
	if err != nil {
		return Operation{}, fmt.Errorf("upgrade extension %q: %w", name, err)
	}
	return Operation{
		Kind:          OperationUpgrade,
		Name:          name,
		TargetVersion: options.Version,
		Baseline:      updated,
		expectation: upgradeWaitExpectation(
			updated.Value,
			options.Version,
			summary.ResultScheduling,
		),
	}, nil
}

func (s *Service) Configure(
	ctx context.Context,
	name string,
	changes PlanChanges,
) (Operation, error) {
	if err := validatePathName("extension", name); err != nil {
		return Operation{}, err
	}
	current, err := s.client.GetInstallPlan(ctx, name)
	if err != nil {
		return Operation{}, fmt.Errorf("get install plan %q: %w", name, err)
	}
	if err := ensurePlanIdentity(name, current.Value); err != nil {
		return Operation{}, err
	}
	if err := ensureMutable(current.Value); err != nil {
		return Operation{}, err
	}
	version, err := s.exactVersion(
		ctx,
		name,
		current.Value.Spec.Extension.Version,
	)
	if err != nil {
		return Operation{}, err
	}
	specPatch, summary, err := BuildSpecPatch(
		current.Value,
		version.Value.Spec.InstallationMode,
		changes,
	)
	if err != nil {
		return Operation{}, err
	}
	if !summary.Changed() {
		return Operation{}, fmt.Errorf(
			"extension %q configuration and scheduling are unchanged",
			name,
		)
	}
	if err := requireFailedPlanConfigChange(current.Value, summary); err != nil {
		return Operation{}, err
	}
	if current.Value.Metadata.ResourceVersion == "" {
		return Operation{}, fmt.Errorf(
			"install plan %q response is missing metadata.resourceVersion",
			name,
		)
	}
	patch, err := encodePlanPatch(
		current.Value.Metadata.ResourceVersion,
		specPatch,
	)
	if err != nil {
		return Operation{}, fmt.Errorf("encode install plan %q patch: %w", name, err)
	}
	updated, err := s.client.PatchInstallPlan(ctx, name, patch)
	if err != nil {
		return Operation{}, fmt.Errorf("configure extension %q: %w", name, err)
	}
	return Operation{
		Kind:          OperationConfigure,
		Name:          name,
		TargetVersion: current.Value.Spec.Extension.Version,
		Baseline:      updated,
		expectation: configureWaitExpectation(
			current.Value,
			updated.Value,
			current.Value.Spec.Extension.Version,
			changes,
			summary,
		),
	}, nil
}

func (s *Service) Uninstall(ctx context.Context, name string) (Operation, error) {
	if err := validatePathName("extension", name); err != nil {
		return Operation{}, err
	}
	current, err := s.client.GetInstallPlan(ctx, name)
	if err != nil {
		return Operation{}, fmt.Errorf("get install plan %q: %w", name, err)
	}
	if err := ensurePlanIdentity(name, current.Value); err != nil {
		return Operation{}, err
	}
	if err := s.client.DeleteInstallPlan(ctx, name); err != nil {
		return Operation{}, fmt.Errorf("uninstall extension %q: %w", name, err)
	}
	return Operation{
		Kind:          OperationUninstall,
		Name:          name,
		TargetVersion: current.Value.Spec.Extension.Version,
		Baseline:      current,
	}, nil
}

func ensurePlanIdentity(name string, plan InstallPlan) error {
	if plan.Metadata.Name != name {
		return fmt.Errorf(
			"install plan %q response has metadata.name %q",
			name,
			plan.Metadata.Name,
		)
	}
	if plan.Spec.Extension.Name != name {
		return fmt.Errorf(
			"install plan %q references extension %q",
			name,
			plan.Spec.Extension.Name,
		)
	}
	return nil
}

func ensureMutable(plan InstallPlan) error {
	if plan.Metadata.DeletionTimestamp != nil {
		return fmt.Errorf("install plan %q is being deleted", plan.Metadata.Name)
	}
	switch plan.Status.State {
	case "Installing", "Upgrading", "Uninstalling":
		return fmt.Errorf(
			"install plan %q is currently %s",
			plan.Metadata.Name,
			plan.Status.State,
		)
	}
	return nil
}

func failedHostState(state string) bool {
	return state == "InstallFailed" || state == "UpgradeFailed"
}

func requireFailedPlanConfigChange(
	plan InstallPlan,
	summary ChangeSummary,
) error {
	if failedHostState(plan.Status.State) && !summary.ConfigChanged {
		return fmt.Errorf(
			"install plan %q is %s; corrected global configuration is required before the controller can retry",
			plan.Metadata.Name,
			plan.Status.State,
		)
	}
	return nil
}

func encodePlanPatch(resourceVersion string, spec map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": resourceVersion},
		"spec":     spec,
	})
}
