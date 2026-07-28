package extension

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func waitStatus(state, version, configHash string) InstallationStatus {
	return InstallationStatus{
		State:      state,
		Version:    version,
		ConfigHash: configHash,
	}
}

func waitPlan(
	t testing.TB,
	name string,
	host InstallationStatus,
	clusters map[string]InstallationStatus,
) Object[InstallPlan] {
	t.Helper()
	plan := planForTest(name, "1.2.1", host.State)
	plan.Status.InstallationStatus = host
	plan.Status.ClusterSchedulingStatuses = clusters
	return objectForTest(t, plan)
}

func queuePlanObjects(
	client *fakeAPIClient,
	name string,
	objects ...Object[InstallPlan],
) {
	for _, object := range objects {
		client.planReads[name] = append(
			client.planReads[name],
			fakePlanRead{object: object},
		)
	}
}

func pollingTicks(count int) <-chan time.Time {
	ticks := make(chan time.Time, count)
	for range count {
		ticks <- time.Time{}
	}
	return ticks
}

func pollingService(client APIClient, ticks <-chan time.Time) *Service {
	return newServiceForPolling(
		client,
		time.Millisecond,
		func(time.Duration) <-chan time.Time { return ticks },
	)
}

func waitOperation(
	kind OperationKind,
	baseline Object[InstallPlan],
	host *waitTarget,
	clusters map[string]waitTarget,
	removed ...string,
) Operation {
	removedClusters := make(map[string]struct{}, len(removed))
	for _, cluster := range removed {
		removedClusters[cluster] = struct{}{}
	}
	return Operation{
		Kind:     kind,
		Name:     baseline.Value.Metadata.Name,
		Baseline: baseline,
		expectation: waitExpectation{
			Host:            host,
			Clusters:        clusters,
			RemovedClusters: removedClusters,
		},
	}
}

func targetFromBaseline(
	status InstallationStatus,
	version string,
	configHash string,
) *waitTarget {
	return &waitTarget{
		Baseline:    statusFingerprint(status),
		Version:     version,
		ConfigHash:  configHash,
		MustAdvance: true,
	}
}

