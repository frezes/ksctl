package extension

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	removedClusters := make(map[string]removedWaitTarget, len(removed))
	for _, cluster := range removed {
		status, found := baseline.Value.Status.ClusterSchedulingStatuses[cluster]
		if found {
			removedClusters[cluster] = removedWaitTarget{
				Baseline: statusFingerprint(status),
			}
		}
	}
	return Operation{
		Kind:         kind,
		Name:         baseline.Value.Metadata.Name,
		Baseline:     baseline,
		acceptedUID:  baseline.Value.Metadata.UID,
		acceptedSpec: cloneInstallPlanSpec(baseline.Value.Spec),
		hasAccepted:  kind != OperationUninstall,
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

func TestEvaluateOperationWaitsWhileAdvancedFailureWillRetry(t *testing.T) {
	baselineStatus := waitStatus("UpgradeFailed", "1.2.0", "old")
	baseline := waitPlan(t, "demo", baselineStatus, nil)
	target := targetFromBaseline(
		baselineStatus,
		"1.2.1",
		"expected",
	)
	operation := waitOperation(
		OperationUpgrade,
		baseline,
		target,
		nil,
	)
	advanced := map[string]bool{}

	retryable := baseline.Value
	retryable.Status.Conditions = []Condition{{
		Type:    "Ready",
		Reason:  "OldFailureUpdated",
		Message: "old attempt metadata was reconciled",
	}}
	done, err := evaluateOperation(operation, retryable, advanced)
	if done || err != nil {
		t.Fatalf(
			"retryable evaluateOperation() = (%t, %v), want continue",
			done,
			err,
		)
	}

	terminal := retryable
	terminal.Status.Version = "1.2.1"
	terminal.Status.ConfigHash = "expected"
	done, err = evaluateOperation(operation, terminal, advanced)
	var lifecycleErr *LifecycleFailureError
	if done || !errors.As(err, &lifecycleErr) {
		t.Fatalf(
			"terminal evaluateOperation() = (%t, %v), want failure",
			done,
			err,
		)
	}
}

func TestServiceWaitConfigureRequiresConfigHashChange(t *testing.T) {
	old := waitStatus("Installed", "1.2.1", "old")
	baseline := waitPlan(t, "demo", old, nil)
	operation := waitOperation(
		OperationConfigure,
		baseline,
		targetFromBaseline(old, "1.2.1", "expected"),
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

	t.Run("different but unexpected hash times out", func(t *testing.T) {
		client := newFakeAPIClient(t)
		queuePlanObjects(
			client,
			"demo",
			waitPlan(
				t,
				"demo",
				waitStatus("Installed", "1.2.1", "unexpected"),
				nil,
			),
		)
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

	t.Run("expected hash succeeds", func(t *testing.T) {
		client := newFakeAPIClient(t)
		changed := waitPlan(
			t,
			"demo",
			waitStatus("Installed", "1.2.1", "expected"),
			nil,
		)
		queuePlanObjects(client, "demo", changed)

		_, err := pollingService(client, pollingTicks(0)).Wait(
			context.Background(),
			operation,
			PollOptions{Timeout: 20 * time.Millisecond},
		)
		if err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})
}

func TestEvaluateOperationReportsFailureBeforePendingTarget(t *testing.T) {
	host := waitStatus("Preparing", "", "")
	clusterOld := waitStatus("Installed", "1.2.0", "old")
	baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
		"member-b": clusterOld,
		"member-a": clusterOld,
	})
	failedPlan := baseline.Value
	failedPlan.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
		"member-b": waitStatus("UpgradeFailed", "1.2.1", "new-b"),
		"member-a": waitStatus("UpgradeFailed", "1.2.1", "new-a"),
	}

	done, err := evaluateOperation(
		waitOperation(
			OperationUpgrade,
			baseline,
			targetFromBaseline(host, "1.2.1", ""),
			map[string]waitTarget{
				"member-b": *targetFromBaseline(clusterOld, "1.2.1", ""),
				"member-a": *targetFromBaseline(clusterOld, "1.2.1", ""),
			},
		),
		failedPlan,
		map[string]bool{},
	)
	if done {
		t.Fatal("evaluateOperation() done = true")
	}
	if err == nil || !strings.Contains(err.Error(), "cluster/member-a") {
		t.Fatalf("evaluateOperation() error = %v, want deterministic member-a failure", err)
	}
}

func TestServiceWaitRemovedClusterFailureUsesAcceptedBaseline(t *testing.T) {
	host := waitStatus("Installed", "1.2.1", "host")

	t.Run("unchanged stale failure is ignored", func(t *testing.T) {
		stale := waitStatus("InstallFailed", "1.2.1", "old")
		baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-b": stale,
		})
		client := newFakeAPIClient(t)
		queuePlanObjects(client, "demo", baseline)
		expired, cancel := context.WithDeadline(
			context.Background(),
			time.Now().Add(-time.Second),
		)
		defer cancel()

		_, err := pollingService(client, pollingTicks(0)).Wait(
			expired,
			waitOperation(
				OperationConfigure,
				baseline,
				nil,
				nil,
				"member-b",
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
	})

	t.Run("advanced failure is immediate", func(t *testing.T) {
		old := waitStatus("Installed", "1.2.1", "old")
		baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-b": old,
		})
		failedStatus := waitStatus("UninstallFailed", "1.2.1", "old")
		failed := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-b": failedStatus,
		})
		client := newFakeAPIClient(t)
		queuePlanObjects(client, "demo", failed)

		_, err := pollingService(client, pollingTicks(0)).Wait(
			context.Background(),
			waitOperation(
				OperationConfigure,
				baseline,
				nil,
				nil,
				"member-b",
			),
			PollOptions{Timeout: 20 * time.Millisecond},
		)
		var lifecycleErr *LifecycleFailureError
		if !errors.As(err, &lifecycleErr) ||
			!strings.Contains(err.Error(), "cluster/member-b") {
			t.Fatalf("Wait() error = %v, want removed-cluster failure", err)
		}
	})
}

