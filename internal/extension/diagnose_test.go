package extension

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func diagnosticExtension(state string) Extension {
	extension := extensionForTest("demo")
	extension.Status.State = state
	return extension
}

func diagnosticPlan(state string) InstallPlan {
	plan := planForTest("demo", "1.2.1", state)
	plan.Status.TargetNamespace = "demo-system"
	plan.Status.JobName = "install-demo"
	return plan
}

func diagnosticVersion(dependencies ...ExternalDependency) ExtensionVersion {
	version := lifecycleVersion("demo", "1.2.1", "Multicluster", dependencies...)
	return version
}

func prepareDiagnosis(
	t *testing.T,
	client *fakeAPIClient,
	plan InstallPlan,
	version ExtensionVersion,
) {
	t.Helper()
	client.extensionObjects["demo"] = objectForTest(
		t,
		diagnosticExtension("Installed"),
	)
	client.planObjects["demo"] = objectForTest(t, plan)
	client.versions["demo"] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		version,
	)
	client.versionObjects[version.Metadata.Name] = objectForTest(t, version)
}

func completeJob(namespace, name string, completion time.Time) Job {
	return Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{{
				Type:   batchv1.JobComplete,
				Status: corev1.ConditionTrue,
			}},
			CompletionTime: &metav1.Time{Time: completion},
			Succeeded:      1,
		},
	}
}

func successfulPod(namespace, name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
}

func findDiagnosticCheck(
	t testing.TB,
	diagnosis Diagnosis,
	name string,
) DiagnosticCheck {
	t.Helper()
	for _, check := range diagnosis.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing diagnostic check %q in %#v", name, diagnosis.Checks)
	return DiagnosticCheck{}
}

func TestServiceDiagnoseHealthyResourcesInStableOrder(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	plan.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
		"member-b": {State: "Installed", Version: "1.2.1"},
		"member-a": {State: "Installed", Version: "1.2.1"},
	}
	version := diagnosticVersion(ExternalDependency{
		Name:     "logging",
		Version:  "1.x",
		Required: true,
	})
	prepareDiagnosis(t, client, plan, version)
	dependency := planForTest("logging", "1.4.0", "Installed")
	client.planObjects["logging"] = objectForTest(t, dependency)
	client.namespaces["demo-system"] = Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
	}
	client.jobs["demo-system/install-demo"] = completeJob(
		"demo-system",
		"install-demo",
		time.Now(),
	)
	client.pods["demo-system/install-demo"] = PodList{Items: []corev1.Pod{
		successfulPod("demo-system", "pod-b"),
		successfulPod("demo-system", "pod-a"),
	}}

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if err := diagnosis.Err(); err != nil {
		t.Fatalf("Diagnosis.Err() = %v", err)
	}
	var names []string
	for _, check := range diagnosis.Checks {
		names = append(names, check.Name)
	}
	wantNames := []string{
		"extension",
		"install-plan",
		"version",
		"dependency/logging",
		"namespace",
		"job",
		"pod/pod-a",
		"pod/pod-b",
		"cluster/member-a",
		"cluster/member-b",
		"clock",
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("check names = %v, want %v", names, wantNames)
	}
	for _, check := range diagnosis.Checks {
		if check.Status == DiagnosticError {
			t.Fatalf("unexpected error check = %#v", check)
		}
	}
	wantCalls := []string{
		"get extension demo",
		"get install plan demo",
		"get extension version demo-1.2.1",
		"get install plan logging",
		"get namespace demo-system",
		"get job demo-system/install-demo",
		"list pods demo-system/install-demo",
	}
	if !reflect.DeepEqual(client.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", client.calls, wantCalls)
	}
}