func TestServiceWaitInstallTransitionsToInstalled(t *testing.T) {
	client := newFakeAPIClient(t)
	empty := waitStatus("", "", "")
	baseline := waitPlan(t, "demo", empty, nil)
	preparing := waitPlan(t, "demo", waitStatus("Preparing", "", ""), nil)
	installing := waitPlan(t, "demo", waitStatus("Installing", "1.2.1", ""), nil)
	installed := waitPlan(t, "demo", waitStatus("Installed", "1.2.1", "new"), nil)
	queuePlanObjects(client, "demo", baseline, preparing, installing, installed)

	var states []string
	result, err := pollingService(client, pollingTicks(3)).Wait(
		context.Background(),
		waitOperation(
			OperationInstall,
			baseline,
			targetFromBaseline(empty, "1.2.1", ""),
			nil,
		),
		PollOptions{
			Timeout: time.Minute,
			OnState: func(event StateEvent) error {
				states = append(states, event.State)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Plan == nil || result.Deleted ||
		result.Plan.Value.Status.State != "Installed" {
		t.Fatalf("result = %#v", result)
	}
	if want := []string{"Preparing", "Installing", "Installed"}; !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}

func TestServiceWaitIgnoresUnchangedStaleFailure(t *testing.T) {
	client := newFakeAPIClient(t)
	stale := waitStatus("UpgradeFailed", "1.2.0", "old")
	baseline := waitPlan(t, "demo", stale, nil)
	queuePlanObjects(client, "demo", baseline)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := pollingService(client, pollingTicks(0)).Wait(
		expired,
		waitOperation(
			OperationUpgrade,
			baseline,
			targetFromBaseline(stale, "1.2.1", ""),
			nil,
		),
		PollOptions{Timeout: time.Minute},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() error = %v, want DeadlineExceeded", err)
	}
	var lifecycleErr *LifecycleFailureError
	if errors.As(err, &lifecycleErr) {
		t.Fatalf("Wait() returned stale lifecycle failure: %v", err)
	}
}

func TestServiceWaitAcceptsDirectTargetSuccess(t *testing.T) {
	client := newFakeAPIClient(t)
	stale := waitStatus("Installed", "1.2.0", "old")
	baseline := waitPlan(t, "demo", stale, nil)
	success := waitPlan(t, "demo", waitStatus("Installed", "1.2.1", "new"), nil)
	queuePlanObjects(client, "demo", success)

	result, err := pollingService(client, pollingTicks(0)).Wait(
		context.Background(),
		waitOperation(
			OperationUpgrade,
			baseline,
			targetFromBaseline(stale, "1.2.1", ""),
			nil,
		),
		PollOptions{Timeout: time.Minute},
	)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Plan == nil || result.Plan.Value.Status.Version != "1.2.1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceWaitReturnsAdvancedLifecycleFailure(t *testing.T) {
	client := newFakeAPIClient(t)
	stale := waitStatus("Installed", "1.2.0", "old")
	baseline := waitPlan(t, "demo", stale, nil)
	upgrading := waitPlan(t, "demo", waitStatus("Upgrading", "1.2.1", "old"), nil)
	failedStatus := waitStatus("UpgradeFailed", "1.2.1", "old")
	failedStatus.TargetNamespace = "extension-demo"
	failedStatus.JobName = "demo-upgrade"
	failedStatus.Conditions = []Condition{{
		Type:    "Ready",
		Reason:  "HelmError",
		Message: "upgrade failed",
	}}
	failed := waitPlan(t, "demo", failedStatus, nil)
	queuePlanObjects(client, "demo", upgrading, failed)

	_, err := pollingService(client, pollingTicks(1)).Wait(
		context.Background(),
		waitOperation(
			OperationUpgrade,
			baseline,
			targetFromBaseline(stale, "1.2.1", ""),
			nil,
		),
		PollOptions{Timeout: time.Minute},
	)
	var lifecycleErr *LifecycleFailureError
	if !errors.As(err, &lifecycleErr) {
		t.Fatalf("Wait() error = %v, want LifecycleFailureError", err)
	}
	for _, want := range []string{
		"UpgradeFailed",
		"namespace=extension-demo",
		"job=demo-upgrade",
		"Ready/HelmError: upgrade failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Wait() error = %v, want %q", err, want)
		}
	}
}

func TestServiceWaitConfigureRequiresConfigHashChange(t *testing.T) {
	old := waitStatus("Installed", "1.2.1", "old")
	baseline := waitPlan(t, "demo", old, nil)
	operation := waitOperation(
		OperationConfigure,
		baseline,
		targetFromBaseline(old, "1.2.1", "old"),
		nil,
	)

	t.Run("unchanged times out", func(t *testing.T) {
		client := newFakeAPIClient(t)
		queuePlanObjects(client, "demo", baseline)
		expired, cancel := context.WithDeadline(
			context.Background(),
			time.Now().Add(-time.Second),
		)
		defer cancel()

		_, err := pollingService(client, pollingTicks(0)).Wait(
			expired,
			operation,
			PollOptions{Timeout: time.Minute},
		)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait() error = %v, want DeadlineExceeded", err)
		}
	})

	t.Run("changed succeeds", func(t *testing.T) {
		client := newFakeAPIClient(t)
		changed := waitPlan(
			t,
			"demo",
			waitStatus("Installed", "1.2.1", "new"),
			nil,
		)
		queuePlanObjects(client, "demo", changed)

		_, err := pollingService(client, pollingTicks(0)).Wait(
			context.Background(),
			operation,
			PollOptions{Timeout: time.Minute},
		)
		if err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})
}

