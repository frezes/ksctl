package extension

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	yaml3 "gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type PollOptions struct {
	Timeout time.Duration
	OnState func(StateEvent) error
}

type StateEvent struct {
	State           string
	TargetNamespace string
	JobName         string
}

type WaitResult struct {
	Plan    *Object[InstallPlan]
	Deleted bool
}

type LifecycleFailureError struct {
	Name            string
	Scope           string
	State           string
	TargetNamespace string
	JobName         string
	Conditions      []Condition
}

func (e *LifecycleFailureError) Error() string {
	location := ""
	if e.TargetNamespace != "" {
		location += " namespace=" + e.TargetNamespace
	}
	if e.JobName != "" {
		location += " job=" + e.JobName
	}

	messages := make([]string, 0, len(e.Conditions))
	for _, condition := range e.Conditions {
		if condition.Message == "" {
			continue
		}
		label := condition.Type
		if condition.Reason != "" {
			label += "/" + condition.Reason
		}
		messages = append(messages, label+": "+condition.Message)
	}
	detail := ""
	if len(messages) != 0 {
		detail = ": " + strings.Join(messages, "; ")
	}
	return fmt.Sprintf(
		"extension %q %s reached %s%s%s",
		e.Name,
		e.Scope,
		e.State,
		location,
		detail,
	)
}

type waitTarget struct {
	Baseline    string
	Version     string
	ConfigHash  string
	MustAdvance bool
}

type removedWaitTarget struct {
	Baseline string
}

type waitExpectation struct {
	Host            *waitTarget
	Clusters        map[string]waitTarget
	RemovedClusters map[string]removedWaitTarget
	SelectorOnly    bool
}