func TestServiceDiagnoseConvertsMissingPrimaryResourcesToChecks(t *testing.T) {
	client := newFakeAPIClient(t)
	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if got := []DiagnosticCheck{
		findDiagnosticCheck(t, diagnosis, "extension"),
		findDiagnosticCheck(t, diagnosis, "install-plan"),
	}; got[0].Status != DiagnosticError || got[1].Status != DiagnosticError {
		t.Fatalf("checks = %#v", diagnosis.Checks)
	}
	if !errors.Is(diagnosis.Err(), ErrDiagnosisFailed) {
		t.Fatalf("Diagnosis.Err() = %v, want ErrDiagnosisFailed", diagnosis.Err())
	}
}

func TestServiceDiagnoseSelectsTargetClusterButUsesHostWorkloads(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	plan.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
		"member-a": {
			State:           "Installed",
			Version:         "1.2.1",
			TargetNamespace: "member-executor",
			JobName:         "member-install",
		},
	}
	prepareDiagnosis(t, client, plan, diagnosticVersion())
	client.namespaces["member-executor"] = Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "member-executor"},
	}
	client.jobs["member-executor/member-install"] = completeJob(
		"member-executor",
		"member-install",
		time.Now(),
	)
	client.pods["member-executor/member-install"] = PodList{}

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{TargetCluster: "member-a"},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if err := diagnosis.Err(); err != nil {
		t.Fatalf("Diagnosis.Err() = %v", err)
	}
	wantWorkloadCalls := []string{
		"get namespace member-executor",
		"get job member-executor/member-install",
		"list pods member-executor/member-install",
	}
	if got := client.calls[len(client.calls)-3:]; !reflect.DeepEqual(got, wantWorkloadCalls) {
		t.Fatalf("workload calls = %v, want %v", got, wantWorkloadCalls)
	}
	for _, call := range client.calls {
		if strings.Contains(call, "clusters/") {
			t.Fatalf("member-routed call = %q", call)
		}
	}
}

func TestServiceDiagnoseReportsUnknownTargetCluster(t *testing.T) {
	client := newFakeAPIClient(t)
	prepareDiagnosis(t, client, diagnosticPlan("Installed"), diagnosticVersion())

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{TargetCluster: "missing"},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "cluster/missing")
	if check.Status != DiagnosticError || !strings.Contains(check.Message, "not found") {
		t.Fatalf("cluster check = %#v", check)
	}
	planCheck := findDiagnosticCheck(t, diagnosis, "install-plan")
	if planCheck.Status != DiagnosticError ||
		!strings.Contains(planCheck.Message, "scope=cluster/missing") ||
		!strings.Contains(planCheck.Message, "no status") {
		t.Fatalf("install-plan check = %#v", planCheck)
	}
	for _, call := range client.calls {
		if strings.HasPrefix(call, "get namespace ") ||
			strings.HasPrefix(call, "get job ") ||
			strings.HasPrefix(call, "list pods ") {
			t.Fatalf("unexpected workload call = %q", call)
		}
	}
}

func TestServiceDiagnoseReportsMissingExactVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	prepareDiagnosis(t, client, plan, diagnosticVersion())
	client.versions["demo"] = listForTest[ExtensionVersion](
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
	)
	delete(client.versionObjects, "demo-1.2.1")
	client.namespaces["demo-system"] = Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
	}
	client.jobErrs["demo-system/install-demo"] = notFound("jobs", "install-demo")

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "version")
	if check.Status != DiagnosticError || !strings.Contains(check.Message, "1.2.1") {
		t.Fatalf("version check = %#v", check)
	}
}

func TestServiceDiagnoseChecksControllerTargetVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	plan.Spec.Extension.Version = "2.0.0"
	plan.Status.Version = "1.2.1"
	prepareDiagnosis(t, client, plan, diagnosticVersion())

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "version")
	if check.Status != DiagnosticError ||
		!strings.Contains(check.Message, "demo-2.0.0") {
		t.Fatalf("version check = %#v", check)
	}
	if !slices.Contains(
		client.calls,
		"get extension version demo-2.0.0",
	) {
		t.Fatalf("calls = %v, want target version lookup", client.calls)
	}
}