func TestServiceWaitTracksClusterChangesAndRemoval(t *testing.T) {
	host := waitStatus("Installed", "1.2.1", "host")
	clusterOld := waitStatus("Installed", "1.2.1", "old")
	baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
		"member-a": clusterOld,
		"member-b": clusterOld,
	})

	t.Run("cluster advances", func(t *testing.T) {
		client := newFakeAPIClient(t)
		unchanged := baseline
		changed := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-a": waitStatus("Installed", "1.2.1", "new"),
			"member-b": clusterOld,
		})
		queuePlanObjects(client, "demo", unchanged, changed)

		_, err := pollingService(client, pollingTicks(1)).Wait(
			context.Background(),
			waitOperation(
				OperationConfigure,
				baseline,
				nil,
				map[string]waitTarget{
					"member-a": *targetFromBaseline(clusterOld, "1.2.1", ""),
				},
			),
			PollOptions{Timeout: time.Minute},
		)
		if err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})

	t.Run("removed key disappears", func(t *testing.T) {
		client := newFakeAPIClient(t)
		removed := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-a": clusterOld,
		})
		queuePlanObjects(client, "demo", baseline, removed)

		_, err := pollingService(client, pollingTicks(1)).Wait(
			context.Background(),
			waitOperation(
				OperationConfigure,
				baseline,
				nil,
				nil,
				"member-b",
			),
			PollOptions{Timeout: time.Minute},
		)
		if err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})
}

func TestServiceWaitReportsClusterFailureLocation(t *testing.T) {
	client := newFakeAPIClient(t)
	host := waitStatus("Installed", "1.2.1", "host")
	clusterOld := waitStatus("Installed", "1.2.1", "old")
	baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
		"member-a": clusterOld,
	})
	clusterFailed := waitStatus("InstallFailed", "1.2.1", "new")
	clusterFailed.TargetNamespace = "demo-system"
	clusterFailed.JobName = "demo-install"
	failed := waitPlan(t, "demo", host, map[string]InstallationStatus{
		"member-a": clusterFailed,
	})
	queuePlanObjects(client, "demo", failed)

	_, err := pollingService(client, pollingTicks(0)).Wait(
		context.Background(),
		waitOperation(
			OperationConfigure,
			baseline,
			nil,
			map[string]waitTarget{
				"member-a": *targetFromBaseline(clusterOld, "1.2.1", ""),
			},
		),
		PollOptions{Timeout: time.Minute},
	)
	for _, want := range []string{
		"cluster/member-a",
		"namespace=demo-system",
		"job=demo-install",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Wait() error = %v, want %q", err, want)
		}
	}
}

