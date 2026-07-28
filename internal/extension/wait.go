package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

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
	Baseline                string
	Version                 string
	ConfigHash              string
	MustAdvance             bool
	requireConfigHashChange bool
}

type waitExpectation struct {
	Host            *waitTarget
	Clusters        map[string]waitTarget
	RemovedClusters map[string]struct{}
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

func versionWaitTarget(status InstallationStatus, version string) waitTarget {
	target := waitTarget{
		Baseline:    statusFingerprint(status),
		Version:     version,
		MustAdvance: true,
	}
	if successfulState(status.State) && status.Version == version {
		target.MustAdvance = false
	}
	return target
}

func changedWaitTarget(
	before InstallationStatus,
	beforeFound bool,
	after InstallationStatus,
	afterFound bool,
	version string,
	requireConfigHashChange bool,
) waitTarget {
	target := waitTarget{
		Baseline:    statusFingerprint(after),
		Version:     version,
		MustAdvance: true,
	}

	observed := afterFound &&
		(!beforeFound || statusFingerprint(before) != statusFingerprint(after))
	if requireConfigHashChange {
		observed = afterFound && after.ConfigHash != before.ConfigHash
		if !observed {
			target.ConfigHash = after.ConfigHash
			target.requireConfigHashChange = true
		}
	}
	if observed {
		target.MustAdvance = false
	}
	return target
}

func installWaitExpectation(
	baseline InstallPlan,
	version string,
	scheduling *ClusterScheduling,
) waitExpectation {
	host := versionWaitTarget(baseline.Status.InstallationStatus, version)
	expectation := waitExpectation{
		Host:     &host,
		Clusters: map[string]waitTarget{},
	}
	if scheduling == nil || scheduling.Placement == nil {
		return expectation
	}
	for _, cluster := range scheduling.Placement.Clusters {
		status := baseline.Status.ClusterSchedulingStatuses[cluster]
		expectation.Clusters[cluster] = versionWaitTarget(status, version)
	}
	return expectation
}

func upgradeWaitExpectation(
	baseline InstallPlan,
	version string,
	scheduling *ClusterScheduling,
) waitExpectation {
	expectation := installWaitExpectation(baseline, version, scheduling)
	if scheduling == nil ||
		scheduling.Placement == nil ||
		scheduling.Placement.ClusterSelector == nil {
		return expectation
	}
	for cluster, status := range baseline.Status.ClusterSchedulingStatuses {
		expectation.Clusters[cluster] = versionWaitTarget(status, version)
	}
	return expectation
}

func configureWaitExpectation(
	before InstallPlan,
	baseline InstallPlan,
	version string,
	changes PlanChanges,
	summary ChangeSummary,
) waitExpectation {
	expectation := waitExpectation{
		Clusters:        map[string]waitTarget{},
		RemovedClusters: map[string]struct{}{},
	}

	resultClusters := map[string]struct{}{}
	if result := summary.ResultScheduling; result != nil && result.Placement != nil {
		for _, cluster := range result.Placement.Clusters {
			resultClusters[cluster] = struct{}{}
		}
		if result.Placement.ClusterSelector != nil {
			for cluster := range before.Status.ClusterSchedulingStatuses {
				resultClusters[cluster] = struct{}{}
			}
			for cluster := range baseline.Status.ClusterSchedulingStatuses {
				resultClusters[cluster] = struct{}{}
			}
		}
	}

	switch changes.Scheduling.Mode {
	case Clear:
		for cluster := range before.Status.ClusterSchedulingStatuses {
			expectation.RemovedClusters[cluster] = struct{}{}
		}
	case Replace:
		for cluster := range before.Status.ClusterSchedulingStatuses {
			if _, retained := resultClusters[cluster]; !retained {
				expectation.RemovedClusters[cluster] = struct{}{}
			}
		}
	}

	targetClusters := map[string]struct{}{}
	if summary.ConfigChanged {
		host := changedWaitTarget(
			before.Status.InstallationStatus,
			true,
			baseline.Status.InstallationStatus,
			true,
			version,
			true,
		)
		expectation.Host = &host
		for cluster := range before.Status.ClusterSchedulingStatuses {
			targetClusters[cluster] = struct{}{}
		}
		for cluster := range baseline.Status.ClusterSchedulingStatuses {
			targetClusters[cluster] = struct{}{}
		}
		for cluster := range resultClusters {
			targetClusters[cluster] = struct{}{}
		}
	}

	if summary.SchedulingChanged && changes.Scheduling.Mode == Replace {
		for cluster := range resultClusters {
			targetClusters[cluster] = struct{}{}
		}
	}

	beforeOverrides := schedulingOverrides(before.Spec.ClusterScheduling)
	afterOverrides := schedulingOverrides(summary.ResultScheduling)
	for cluster := range changes.Scheduling.SetOverrides {
		if beforeOverrides[cluster] != afterOverrides[cluster] {
			targetClusters[cluster] = struct{}{}
		}
	}
	for _, cluster := range changes.Scheduling.RemoveOverrides {
		_, existedBefore := beforeOverrides[cluster]
		_, existsAfter := afterOverrides[cluster]
		if existedBefore && !existsAfter {
			targetClusters[cluster] = struct{}{}
		}
	}

	for cluster := range expectation.RemovedClusters {
		delete(targetClusters, cluster)
	}
	for _, cluster := range slices.Sorted(maps.Keys(targetClusters)) {
		beforeStatus, beforeFound := before.Status.ClusterSchedulingStatuses[cluster]
		afterStatus, afterFound := baseline.Status.ClusterSchedulingStatuses[cluster]
		expectation.Clusters[cluster] = changedWaitTarget(
			beforeStatus,
			beforeFound,
			afterStatus,
			afterFound,
			version,
			summary.ConfigChanged,
		)
	}
	return expectation
}

func schedulingOverrides(scheduling *ClusterScheduling) map[string]string {
	if scheduling == nil {
		return nil
	}
	return scheduling.Overrides
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
	if (target.requireConfigHashChange || target.ConfigHash != "") &&
		status.ConfigHash == target.ConfigHash {
		return false
	}
	return !target.MustAdvance || advanced
}

func evaluateOperation(
	operation Operation,
	plan InstallPlan,
	advanced map[string]bool,
) (bool, error) {
	if operation.Kind == OperationUninstall {
		if plan.Status.State == "UninstallFailed" {
			return false, lifecycleFailure(
				operation.Name,
				"host",
				plan.Status.InstallationStatus,
			)
		}
		for cluster, status := range plan.Status.ClusterSchedulingStatuses {
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

	if target := operation.expectation.Host; target != nil {
		status := plan.Status.InstallationStatus
		if statusFingerprint(status) != target.Baseline {
			advanced["host"] = true
		}
		if failedState(status.State) && advanced["host"] {
			return false, lifecycleFailure(operation.Name, "host", status)
		}
		if !targetSatisfied(status, *target, advanced["host"]) {
			return false, nil
		}
	}
	for cluster, target := range operation.expectation.Clusters {
		status, found := plan.Status.ClusterSchedulingStatuses[cluster]
		if !found {
			return false, nil
		}
		scope := "cluster/" + cluster
		if statusFingerprint(status) != target.Baseline {
			advanced[scope] = true
		}
		if failedState(status.State) && advanced[scope] {
			return false, lifecycleFailure(operation.Name, scope, status)
		}
		if !targetSatisfied(status, target, advanced[scope]) {
			return false, nil
		}
	}
	for cluster := range operation.expectation.RemovedClusters {
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
