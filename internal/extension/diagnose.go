package extension

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DiagnoseOptions struct {
	TargetCluster string
}

type DiagnosticStatus string

const (
	DiagnosticOK    DiagnosticStatus = "OK"
	DiagnosticInfo  DiagnosticStatus = "INFO"
	DiagnosticWarn  DiagnosticStatus = "WARN"
	DiagnosticError DiagnosticStatus = "ERROR"
)

var ErrDiagnosisFailed = errors.New("extension diagnosis found errors")

type DiagnosticCheck struct {
	Name    string
	Status  DiagnosticStatus
	Message string
}

type Diagnosis struct {
	Checks []DiagnosticCheck
}

func (d *Diagnosis) add(
	name string,
	status DiagnosticStatus,
	message string,
) {
	d.Checks = append(d.Checks, DiagnosticCheck{
		Name:    name,
		Status:  status,
		Message: message,
	})
}

func (d Diagnosis) Err() error {
	count := 0
	for _, check := range d.Checks {
		if check.Status == DiagnosticError {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d error checks", ErrDiagnosisFailed, count)
}

func (s *Service) Diagnose(
	ctx context.Context,
	name string,
	options DiagnoseOptions,
) (Diagnosis, error) {
	var diagnosis Diagnosis
	if err := validatePathName("extension", name); err != nil {
		return diagnosis, err
	}
	if options.TargetCluster != "" {
		if err := validatePathName("cluster", options.TargetCluster); err != nil {
			return diagnosis, err
		}
	}

	extension, err := s.client.GetExtension(ctx, name)
	switch {
	case err == nil:
		diagnosis.add(
			"extension",
			diagnosticState(extension.Value.Status.State),
			extensionDiagnosticMessage(extension.Value),
		)
	case apierrors.IsNotFound(err):
		diagnosis.add(
			"extension",
			DiagnosticError,
			fmt.Sprintf("extension %q was not found", name),
		)
	default:
		return diagnosis, fmt.Errorf("diagnose extension %q: %w", name, err)
	}

	planObject, err := s.client.GetInstallPlan(ctx, name)
	switch {
	case err == nil:
	case apierrors.IsNotFound(err):
		diagnosis.add(
			"install-plan",
			DiagnosticError,
			fmt.Sprintf("InstallPlan %q was not found", name),
		)
		return diagnosis, nil
	default:
		return diagnosis, fmt.Errorf(
			"diagnose extension %q InstallPlan: %w",
			name,
			err,
		)
	}
	plan := planObject.Value

	selected := plan.Status.InstallationStatus
	selectedAvailable := true
	selectedScope := "host"
	if options.TargetCluster != "" {
		selectedScope = "cluster/" + options.TargetCluster
		var found bool
		selected, found = plan.Status.ClusterSchedulingStatuses[options.TargetCluster]
		selectedAvailable = found
	}
	statusForPlan := selected
	if !selectedAvailable {
		statusForPlan = plan.Status.InstallationStatus
	}
	diagnosis.add(
		"install-plan",
		diagnosticState(statusForPlan.State),
		installPlanDiagnosticMessage(selectedScope, statusForPlan),
	)

	var selectedVersion *Object[ExtensionVersion]
	if !selectedAvailable {
		diagnosis.add(
			"version",
			DiagnosticError,
			fmt.Sprintf(
				"cannot determine installed version because %s has no status",
				selectedScope,
			),
		)
	} else {
		installedVersion := selected.Version
		if installedVersion == "" && successfulState(selected.State) {
			installedVersion = plan.Spec.Extension.Version
		}
		if installedVersion == "" {
			diagnosis.add(
				"version",
				DiagnosticError,
				"installed exact version is not reported",
			)
		} else {
			versions, listErr := s.client.ListExtensionVersions(ctx, name)
			if listErr != nil {
				return diagnosis, fmt.Errorf(
					"diagnose extension %q versions: %w",
					name,
					listErr,
				)
			}
			version, found := findVersionForDiagnosis(versions, installedVersion)
			if !found {
				diagnosis.add(
					"version",
					DiagnosticError,
					fmt.Sprintf(
						"installed ExtensionVersion %q was not found",
						installedVersion,
					),
				)
			} else {
				copy := version
				selectedVersion = &copy
				diagnosis.add(
					"version",
					DiagnosticOK,
					fmt.Sprintf(
						"exact ExtensionVersion %q is available",
						installedVersion,
					),
				)
			}
		}
	}

	if selectedVersion != nil {
		for _, dependency := range selectedVersion.Value.Spec.ExternalDependencies {
			check := s.checkDependency(ctx, dependency)
			status := dependencyDiagnosticStatus(check)
			diagnosis.add(
				"dependency/"+dependency.Name,
				status,
				dependencyDiagnosticMessage(check),
			)
		}
	}

	var job *Job
	if selectedAvailable {
		job, err = s.diagnoseWorkload(ctx, &diagnosis, selected)
		if err != nil {
			return diagnosis, fmt.Errorf(
				"diagnose extension %q executor workload: %w",
				name,
				err,
			)
		}
	}

	addClusterDiagnostics(
		&diagnosis,
		plan.Status.ClusterSchedulingStatuses,
		options.TargetCluster,
	)
	addClockDiagnostic(&diagnosis, selected, selectedAvailable, job)
	return diagnosis, nil
}

func diagnosticState(state string) DiagnosticStatus {
	switch {
	case state == "Installed" || state == "Upgraded":
		return DiagnosticOK
	case state == "" ||
		state == "Preparing" ||
		state == "Installing" ||
		state == "Upgrading":
		return DiagnosticInfo
	case strings.HasSuffix(state, "Failed"):
		return DiagnosticError
	default:
		return DiagnosticWarn
	}
}

func extensionDiagnosticMessage(extension Extension) string {
	return fmt.Sprintf(
		"state=%s installedVersion=%s recommendedVersion=%s",
		valueOrNone(extension.Status.State),
		valueOrNone(extension.Status.InstalledVersion),
		valueOrNone(extension.Status.RecommendedVersion),
	)
}

func installPlanDiagnosticMessage(
	scope string,
	status InstallationStatus,
) string {
	parts := []string{
		"scope=" + scope,
		"state=" + valueOrNone(status.State),
		"version=" + valueOrNone(status.Version),
	}
	parts = append(parts, conditionMessages(status.Conditions)...)
	return strings.Join(parts, "; ")
}

func conditionMessages(conditions []Condition) []string {
	messages := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		if condition.Message == "" {
			continue
		}
		label := condition.Type
		if condition.Reason != "" {
			label += "/" + condition.Reason
		}
		messages = append(messages, label+": "+condition.Message)
	}
	return messages
}

func findVersionForDiagnosis(
	versions List[ExtensionVersion],
	exact string,
) (Object[ExtensionVersion], bool) {
	for _, version := range versions.Items {
		if version.Value.Spec.Version == exact {
			return version, true
		}
	}
	return Object[ExtensionVersion]{}, false
}

func dependencyDiagnosticStatus(check DependencyCheck) DiagnosticStatus {
	if check.Code == DependencySatisfied {
		return DiagnosticOK
	}
	if !check.Dependency.Required &&
		check.Code == DependencyUnsupportedType {
		return DiagnosticInfo
	}
	if check.Dependency.Required {
		return DiagnosticError
	}
	return DiagnosticWarn
}

func dependencyDiagnosticMessage(check DependencyCheck) string {
	requirement := "optional"
	if check.Dependency.Required {
		requirement = "required"
	}
	return fmt.Sprintf(
		"%s dependency type=%s constraint=%s result=%s state=%s version=%s",
		requirement,
		valueOrDefault(check.Dependency.Type, "extension"),
		valueOrNone(check.Dependency.Version),
		check.Code,
		valueOrNone(check.ObservedState),
		valueOrNone(check.ObservedVersion),
	)
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (s *Service) diagnoseWorkload(
	ctx context.Context,
	diagnosis *Diagnosis,
	status InstallationStatus,
) (*Job, error) {
	if status.TargetNamespace == "" {
		diagnosis.add(
			"namespace",
			missingWorkloadIdentitySeverity(status.State),
			"target namespace is not reported",
		)
		if status.JobName == "" {
			diagnosis.add(
				"job",
				missingWorkloadIdentitySeverity(status.State),
				"executor Job name is not reported",
			)
		} else {
			diagnosis.add(
				"job",
				DiagnosticError,
				fmt.Sprintf(
					"cannot inspect Job %q without a target namespace",
					status.JobName,
				),
			)
		}
		return nil, nil
	}

	_, err := s.client.GetNamespace(ctx, status.TargetNamespace)
	switch {
	case err == nil:
		diagnosis.add(
			"namespace",
			DiagnosticOK,
			fmt.Sprintf("namespace %q exists", status.TargetNamespace),
		)
	case apierrors.IsNotFound(err):
		diagnosis.add(
			"namespace",
			DiagnosticError,
			fmt.Sprintf("namespace %q was not found", status.TargetNamespace),
		)
		if status.JobName != "" {
			diagnosis.add(
				"job",
				DiagnosticError,
				fmt.Sprintf(
					"cannot inspect Job %q because namespace %q was not found",
					status.JobName,
					status.TargetNamespace,
				),
			)
		}
		return nil, nil
	default:
		return nil, err
	}

	if status.JobName == "" {
		diagnosis.add(
			"job",
			missingWorkloadIdentitySeverity(status.State),
			"executor Job name is not reported",
		)
		return nil, nil
	}

	jobValue, err := s.client.GetJob(
		ctx,
		status.TargetNamespace,
		status.JobName,
	)
	switch {
	case err == nil:
	case apierrors.IsNotFound(err):
		severity := DiagnosticError
		message := fmt.Sprintf(
			"Job %q was not found",
			status.TargetNamespace+"/"+status.JobName,
		)
		if successfulState(status.State) {
			severity = DiagnosticWarn
			message += "; it may have been removed by its TTL after success"
		}
		diagnosis.add("job", severity, message)
		return nil, nil
	default:
		return nil, err
	}

	jobStatus, message := jobDiagnostic(
		jobValue,
		status.TargetNamespace,
		status.JobName,
	)
	diagnosis.add("job", jobStatus, message)

	pods, err := s.client.ListPodsForJob(
		ctx,
		status.TargetNamespace,
		status.JobName,
	)
	if err != nil {
		return &jobValue, err
	}
	sort.SliceStable(pods.Items, func(i, j int) bool {
		return pods.Items[i].Name < pods.Items[j].Name
	})
	for _, pod := range pods.Items {
		severity, podMessage := podDiagnostic(
			pod,
			status.TargetNamespace,
			status.JobName,
		)
		diagnosis.add("pod/"+pod.Name, severity, podMessage)
	}
	return &jobValue, nil
}

func missingWorkloadIdentitySeverity(state string) DiagnosticStatus {
	switch {
	case failedState(state):
		return DiagnosticError
	case successfulState(state):
		return DiagnosticWarn
	default:
		return DiagnosticInfo
	}
}

func jobDiagnostic(
	job Job,
	namespace string,
	name string,
) (DiagnosticStatus, string) {
	base := fmt.Sprintf(
		"Job %q active=%d succeeded=%d failed=%d",
		namespace+"/"+name,
		job.Status.Active,
		job.Status.Succeeded,
		job.Status.Failed,
	)
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed &&
			condition.Status == corev1.ConditionTrue {
			detail := condition.Reason
			if condition.Message != "" {
				if detail != "" {
					detail += ": "
				}
				detail += condition.Message
			}
			if detail != "" {
				base += "; " + detail
			}
			return DiagnosticError, base + "; " +
				kubectlLogsSuggestion(namespace, name)
		}
	}
	if job.Status.Failed != 0 {
		return DiagnosticError, base + "; " +
			kubectlLogsSuggestion(namespace, name)
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete &&
			condition.Status == corev1.ConditionTrue {
			return DiagnosticOK, base + "; complete"
		}
	}
	if job.Status.Active != 0 {
		return DiagnosticInfo, base + "; active"
	}
	return DiagnosticInfo, base + "; waiting"
}

func podDiagnostic(
	pod corev1.Pod,
	namespace string,
	jobName string,
) (DiagnosticStatus, string) {
	terminations, failedTermination := podTerminations(pod)
	parts := []string{"phase=" + string(pod.Status.Phase)}
	if len(terminations) != 0 {
		parts = append(parts, strings.Join(terminations, ", "))
	}

	severity := DiagnosticWarn
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		severity = DiagnosticOK
	case corev1.PodPending, corev1.PodRunning:
		severity = DiagnosticInfo
	case corev1.PodFailed:
		severity = DiagnosticError
	}
	if failedTermination {
		severity = DiagnosticError
	}
	if severity == DiagnosticError {
		parts = append(parts, kubectlLogsSuggestion(namespace, jobName))
	}
	return severity, strings.Join(parts, "; ")
}

