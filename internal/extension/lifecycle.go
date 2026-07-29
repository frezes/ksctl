package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
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

const installPlanProtectionFinalizer = "kubesphere.io/installplan-protection"

type Operation struct {
	Kind          OperationKind
	Name          string
	TargetVersion string
	Baseline      Object[InstallPlan]
	expectation   waitExpectation
	acceptedUID   string
	acceptedSpec  InstallPlanSpec
	hasAccepted   bool
}

type InstallOptions struct {
	Version     string
	Config      *string
	Clusters    []string
	AllClusters bool
	Overrides   map[string]string
}

type UpgradeOptions struct {
	Version         string
	Changes         PlanChanges
	RequireWaitable bool
}

func (s *Service) Install(
	ctx context.Context,
	name string,
	options InstallOptions,
) (Operation, error) {
	if err := validatePathName("extension", name); err != nil {
		return Operation{}, err
	}
	extension, err := s.client.GetExtension(ctx, name)
	if err != nil {
		return Operation{}, fmt.Errorf("get extension %q: %w", name, err)
	}
	selectedVersion := options.Version
	if strings.TrimSpace(selectedVersion) == "" {
		selectedVersion = extension.Value.Status.RecommendedVersion
		if strings.TrimSpace(selectedVersion) == "" {
			return Operation{}, fmt.Errorf(
				"extension %q has no recommended version; run extension versions %s",
				name,
				name,
			)
		}
	}
	options.Version = selectedVersion
	version, err := s.exactVersion(ctx, name, options.Version)
	if err != nil {
		return Operation{}, err
	}
	if options.AllClusters {
		if version.Value.Spec.InstallationMode != "Multicluster" {
			return Operation{}, fmt.Errorf(
				"extension version %q uses installation mode %q and does not accept --all-clusters",
				options.Version,
				valueOrNone(version.Value.Spec.InstallationMode),
			)
		}
		clusters, err := s.client.ListClusters(ctx)
		if err != nil {
			return Operation{}, err
		}
		options.Clusters = eligibleClusterNames(clusters)
		if len(options.Clusters) == 0 {
			return Operation{}, fmt.Errorf(
				"no ready, schedulable Clusters are available for --all-clusters",
			)
		}
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
	if err := ensureAcceptedPlan(
		name,
		plan.Spec,
		created.Value,
	); err != nil {
		return Operation{}, fmt.Errorf(
			"validate accepted install plan %q: %w",
			name,
			err,
		)
	}
	return Operation{
		Kind:          OperationInstall,
		Name:          name,
		TargetVersion: options.Version,
		Baseline:      created,
		expectation: installWaitExpectation(
			created.Value,
			options.Version,
		),
		acceptedUID:  created.Value.Metadata.UID,
		acceptedSpec: cloneInstallPlanSpec(created.Value.Spec),
		hasAccepted:  true,
	}, nil
}

func clusterCondition(
	cluster Cluster,
	conditionType string,
) (string, bool) {
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status, true
		}
	}
	return "", false
}