func TestServiceDiagnoseRejectsMismatchedInstallPlanIdentity(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	plan.Spec.Extension.Name = "other"
	prepareDiagnosis(t, client, plan, diagnosticVersion(
		ExternalDependency{Name: "logging", Version: "1.x", Required: true},
	))

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "install-plan")
	if check.Status != DiagnosticError ||
		!strings.Contains(check.Message, `references extension "other"`) {
		t.Fatalf("install-plan check = %#v", check)
	}
	for _, call := range client.calls {
		if strings.HasPrefix(call, "get extension version ") ||
			strings.HasPrefix(call, "get install plan logging") {
			t.Fatalf("trusted mismatched plan: calls = %v", client.calls)
		}
	}
}

func TestServiceDiagnoseReportsDeletingInstallPlan(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	now := metav1.Now()
	plan.Metadata.DeletionTimestamp = &now
	prepareDiagnosis(t, client, plan, diagnosticVersion())

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "install-plan")
	if check.Status != DiagnosticError ||
		!strings.Contains(check.Message, "deleting") {
		t.Fatalf("install-plan check = %#v", check)
	}
	if !errors.Is(diagnosis.Err(), ErrDiagnosisFailed) {
		t.Fatalf("Diagnosis.Err() = %v, want deleting failure", diagnosis.Err())
	}
}

func TestServiceDiagnoseRequiresExactVersionResourceIdentity(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	prepareDiagnosis(t, client, plan, diagnosticVersion())
	wrong := diagnosticVersion()
	wrong.Metadata.Name = "demo-1-2-1"
	client.versionObjects["demo-1.2.1"] = objectForTest(t, wrong)

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "version")
	if check.Status != DiagnosticError ||
		!strings.Contains(check.Message, `requires resource "demo-1.2.1"`) {
		t.Fatalf("version check = %#v", check)
	}
}

func TestServiceDiagnoseMapsDependencySeverities(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	plan.Status.JobName = ""
	plan.Status.TargetNamespace = ""
	prepareDiagnosis(
		t,
		client,
		plan,
		diagnosticVersion(
			ExternalDependency{Name: "required", Version: "1.x", Required: true},
			ExternalDependency{Name: "optional", Version: "1.x"},
			ExternalDependency{Name: "service", Type: "service"},
		),
	)

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	if got := findDiagnosticCheck(t, diagnosis, "dependency/required").Status; got != DiagnosticError {
		t.Fatalf("required status = %s", got)
	}
	if got := findDiagnosticCheck(t, diagnosis, "dependency/optional").Status; got != DiagnosticWarn {
		t.Fatalf("optional status = %s", got)
	}
	if got := findDiagnosticCheck(t, diagnosis, "dependency/service").Status; got != DiagnosticInfo {
		t.Fatalf("unsupported optional status = %s", got)
	}
	if !errors.Is(diagnosis.Err(), ErrDiagnosisFailed) {
		t.Fatalf("Diagnosis.Err() = %v", diagnosis.Err())
	}
}

func TestServiceDiagnoseDependencyIncludesUnderlyingCause(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	plan.Status.JobName = ""
	plan.Status.TargetNamespace = ""
	prepareDiagnosis(
		t,
		client,
		plan,
		diagnosticVersion(ExternalDependency{
			Name:     "logging",
			Version:  "1.x",
			Required: true,
		}),
	)
	dependency := planForTest("logging", "1.4.0", "Installed")
	dependency.Spec.Extension.Name = "other"
	client.planObjects["logging"] = objectForTest(t, dependency)

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "dependency/logging")
	if check.Status != DiagnosticError ||
		!strings.Contains(check.Message, `references extension "other"`) {
		t.Fatalf("dependency check = %#v", check)
	}
}

func TestServiceDiagnoseFailedPlanIncludesCondition(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("InstallFailed")
	plan.Status.Conditions = []Condition{{
		Type:    "Installed",
		Reason:  "HelmError",
		Message: "render failed",
	}}
	prepareDiagnosis(t, client, plan, diagnosticVersion())
	client.namespaces["demo-system"] = Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
	}
	client.jobErrs["demo-system/install-demo"] = notFound("jobs", "install-demo")

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "install-plan")
	if check.Status != DiagnosticError ||
		!strings.Contains(check.Message, "Installed/HelmError: render failed") {
		t.Fatalf("install-plan check = %#v", check)
	}
}