func statusFingerprint(status InstallationStatus) string {
	raw, err := json.Marshal(status)
	if err != nil {
		panic(fmt.Sprintf("marshal installation status: %v", err))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func successfulState(state string) bool {
	return state == "Installed" || state == "Upgraded"
}

func failedState(state string) bool {
	return strings.HasSuffix(state, "Failed")
}

func lifecycleFailure(name, scope string, status InstallationStatus) error {
	return &LifecycleFailureError{
		Name:            name,
		Scope:           scope,
		State:           status.State,
		TargetNamespace: status.TargetNamespace,
		JobName:         status.JobName,
		Conditions:      slices.Clone(status.Conditions),
	}
}

func baselineWaitTarget(
	status InstallationStatus,
	found bool,
	version string,
	configHash string,
) waitTarget {
	target := waitTarget{
		Baseline:    statusFingerprint(status),
		Version:     version,
		ConfigHash:  configHash,
		MustAdvance: true,
	}
	if found && targetStatusMatches(status, target) {
		target.MustAdvance = false
	}
	return target
}

func installWaitExpectation(
	baseline InstallPlan,
	version string,
) waitExpectation {
	host := baselineWaitTarget(
		baseline.Status.InstallationStatus,
		true,
		version,
		controllerConfigHash([]byte(baseline.Spec.Config)),
	)
	expectation := waitExpectation{
		Host:     &host,
		Clusters: map[string]waitTarget{},
	}
	scheduling := baseline.Spec.ClusterScheduling
	if scheduling == nil || scheduling.Placement == nil {
		return expectation
	}
	for _, cluster := range scheduling.Placement.Clusters {
		status, found := baseline.Status.ClusterSchedulingStatuses[cluster]
		expectation.Clusters[cluster] = baselineWaitTarget(
			status,
			found,
			version,
			controllerConfigHash(effectiveClusterConfig(
				baseline.Spec.Config,
				scheduling,
				cluster,
			)),
		)
	}
	return expectation
}

func upgradeWaitExpectation(
	before InstallPlan,
	baseline InstallPlan,
	version string,
	summary ChangeSummary,
) waitExpectation {
	host := baselineWaitTarget(
		baseline.Status.InstallationStatus,
		true,
		version,
		controllerConfigHash([]byte(summary.ResultConfig)),
	)
	expectation := waitExpectation{
		Host:         &host,
		Clusters:     map[string]waitTarget{},
		SelectorOnly: selectorOnlyPlacement(summary.ResultScheduling),
		RemovedClusters: removedWaitTargets(
			before,
			baseline,
			knownPlacementClusters(summary.ResultScheduling),
			hasDeterministicPlacement(summary.ResultScheduling),
		),
	}
	resultClusters := knownPlacementClusters(summary.ResultScheduling)
	for _, cluster := range slices.Sorted(maps.Keys(resultClusters)) {
		status, found := baseline.Status.ClusterSchedulingStatuses[cluster]
		expectation.Clusters[cluster] = baselineWaitTarget(
			status,
			found,
			version,
			controllerConfigHash(effectiveClusterConfig(
				summary.ResultConfig,
				summary.ResultScheduling,
				cluster,
			)),
		)
	}
	return expectation
}

func configureWaitExpectation(
	before InstallPlan,
	baseline InstallPlan,
	version string,
	summary ChangeSummary,
) waitExpectation {
	resultClusters := knownPlacementClusters(summary.ResultScheduling)
	expectation := waitExpectation{
		Clusters:     map[string]waitTarget{},
		SelectorOnly: selectorOnlyPlacement(summary.ResultScheduling),
		RemovedClusters: removedWaitTargets(
			before,
			baseline,
			resultClusters,
			hasDeterministicPlacement(summary.ResultScheduling),
		),
	}

	beforeHostConfig := []byte(before.Spec.Config)
	resultHostConfig := []byte(summary.ResultConfig)
	if !bytes.Equal(beforeHostConfig, resultHostConfig) {
		host := baselineWaitTarget(
			baseline.Status.InstallationStatus,
			true,
			version,
			controllerConfigHash(resultHostConfig),
		)
		expectation.Host = &host
	}

	beforeClusters := knownPlacementClusters(before.Spec.ClusterScheduling)
	for _, cluster := range slices.Sorted(maps.Keys(resultClusters)) {
		beforeConfig := effectiveClusterConfig(
			before.Spec.Config,
			before.Spec.ClusterScheduling,
			cluster,
		)
		resultConfig := effectiveClusterConfig(
			summary.ResultConfig,
			summary.ResultScheduling,
			cluster,
		)
		_, retained := beforeClusters[cluster]
		if retained && bytes.Equal(beforeConfig, resultConfig) {
			continue
		}
		status, found := baseline.Status.ClusterSchedulingStatuses[cluster]
		expectation.Clusters[cluster] = baselineWaitTarget(
			status,
			found,
			version,
			controllerConfigHash(resultConfig),
		)
	}
	return expectation
}

func knownPlacementClusters(
	scheduling *ClusterScheduling,
) map[string]struct{} {
	clusters := map[string]struct{}{}
	if scheduling == nil || scheduling.Placement == nil {
		return clusters
	}
	for _, cluster := range scheduling.Placement.Clusters {
		clusters[cluster] = struct{}{}
	}
	return clusters
}

func hasDeterministicPlacement(scheduling *ClusterScheduling) bool {
	return scheduling != nil &&
		scheduling.Placement != nil &&
		!selectorOnlyPlacement(scheduling)
}

func removedWaitTargets(
	before InstallPlan,
	baseline InstallPlan,
	resultClusters map[string]struct{},
	cleanupExpected bool,
) map[string]removedWaitTarget {
	result := map[string]removedWaitTarget{}
	if !cleanupExpected {
		return result
	}
	candidates := map[string]struct{}{}
	for cluster := range before.Status.ClusterSchedulingStatuses {
		candidates[cluster] = struct{}{}
	}
	for cluster := range baseline.Status.ClusterSchedulingStatuses {
		candidates[cluster] = struct{}{}
	}
	for _, cluster := range slices.Sorted(maps.Keys(candidates)) {
		if _, retained := resultClusters[cluster]; retained {
			continue
		}
		status, found := baseline.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			continue
		}
		result[cluster] = removedWaitTarget{
			Baseline: statusFingerprint(status),
		}
	}
	return result
}

func effectiveClusterConfig(
	config string,
	scheduling *ClusterScheduling,
	cluster string,
) []byte {
	if scheduling == nil {
		return []byte(config)
	}
	override, found := scheduling.Overrides[cluster]
	if !found {
		return []byte(config)
	}
	return mergeControllerConfig(config, override)
}

func mergeControllerConfig(config, override string) []byte {
	config = strings.TrimSpace(config)
	override = strings.TrimSpace(override)
	switch {
	case config == "" && override == "":
		return []byte("")
	case override == "":
		return []byte(config)
	case config == "":
		return []byte(override)
	}

	base := map[string]any{}
	_ = yaml3.Unmarshal([]byte(config), &base)
	overlay := map[string]any{}
	_ = yaml3.Unmarshal([]byte(override), &overlay)
	merged, _ := yaml3.Marshal(mergeConfigValues(base, overlay))
	return merged
}

func mergeConfigValues(destination, source map[string]any) map[string]any {
	for key, value := range source {
		current, found := destination[key]
		if !found {
			destination[key] = value
			continue
		}
		sourceMap, sourceIsMap := value.(map[string]any)
		destinationMap, destinationIsMap := current.(map[string]any)
		if !sourceIsMap || !destinationIsMap {
			destination[key] = value
			continue
		}
		destination[key] = mergeConfigValues(destinationMap, sourceMap)
	}
	return destination
}

func controllerConfigHash(config []byte) string {
	hash := fnv.New64a()
	_, _ = hash.Write(config)
	return hex.EncodeToString(hash.Sum(nil))
}

func targetSatisfied(
	status InstallationStatus,
	target waitTarget,
	advanced bool,
) bool {
	if !successfulState(status.State) {
		return false
	}
	if target.Version != "" && status.Version != target.Version {
		return false
	}
	if target.ConfigHash != "" && status.ConfigHash != target.ConfigHash {
		return false
	}
	return !target.MustAdvance || advanced
}

func targetStatusMatches(status InstallationStatus, target waitTarget) bool {
	return targetSatisfied(status, target, true)
}

func targetFailureIsTerminal(
	status InstallationStatus,
	target waitTarget,
	advanced bool,
) bool {
	if !failedState(status.State) || !advanced {
		return false
	}
	if status.State != "InstallFailed" &&
		status.State != "UpgradeFailed" {
		return true
	}
	if target.ConfigHash == "" {
		return true
	}
	versionChanged := status.Version != "" &&
		status.Version != target.Version
	configChanged := status.ConfigHash == "" ||
		status.ConfigHash != target.ConfigHash
	return !versionChanged && !configChanged
}

func evaluateOperation(
	operation Operation,
	plan InstallPlan,
	advanced map[string]bool,
) (bool, error) {
	if operation.Kind == OperationUninstall {
		if operation.acceptedUID != "" &&
			plan.Metadata.UID != operation.acceptedUID {
			return false, fmt.Errorf(
				"extension %q uninstall was superseded: install plan identity changed",
				operation.Name,
			)
		}
		if plan.Status.State == "UninstallFailed" {
			return false, lifecycleFailure(
				operation.Name,
				"host",
				plan.Status.InstallationStatus,
			)
		}
		for _, cluster := range slices.Sorted(
			maps.Keys(plan.Status.ClusterSchedulingStatuses),
		) {
			status := plan.Status.ClusterSchedulingStatuses[cluster]
			if status.State == "UninstallFailed" {
				return false, lifecycleFailure(
					operation.Name,
					"cluster/"+cluster,
					status,
				)
			}
		}
		return false, nil
	}
	if operation.hasAccepted {
		if plan.Metadata.DeletionTimestamp != nil {
			return false, fmt.Errorf(
				"extension %q operation was superseded: accepted install plan is being deleted",
				operation.Name,
			)
		}
		if operation.acceptedUID != "" &&
			plan.Metadata.UID != operation.acceptedUID {
			return false, fmt.Errorf(
				"extension %q operation was superseded: accepted install plan identity changed",
				operation.Name,
			)
		}
		if !reflect.DeepEqual(plan.Spec, operation.acceptedSpec) {
			return false, fmt.Errorf(
				"extension %q operation was superseded: accepted install plan spec changed",
				operation.Name,
			)
		}
	}

	clusterNames := slices.Sorted(maps.Keys(operation.expectation.Clusters))
	removedNames := slices.Sorted(maps.Keys(operation.expectation.RemovedClusters))

	// First record advancement for every target. Failure checks happen only
	// after this pass so an incomplete target cannot mask a later failure.
	if target := operation.expectation.Host; target != nil {
		status := plan.Status.InstallationStatus
		if statusFingerprint(status) != target.Baseline {
			advanced["host"] = true
		}
	}
	for _, cluster := range clusterNames {
		target := operation.expectation.Clusters[cluster]
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			continue
		}
		scope := "cluster/" + cluster
		if statusFingerprint(status) != target.Baseline {
			advanced[scope] = true
		}
	}
	for _, cluster := range removedNames {
		target := operation.expectation.RemovedClusters[cluster]
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			continue
		}
		scope := "cluster/" + cluster
		if statusFingerprint(status) != target.Baseline {
			advanced[scope] = true
		}
	}

	if target := operation.expectation.Host; target != nil {
		status := plan.Status.InstallationStatus
		if targetFailureIsTerminal(status, *target, advanced["host"]) {
			return false, lifecycleFailure(operation.Name, "host", status)
		}
	}
	for _, cluster := range clusterNames {
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			continue
		}
		scope := "cluster/" + cluster
		if status.State == "UninstallFailed" ||
			targetFailureIsTerminal(
				status,
				operation.expectation.Clusters[cluster],
				advanced[scope],
			) {
			return false, lifecycleFailure(operation.Name, scope, status)
		}
	}
	for _, cluster := range removedNames {
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			continue
		}
		scope := "cluster/" + cluster
		if status.State == "UninstallFailed" {
			return false, lifecycleFailure(operation.Name, scope, status)
		}
	}

	if target := operation.expectation.Host; target != nil {
		status := plan.Status.InstallationStatus
		if !targetSatisfied(status, *target, advanced["host"]) {
			return false, nil
		}
	}
	for _, cluster := range clusterNames {
		target := operation.expectation.Clusters[cluster]
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			return false, nil
		}
		scope := "cluster/" + cluster
		if !targetSatisfied(status, target, advanced[scope]) {
			return false, nil
		}
	}
	for _, cluster := range removedNames {
		if _, found := plan.Status.ClusterSchedulingStatuses[cluster]; found {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) Wait(
	parent context.Context,
	operation Operation,
	options PollOptions,
) (WaitResult, error) {
	if options.Timeout <= 0 {
		return WaitResult{}, fmt.Errorf("wait timeout must be positive")
	}
	if operation.expectation.SelectorOnly {
		return WaitResult{}, fmt.Errorf(
			"extension %q request was accepted, but dynamic clusterSelector placement cannot be tracked safely; omit --wait or replace it with --clusters",
			operation.Name,
		)
	}
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()

	advanced := map[string]bool{}
	lastHostState := operation.Baseline.Value.Status.State
	for {
		plan, err := s.client.GetInstallPlan(ctx, operation.Name)
		if err != nil {
			if apierrors.IsNotFound(err) && operation.Kind == OperationUninstall {
				return WaitResult{Deleted: true}, nil
			}
			return WaitResult{}, fmt.Errorf(
				"wait for extension %q: %w",
				operation.Name,
				err,
			)
		}
		if plan.Value.Status.State != lastHostState {
			lastHostState = plan.Value.Status.State
			if options.OnState != nil {
				status := plan.Value.Status.InstallationStatus
				if err := options.OnState(StateEvent{
					State:           status.State,
					TargetNamespace: status.TargetNamespace,
					JobName:         status.JobName,
				}); err != nil {
					return WaitResult{}, fmt.Errorf(
						"report extension %q state: %w",
						operation.Name,
						err,
					)
				}
			}
		}
		done, err := evaluateOperation(operation, plan.Value, advanced)
		if err != nil {
			return WaitResult{}, err
		}
		if done {
			copy := plan
			return WaitResult{Plan: &copy}, nil
		}
		select {
		case <-ctx.Done():
			return WaitResult{}, fmt.Errorf(
				"wait for extension %q: %w",
				operation.Name,
				ctx.Err(),
			)
		case <-s.after(s.pollInterval):
		}
	}
}

func (s *Service) Watch(
	parent context.Context,
	name string,
	options PollOptions,
) (Object[InstallPlan], error) {
	if err := validatePathName("extension", name); err != nil {
		return Object[InstallPlan]{}, err
	}
	if options.Timeout <= 0 {
		return Object[InstallPlan]{}, fmt.Errorf("watch timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()

	lastState := "\x00"
	for {
		plan, err := s.client.GetInstallPlan(ctx, name)
		if err != nil {
			return Object[InstallPlan]{}, fmt.Errorf(
				"watch install plan %q: %w",
				name,
				err,
			)
		}
		if err := ensurePlanIdentity(name, plan.Value); err != nil {
			return Object[InstallPlan]{}, err
		}
		status := plan.Value.Status.InstallationStatus
		if status.State != lastState {
			lastState = status.State
			if options.OnState != nil {
				if err := options.OnState(StateEvent{
					State:           status.State,
					TargetNamespace: status.TargetNamespace,
					JobName:         status.JobName,
				}); err != nil {
					return Object[InstallPlan]{}, fmt.Errorf(
						"report install plan %q state: %w",
						name,
						err,
					)
				}
			}
		}
		if failedState(status.State) {
			return Object[InstallPlan]{}, lifecycleFailure(name, "host", status)
		}
		if successfulState(status.State) {
			return plan, nil
		}
		select {
		case <-ctx.Done():
			return Object[InstallPlan]{}, fmt.Errorf(
				"watch install plan %q: %w",
				name,
				ctx.Err(),
			)
		case <-s.after(s.pollInterval):
		}
	}
}