func podTerminations(pod corev1.Pod) ([]string, bool) {
	type namedTermination struct {
		name        string
		termination *corev1.ContainerStateTerminated
	}
	terminations := make([]namedTermination, 0)
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Terminated != nil {
			terminations = append(terminations, namedTermination{
				name:        "init/" + status.Name,
				termination: status.State.Terminated,
			})
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			terminations = append(terminations, namedTermination{
				name:        status.Name,
				termination: status.State.Terminated,
			})
		}
	}
	slices.SortFunc(terminations, func(left, right namedTermination) int {
		return strings.Compare(left.name, right.name)
	})

	result := make([]string, 0, len(terminations))
	failed := false
	for _, item := range terminations {
		reason := valueOrDefault(item.termination.Reason, "Terminated")
		result = append(result, fmt.Sprintf(
			"%s=%s(exit=%d)",
			item.name,
			reason,
			item.termination.ExitCode,
		))
		if item.termination.ExitCode != 0 {
			failed = true
		}
	}
	return result, failed
}

func kubectlLogsSuggestion(namespace, jobName string) string {
	return fmt.Sprintf(
		"inspect with: kubectl -n %s logs job/%s",
		namespace,
		jobName,
	)
}

func addClusterDiagnostics(
	diagnosis *Diagnosis,
	statuses map[string]InstallationStatus,
	requested string,
) {
	names := make(map[string]struct{}, len(statuses)+1)
	for name := range statuses {
		names[name] = struct{}{}
	}
	if requested != "" {
		names[requested] = struct{}{}
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		status, found := statuses[name]
		if !found {
			diagnosis.add(
				"cluster/"+name,
				DiagnosticError,
				fmt.Sprintf("cluster status %q was not found", name),
			)
			continue
		}
		diagnosis.add(
			"cluster/"+name,
			diagnosticState(status.State),
			fmt.Sprintf(
				"state=%s version=%s namespace=%s job=%s",
				valueOrNone(status.State),
				valueOrNone(status.Version),
				valueOrNone(status.TargetNamespace),
				valueOrNone(status.JobName),
			),
		)
	}
}