func TestServiceDiagnoseMapsJobSeveritiesAndMissingJob(t *testing.T) {
	for _, test := range []struct {
		name       string
		state      string
		job        *Job
		wantStatus DiagnosticStatus
		wantText   string
	}{
		{
			name:  "active",
			state: "Installing",
			job: &Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "install-demo",
					Namespace: "demo-system",
				},
				Status: batchv1.JobStatus{Active: 1},
			},
			wantStatus: DiagnosticInfo,
			wantText:   "active",
		},
		{
			name:  "complete",
			state: "Installed",
			job: func() *Job {
				job := completeJob("demo-system", "install-demo", time.Now())
				return &job
			}(),
			wantStatus: DiagnosticOK,
			wantText:   "complete",
		},
		{
			name:  "complete after retry",
			state: "Installed",
			job: func() *Job {
				job := completeJob(
					"demo-system",
					"install-demo",
					time.Now(),
				)
				job.Status.Failed = 1
				return &job
			}(),
			wantStatus: DiagnosticOK,
			wantText:   "complete",
		},
		{
			name:  "failed",
			state: "InstallFailed",
			job: &Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "install-demo",
					Namespace: "demo-system",
				},
				Status: batchv1.JobStatus{
					Failed: 1,
					Conditions: []batchv1.JobCondition{{
						Type:    batchv1.JobFailed,
						Status:  corev1.ConditionTrue,
						Reason:  "BackoffLimitExceeded",
						Message: "retry limit reached",
					}},
				},
			},
			wantStatus: DiagnosticError,
			wantText:   "kubectl -n demo-system logs job/install-demo",
		},
		{
			name:       "ttl deleted after success",
			state:      "Installed",
			wantStatus: DiagnosticWarn,
			wantText:   "TTL",
		},
		{
			name:       "missing while active",
			state:      "Installing",
			wantStatus: DiagnosticError,
			wantText:   "not found",
		},
		{
			name:       "missing while failed",
			state:      "InstallFailed",
			wantStatus: DiagnosticError,
			wantText:   "not found",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			plan := diagnosticPlan(test.state)
			prepareDiagnosis(t, client, plan, diagnosticVersion())
			client.namespaces["demo-system"] = Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
			}
			if test.job == nil {
				client.jobErrs["demo-system/install-demo"] = notFound(
					"jobs",
					"install-demo",
				)
			} else {
				client.jobs["demo-system/install-demo"] = *test.job
				client.pods["demo-system/install-demo"] = PodList{}
			}

			diagnosis, err := NewService(client).Diagnose(
				context.Background(),
				"demo",
				DiagnoseOptions{},
			)
			if err != nil {
				t.Fatalf("Diagnose() error = %v", err)
			}
			check := findDiagnosticCheck(t, diagnosis, "job")
			if check.Status != test.wantStatus ||
				!strings.Contains(check.Message, test.wantText) {
				t.Fatalf("job check = %#v", check)
			}
		})
	}
}

func TestServiceDiagnoseSortsPodsAndReportsTerminations(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("InstallFailed")
	prepareDiagnosis(t, client, plan, diagnosticVersion())
	client.namespaces["demo-system"] = Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
	}
	client.jobs["demo-system/install-demo"] = Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "install-demo",
			Namespace: "demo-system",
		},
		Status: batchv1.JobStatus{Failed: 1},
	}
	client.pods["demo-system/install-demo"] = PodList{Items: []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-z"},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
				InitContainerStatuses: []corev1.ContainerStatus{{
					Name: "prepare",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "Error",
							ExitCode: 17,
						},
					},
				}},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-a"},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "installer",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "OOMKilled",
							ExitCode: 137,
						},
					},
				}},
			},
		},
	}}

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	var podChecks []DiagnosticCheck
	for _, check := range diagnosis.Checks {
		if strings.HasPrefix(check.Name, "pod/") {
			podChecks = append(podChecks, check)
		}
	}
	if got := []string{podChecks[0].Name, podChecks[1].Name}; !reflect.DeepEqual(
		got,
		[]string{"pod/pod-a", "pod/pod-z"},
	) {
		t.Fatalf("pod checks = %#v", podChecks)
	}
	for _, want := range []struct {
		index int
		text  string
	}{
		{0, "installer=OOMKilled(exit=137)"},
		{1, "init/prepare=Error(exit=17)"},
	} {
		if podChecks[want.index].Status != DiagnosticError ||
			!strings.Contains(podChecks[want.index].Message, want.text) {
			t.Fatalf("pod check = %#v, want %q", podChecks[want.index], want.text)
		}
	}
}