func eligibleClusterNames(clusters List[Cluster]) []string {
	names := make([]string, 0, len(clusters.Items))
	for _, item := range clusters.Items {
		cluster := item.Value
		if cluster.Metadata.DeletionTimestamp != nil {
			continue
		}
		ready, found := clusterCondition(cluster, "KSCoreReady")
		if !found || ready != "True" {
			continue
		}
		if schedulable, found := clusterCondition(
			cluster,
			"Schedulable",
		); found && schedulable == "False" {
			continue
		}
		names = append(names, cluster.Metadata.Name)
	}
	slices.Sort(names)
	return names
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
	if current.Value.Metadata.ResourceVersion == "" {
		return Operation{}, fmt.Errorf(
			"install plan %q response is missing metadata.resourceVersion",
			name,
		)
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
	if err := ensureClusterTargetsReady(
		current.Value,
		summary.ResultScheduling,
	); err != nil {
		return Operation{}, err
	}
	if options.RequireWaitable &&
		selectorOnlyPlacement(summary.ResultScheduling) {
		return Operation{}, fmt.Errorf(
			"--wait cannot track dynamic clusterSelector placement; omit --wait or replace it with --clusters",
		)
	}
	if failedHostState(current.Value.Status.State) &&
		!controllerWillRetry(
			current.Value,
			options.Version,
			summary.ResultConfig,
		) {
		return Operation{}, fmt.Errorf(
			"install plan %q is %s; a different target version or corrected global configuration is required before the controller can retry",
			name,
			current.Value.Status.State,
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
	expectedSpec := cloneInstallPlanSpec(current.Value.Spec)
	expectedSpec.Extension.Version = options.Version
	expectedSpec.UpgradeStrategy = "Manual"
	expectedSpec.Config = summary.ResultConfig
	expectedSpec.ClusterScheduling = cloneScheduling(
		summary.ResultScheduling,
	)
	if err := ensureAcceptedPlan(name, expectedSpec, updated.Value); err != nil {
		return Operation{}, fmt.Errorf(
			"validate accepted install plan %q: %w",
			name,
			err,
		)
	}
	summary.ResultConfig = updated.Value.Spec.Config
	summary.ResultScheduling = cloneScheduling(
		updated.Value.Spec.ClusterScheduling,
	)
	return Operation{
		Kind:          OperationUpgrade,
		Name:          name,
		TargetVersion: options.Version,
		Baseline:      updated,
		expectation: upgradeWaitExpectation(
			current.Value,
			updated.Value,
			options.Version,
			summary,
		),
		acceptedUID:  updated.Value.Metadata.UID,
		acceptedSpec: cloneInstallPlanSpec(updated.Value.Spec),
		hasAccepted:  true,
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
	if err := ensureClusterTargetsReady(
		current.Value,
		summary.ResultScheduling,
	); err != nil {
		return Operation{}, err
	}
	if !summary.Changed() {
		return Operation{}, fmt.Errorf(
			"extension %q configuration and scheduling are unchanged",
			name,
		)
	}
	if changes.RequireWaitable &&
		selectorOnlyPlacement(summary.ResultScheduling) {
		return Operation{}, fmt.Errorf(
			"--wait cannot track dynamic clusterSelector placement; omit --wait or replace it with --clusters",
		)
	}
	if failedHostState(current.Value.Status.State) &&
		!controllerWillRetry(
			current.Value,
			current.Value.Spec.Extension.Version,
			summary.ResultConfig,
		) {
		return Operation{}, fmt.Errorf(
			"install plan %q is %s; corrected global configuration is required before the controller can retry",
			name,
			current.Value.Status.State,
		)
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
	expectedSpec := cloneInstallPlanSpec(current.Value.Spec)
	expectedSpec.UpgradeStrategy = "Manual"
	expectedSpec.Config = summary.ResultConfig
	expectedSpec.ClusterScheduling = cloneScheduling(
		summary.ResultScheduling,
	)
	if err := ensureAcceptedPlan(name, expectedSpec, updated.Value); err != nil {
		return Operation{}, fmt.Errorf(
			"validate accepted install plan %q: %w",
			name,
			err,
		)
	}
	summary.ResultConfig = updated.Value.Spec.Config
	summary.ResultScheduling = cloneScheduling(
		updated.Value.Spec.ClusterScheduling,
	)
	return Operation{
		Kind:          OperationConfigure,
		Name:          name,
		TargetVersion: current.Value.Spec.Extension.Version,
		Baseline:      updated,
		expectation: configureWaitExpectation(
			current.Value,
			updated.Value,
			current.Value.Spec.Extension.Version,
			summary,
		),
		acceptedUID:  updated.Value.Metadata.UID,
		acceptedSpec: cloneInstallPlanSpec(updated.Value.Spec),
		hasAccepted:  true,
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
	if current.Value.Metadata.ResourceVersion == "" {
		return Operation{}, fmt.Errorf(
			"install plan %q response is missing metadata.resourceVersion",
			name,
		)
	}
	if slices.Contains(
		current.Value.Metadata.Finalizers,
		installPlanProtectionFinalizer,
	) {
		if _, err := s.exactVersion(
			ctx,
			name,
			current.Value.Spec.Extension.Version,
		); err != nil {
			return Operation{}, fmt.Errorf(
				"cannot safely uninstall extension %q because the controller requires its current ExtensionVersion: %w",
				name,
				err,
			)
		}
	}
	if err := s.client.DeleteInstallPlan(
		ctx,
		name,
		current.Value.Metadata.ResourceVersion,
	); err != nil {
		return Operation{}, fmt.Errorf("uninstall extension %q: %w", name, err)
	}
	return Operation{
		Kind:          OperationUninstall,
		Name:          name,
		TargetVersion: current.Value.Spec.Extension.Version,
		Baseline:      current,
		acceptedUID:   current.Value.Metadata.UID,
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

func cloneInstallPlanSpec(spec InstallPlanSpec) InstallPlanSpec {
	copy := spec
	copy.ClusterScheduling = cloneScheduling(spec.ClusterScheduling)
	return copy
}

func ensureAcceptedPlan(
	name string,
	expected InstallPlanSpec,
	plan InstallPlan,
) error {
	if err := ensurePlanIdentity(name, plan); err != nil {
		return err
	}
	if plan.Spec.Extension.Version != expected.Extension.Version {
		return fmt.Errorf(
			"install plan %q accepted version %q, want %q",
			name,
			plan.Spec.Extension.Version,
			expected.Extension.Version,
		)
	}
	if plan.Spec.Enabled != expected.Enabled {
		return fmt.Errorf(
			"install plan %q accepted enabled=%t, want %t",
			name,
			plan.Spec.Enabled,
			expected.Enabled,
		)
	}
	if plan.Spec.UpgradeStrategy != "Manual" {
		return fmt.Errorf(
			"install plan %q accepted upgradeStrategy %q, want %q",
			name,
			plan.Spec.UpgradeStrategy,
			"Manual",
		)
	}
	if plan.Spec.Config != expected.Config {
		return fmt.Errorf(
			"install plan %q accepted config differs from requested config",
			name,
		)
	}
	if !schedulingSemanticallyEqual(
		plan.Spec.ClusterScheduling,
		expected.ClusterScheduling,
	) {
		return fmt.Errorf(
			"install plan %q accepted cluster scheduling differs from requested scheduling",
			name,
		)
	}
	return nil
}

func schedulingSemanticallyEqual(
	left *ClusterScheduling,
	right *ClusterScheduling,
) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil &&
		rightErr == nil &&
		bytes.Equal(leftJSON, rightJSON)
}

func ensureMutable(plan InstallPlan) error {
	if plan.Metadata.DeletionTimestamp != nil {
		return fmt.Errorf("install plan %q is being deleted", plan.Metadata.Name)
	}
	switch plan.Status.State {
	case "Preparing", "Installing", "Upgrading", "Uninstalling":
		return fmt.Errorf(
			"install plan %q is currently %s",
			plan.Metadata.Name,
			plan.Status.State,
		)
	case "Uninstalled":
		return fmt.Errorf(
			"install plan %q is Uninstalled and cannot resume; run extension uninstall to remove the stale plan, then install it again",
			plan.Metadata.Name,
		)
	}
	return nil
}

func ensureClusterTargetsReady(
	plan InstallPlan,
	result *ClusterScheduling,
) error {
	for cluster := range knownPlacementClusters(result) {
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			continue
		}
		switch status.State {
		case "Uninstalling", "UninstallFailed", "Uninstalled":
			return fmt.Errorf(
				"cluster %q is %s; wait for its stale scheduling status to be removed before targeting it again",
				cluster,
				status.State,
			)
		}
	}
	return nil
}

func failedHostState(state string) bool {
	return state == "InstallFailed" || state == "UpgradeFailed"
}

func controllerWillRetry(
	plan InstallPlan,
	targetVersion string,
	resultConfig string,
) bool {
	versionChanged := plan.Status.Version != "" &&
		plan.Status.Version != targetVersion
	configHash := controllerConfigHash([]byte(resultConfig))
	configChanged := plan.Status.ConfigHash == "" ||
		plan.Status.ConfigHash != configHash
	if versionChanged || configChanged {
		return true
	}
	return false
}

func encodePlanPatch(resourceVersion string, spec map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": resourceVersion},
		"spec":     spec,
	})
}