func TestEvaluateRemovedClusterUsesUninstallFailureSemantics(t *testing.T) {
	host := waitStatus("Installed", "1.2.1", "host")

	t.Run("baseline uninstall failure is terminal", func(t *testing.T) {
		failed := waitStatus("UninstallFailed", "1.2.1", "old")
		baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-b": failed,
		})
		done, err := evaluateOperation(
			waitOperation(
				OperationConfigure,
				baseline,
				nil,
				nil,
				"member-b",
			),
			baseline.Value,
			map[string]bool{},
		)
		var lifecycleErr *LifecycleFailureError
		if done || !errors.As(err, &lifecycleErr) ||
			!strings.Contains(err.Error(), "cluster/member-b") {
			t.Fatalf("evaluateOperation() = (%t, %v)", done, err)
		}
	})

	t.Run("retained uninstall failure is terminal", func(t *testing.T) {
		failed := waitStatus("UninstallFailed", "1.2.1", "old")
		baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
			"member-b": failed,
		})
		done, err := evaluateOperation(
			waitOperation(
				OperationUpgrade,
				baseline,
				nil,
				map[string]waitTarget{
					"member-b": *targetFromBaseline(
						failed,
						"1.2.1",
						"",
					),
				},
			),
			baseline.Value,
			map[string]bool{},
		)
		var lifecycleErr *LifecycleFailureError
		if done || !errors.As(err, &lifecycleErr) ||
			!strings.Contains(err.Error(), "cluster/member-b") {
			t.Fatalf("evaluateOperation() = (%t, %v)", done, err)
		}
	})

	for _, state := range []string{"InstallFailed", "UpgradeFailed"} {
		t.Run("advanced "+state+" still proceeds to uninstall", func(t *testing.T) {
			old := waitStatus("Installed", "1.2.1", "old")
			baseline := waitPlan(t, "demo", host, map[string]InstallationStatus{
				"member-b": old,
			})
			operation := waitOperation(
				OperationConfigure,
				baseline,
				nil,
				nil,
				"member-b",
			)
			changed := baseline.Value
			failed := waitStatus(state, "1.2.1", "new")
			changed.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
				"member-b": failed,
			}
			done, err := evaluateOperation(
				operation,
				changed,
				map[string]bool{},
			)
			if done || err != nil {
				t.Fatalf("evaluateOperation() = (%t, %v)", done, err)
			}
		})
	}
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

func TestEvaluateOperationRejectsSupersededAcceptedPlan(t *testing.T) {
	hash := controllerConfigHash([]byte("feature: true\n"))
	status := waitStatus("Installed", "1.2.1", hash)
	baseline := waitPlan(t, "demo", status, nil)
	baseline.Value.Metadata.UID = "uid-a"
	baseline.Value.Spec.Config = "feature: true\n"
	operation := waitOperation(
		OperationUpgrade,
		baseline,
		&waitTarget{
			Baseline:   statusFingerprint(status),
			Version:    "1.2.1",
			ConfigHash: hash,
		},
		map[string]waitTarget{},
	)

	tests := []struct {
		name   string
		mutate func(*InstallPlan)
	}{
		{
			name: "accepted spec was replaced",
			mutate: func(plan *InstallPlan) {
				plan.Spec.Extension.Version = "1.2.2"
			},
		},
		{
			name: "accepted plan is being deleted",
			mutate: func(plan *InstallPlan) {
				now := metav1.Now()
				plan.Metadata.DeletionTimestamp = &now
			},
		},
		{
			name: "accepted plan was recreated",
			mutate: func(plan *InstallPlan) {
				plan.Metadata.UID = "uid-b"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			live := baseline.Value
			test.mutate(&live)
			done, err := evaluateOperation(
				operation,
				live,
				map[string]bool{},
			)
			if done {
				t.Fatal("evaluateOperation() done = true")
			}
			if err == nil || !strings.Contains(err.Error(), "superseded") {
				t.Fatalf(
					"evaluateOperation() error = %v, want superseded",
					err,
				)
			}
		})
	}
}

func TestEvaluateUninstallRejectsRecreatedPlan(t *testing.T) {
	baseline := waitPlan(
		t,
		"demo",
		waitStatus("Uninstalling", "1.2.1", ""),
		nil,
	)
	baseline.Value.Metadata.UID = "uid-a"
	operation := waitOperation(
		OperationUninstall,
		baseline,
		nil,
		map[string]waitTarget{},
	)
	recreated := baseline.Value
	recreated.Metadata.UID = "uid-b"
	recreated.Status.State = "Installed"

	done, err := evaluateOperation(
		operation,
		recreated,
		map[string]bool{},
	)
	if done {
		t.Fatal("evaluateOperation() done = true")
	}
	if err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("evaluateOperation() error = %v, want superseded", err)
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