func TestPodDiagnosticReportsActionableWaitingReasons(t *testing.T) {
	for _, reason := range []string{
		"CrashLoopBackOff",
		"ImagePullBackOff",
		"ErrImagePull",
		"CreateContainerConfigError",
	} {
		t.Run(reason, func(t *testing.T) {
			pod := corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: "installer",
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  reason,
								Message: "executor cannot start",
							},
						},
					}},
				},
			}

			status, message := podDiagnostic(
				pod,
				"demo-system",
				"install-demo",
			)
			if status != DiagnosticError ||
				!strings.Contains(message, "installer="+reason) ||
				!strings.Contains(
					message,
					"kubectl -n demo-system logs job/install-demo",
				) {
				t.Fatalf(
					"podDiagnostic() = (%q, %q), want actionable error",
					status,
					message,
				)
			}
		})
	}
}

func TestPodDiagnosticReportsRecoveredLastTermination(t *testing.T) {
	pod := corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "installer",
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{},
				},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:   "Error",
						ExitCode: 17,
					},
				},
			}},
		},
	}

	status, message := podDiagnostic(
		pod,
		"demo-system",
		"install-demo",
	)
	if status != DiagnosticWarn ||
		!strings.Contains(message, "installer(last)=Error(exit=17)") ||
		strings.Contains(message, "kubectl") {
		t.Fatalf(
			"podDiagnostic() = (%q, %q), want recovered history warning",
			status,
			message,
		)
	}
}

func TestServiceDiagnoseCompletedJobDowngradesFailedAttemptPod(t *testing.T) {
	client := newFakeAPIClient(t)
	plan := diagnosticPlan("Installed")
	prepareDiagnosis(t, client, plan, diagnosticVersion())
	client.namespaces["demo-system"] = Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
	}
	job := completeJob("demo-system", "install-demo", time.Now())
	job.Status.Failed = 1
	client.jobs["demo-system/install-demo"] = job
	client.pods["demo-system/install-demo"] = PodList{Items: []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "failed-attempt"},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "installer",
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:   "Error",
						ExitCode: 1,
					},
				},
			}},
		},
	}}}

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if err != nil {
		t.Fatalf("Diagnose() error = %v", err)
	}
	check := findDiagnosticCheck(t, diagnosis, "pod/failed-attempt")
	if check.Status != DiagnosticWarn ||
		!strings.Contains(check.Message, "historical failed attempt") {
		t.Fatalf("pod check = %#v", check)
	}
	if err := diagnosis.Err(); err != nil {
		t.Fatalf("Diagnosis.Err() = %v", err)
	}
}

func TestPodDiagnosticKeepsNormalWaitingInformational(t *testing.T) {
	pod := corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "installer",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason: "ContainerCreating",
					},
				},
			}},
		},
	}

	status, message := podDiagnostic(
		pod,
		"demo-system",
		"install-demo",
	)
	if status != DiagnosticInfo ||
		!strings.Contains(message, "installer=ContainerCreating") ||
		strings.Contains(message, "kubectl") {
		t.Fatalf(
			"podDiagnostic() = (%q, %q), want informational wait",
			status,
			message,
		)
	}
}