func TestServiceWaitUninstallRequiresNotFound(t *testing.T) {
	t.Run("old failures and Uninstalled continue", func(t *testing.T) {
		client := newFakeAPIClient(t)
		baseline := waitPlan(
			t,
			"demo",
			waitStatus("InstallFailed", "1.2.1", "old"),
			map[string]InstallationStatus{
				"member-a": waitStatus("UpgradeFailed", "1.2.1", "old"),
			},
		)
		uninstalled := waitPlan(
			t,
			"demo",
			waitStatus("Uninstalled", "1.2.1", "old"),
			nil,
		)
		client.planReads["demo"] = []fakePlanRead{
			{object: baseline},
			{object: uninstalled},
			{err: notFound("installplans", "demo")},
		}

		result, err := pollingService(client, pollingTicks(2)).Wait(
			context.Background(),
			waitOperation(OperationUninstall, baseline, nil, nil),
			PollOptions{Timeout: time.Minute},
		)
		if err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
		if !result.Deleted || result.Plan != nil {
			t.Fatalf("result = %#v", result)
		}
	})

	for _, test := range []struct {
		name     string
		host     InstallationStatus
		clusters map[string]InstallationStatus
		want     string
	}{
		{
			name: "host",
			host: waitStatus("UninstallFailed", "1.2.1", "old"),
			want: "host",
		},
		{
			name: "cluster",
			host: waitStatus("Uninstalling", "1.2.1", "old"),
			clusters: map[string]InstallationStatus{
				"member-a": waitStatus("UninstallFailed", "1.2.1", "old"),
			},
			want: "cluster/member-a",
		},
	} {
		t.Run(test.name+" failure", func(t *testing.T) {
			client := newFakeAPIClient(t)
			baseline := waitPlan(t, "demo", test.host, test.clusters)
			queuePlanObjects(client, "demo", baseline)

			_, err := pollingService(client, pollingTicks(0)).Wait(
				context.Background(),
				waitOperation(OperationUninstall, baseline, nil, nil),
				PollOptions{Timeout: time.Minute},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Wait() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestServiceWaitContextAndCallbackErrors(t *testing.T) {
	baselineStatus := waitStatus("", "", "")
	baseline := waitPlan(t, "demo", baselineStatus, nil)
	operation := waitOperation(
		OperationInstall,
		baseline,
		targetFromBaseline(baselineStatus, "1.2.1", ""),
		nil,
	)

	t.Run("canceled", func(t *testing.T) {
		client := newFakeAPIClient(t)
		queuePlanObjects(client, "demo", baseline)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := pollingService(client, pollingTicks(0)).Wait(
			ctx,
			operation,
			PollOptions{Timeout: time.Minute},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Wait() error = %v, want context.Canceled", err)
		}
	})

	t.Run("callback", func(t *testing.T) {
		client := newFakeAPIClient(t)
		preparing := waitPlan(t, "demo", waitStatus("Preparing", "", ""), nil)
		queuePlanObjects(client, "demo", preparing)
		sentinel := errors.New("write failed")

		_, err := pollingService(client, pollingTicks(0)).Wait(
			context.Background(),
			operation,
			PollOptions{
				Timeout: time.Minute,
				OnState: func(StateEvent) error {
					return sentinel
				},
			},
		)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Wait() error = %v, want callback error", err)
		}
	})
}

func TestServiceWaitRejectsNonPositiveTimeout(t *testing.T) {
	client := newFakeAPIClient(t)
	_, err := pollingService(client, pollingTicks(0)).Wait(
		context.Background(),
		Operation{Name: "demo"},
		PollOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("Wait() error = %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("calls = %v", client.calls)
	}
}

func TestServiceWatchEmitsDistinctStatesAndSucceeds(t *testing.T) {
	client := newFakeAPIClient(t)
	empty := waitPlan(t, "demo", waitStatus("", "", ""), nil)
	preparing := waitPlan(t, "demo", waitStatus("Preparing", "", ""), nil)
	installed := waitPlan(t, "demo", waitStatus("Installed", "1.2.1", "new"), nil)
	queuePlanObjects(client, "demo", empty, empty, preparing, installed)
	var states []string

	result, err := pollingService(client, pollingTicks(3)).Watch(
		context.Background(),
		"demo",
		PollOptions{
			Timeout: time.Minute,
			OnState: func(event StateEvent) error {
				states = append(states, event.State)
				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	if result.Value.Status.State != "Installed" {
		t.Fatalf("result = %#v", result.Value)
	}
	if want := []string{"", "Preparing", "Installed"}; !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}

func TestServiceWatchReturnsFailureAndNotFound(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		client := newFakeAPIClient(t)
		failed := waitPlan(
			t,
			"demo",
			waitStatus("InstallFailed", "1.2.1", "old"),
			nil,
		)
		queuePlanObjects(client, "demo", failed)

		_, err := pollingService(client, pollingTicks(0)).Watch(
			context.Background(),
			"demo",
			PollOptions{Timeout: time.Minute},
		)
		var lifecycleErr *LifecycleFailureError
		if !errors.As(err, &lifecycleErr) {
			t.Fatalf("Watch() error = %v, want LifecycleFailureError", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		client := newFakeAPIClient(t)
		client.planReads["missing"] = []fakePlanRead{{
			err: notFound("installplans", "missing"),
		}}

		_, err := pollingService(client, pollingTicks(0)).Watch(
			context.Background(),
			"missing",
			PollOptions{Timeout: time.Minute},
		)
		if !apierrors.IsNotFound(err) {
			t.Fatalf("Watch() error = %v, want NotFound", err)
		}
	})
}

func TestServiceWatchReturnsCallbackError(t *testing.T) {
	client := newFakeAPIClient(t)
	empty := waitPlan(t, "demo", waitStatus("", "", ""), nil)
	queuePlanObjects(client, "demo", empty)
	sentinel := errors.New("write failed")

	_, err := pollingService(client, pollingTicks(0)).Watch(
		context.Background(),
		"demo",
		PollOptions{
			Timeout: time.Minute,
			OnState: func(StateEvent) error {
				return sentinel
			},
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Watch() error = %v, want callback error", err)
	}
}