func addClockDiagnostic(
	diagnosis *Diagnosis,
	status InstallationStatus,
	statusAvailable bool,
	job *Job,
) {
	if !statusAvailable {
		diagnosis.add(
			"clock",
			DiagnosticInfo,
			"no selected status is available for timestamp comparison",
		)
		return
	}
	completed := jobCompletionTime(job)
	if completed == nil {
		diagnosis.add(
			"clock",
			DiagnosticInfo,
			"no completed executor Job timestamp is available for comparison",
		)
		return
	}
	if status.State == "Installing" || status.State == "Upgrading" {
		if possibleClockSkew(status, job) {
			diagnosis.add(
				"clock",
				DiagnosticWarn,
				"executor Job completion predates the latest plan transition; possible clock skew",
			)
			return
		}
		diagnosis.add(
			"clock",
			DiagnosticWarn,
			"executor Job is complete while the plan is still transitioning; possible controller lag",
		)
		return
	}
	diagnosis.add(
		"clock",
		DiagnosticOK,
		"executor Job and plan timestamps show no transition inconsistency",
	)
}

func possibleClockSkew(status InstallationStatus, job *Job) bool {
	if status.State != "Installing" && status.State != "Upgrading" {
		return false
	}
	completed := jobCompletionTime(job)
	if completed == nil {
		return false
	}
	transition := latestStateTransition(status.StateHistory)
	return transition != nil && completed.Before(transition)
}

func jobCompletionTime(job *Job) *metav1.Time {
	if job == nil {
		return nil
	}
	if job.Status.CompletionTime != nil {
		return job.Status.CompletionTime
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete &&
			condition.Status == corev1.ConditionTrue &&
			!condition.LastTransitionTime.IsZero() {
			copy := condition.LastTransitionTime
			return &copy
		}
	}
	return nil
}

func latestStateTransition(history []InstallPlanState) *metav1.Time {
	var latest *metav1.Time
	for _, state := range history {
		if state.LastTransitionTime.IsZero() {
			continue
		}
		if latest == nil || latest.Before(&state.LastTransitionTime) {
			copy := state.LastTransitionTime
			latest = &copy
		}
	}
	return latest
}