func TestServiceDiagnoseDistinguishesClockSkewFromControllerLag(t *testing.T) {
	transition := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		completion time.Time
		want       string
		forbidden  string
	}{
		{
			name:       "timestamp inversion",
			completion: transition.Add(-time.Minute),
			want:       "possible clock skew",
		},
		{
			name:       "normal ordering",
			completion: transition.Add(time.Minute),
			want:       "controller lag",
			forbidden:  "clock skew",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			plan := diagnosticPlan("Installing")
			plan.Status.StateHistory = []InstallPlanState{{
				State:              "Installing",
				LastTransitionTime: metav1.Time{Time: transition},
			}}
			prepareDiagnosis(t, client, plan, diagnosticVersion())
			client.namespaces["demo-system"] = Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "demo-system"},
			}
			client.jobs["demo-system/install-demo"] = completeJob(
				"demo-system",
				"install-demo",
				test.completion,
			)
			client.pods["demo-system/install-demo"] = PodList{}

			diagnosis, err := NewService(client).Diagnose(
				context.Background(),
				"demo",
				DiagnoseOptions{},
			)
			if err != nil {
				t.Fatalf("Diagnose() error = %v", err)
			}
			check := findDiagnosticCheck(t, diagnosis, "clock")
			if check.Status != DiagnosticWarn ||
				!strings.Contains(strings.ToLower(check.Message), test.want) {
				t.Fatalf("clock check = %#v", check)
			}
			if test.forbidden != "" &&
				strings.Contains(strings.ToLower(check.Message), test.forbidden) {
				t.Fatalf("clock check = %#v, forbidden %q", check, test.forbidden)
			}
			if err := diagnosis.Err(); err != nil {
				t.Fatalf("warnings should not fail diagnosis: %v", err)
			}
		})
	}
}

func TestDiagnosisErrCountsOnlyErrors(t *testing.T) {
	warnings := Diagnosis{Checks: []DiagnosticCheck{
		{Name: "one", Status: DiagnosticWarn},
		{Name: "two", Status: DiagnosticInfo},
	}}
	if err := warnings.Err(); err != nil {
		t.Fatalf("warnings.Err() = %v", err)
	}

	failed := Diagnosis{Checks: append(
		warnings.Checks,
		DiagnosticCheck{Name: "three", Status: DiagnosticError},
		DiagnosticCheck{Name: "four", Status: DiagnosticError},
	)}
	err := failed.Err()
	if !errors.Is(err, ErrDiagnosisFailed) || !strings.Contains(err.Error(), "2") {
		t.Fatalf("failed.Err() = %v", err)
	}
}

func TestServiceDiagnoseReturnsNonNotFoundAPIErrors(t *testing.T) {
	client := newFakeAPIClient(t)
	sentinel := apierrors.NewForbidden(
		corev1.Resource("extensions"),
		"demo",
		errors.New("denied"),
	)
	client.getExtensionErrs["demo"] = sentinel

	diagnosis, err := NewService(client).Diagnose(
		context.Background(),
		"demo",
		DiagnoseOptions{},
	)
	if !apierrors.IsForbidden(err) {
		t.Fatalf("Diagnose() error = %v, want Forbidden", err)
	}
	if len(diagnosis.Checks) != 0 {
		t.Fatalf("diagnosis = %#v", diagnosis)
	}
}

func TestServiceDiagnoseValidatesNamesBeforeClientCalls(t *testing.T) {
	for _, test := range []struct {
		name    string
		options DiagnoseOptions
	}{
		{name: "bad/name"},
		{name: "demo", options: DiagnoseOptions{TargetCluster: "bad/name"}},
	} {
		client := newFakeAPIClient(t)
		_, err := NewService(client).Diagnose(
			context.Background(),
			test.name,
			test.options,
		)
		if err == nil {
			t.Fatalf("Diagnose(%q, %#v) error = nil", test.name, test.options)
		}
		if len(client.calls) != 0 {
			t.Fatalf("calls = %v", client.calls)
		}
	}
}
