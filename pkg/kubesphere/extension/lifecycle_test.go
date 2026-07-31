package extension

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func configHashForTest(value string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return hex.EncodeToString(hash.Sum(nil))
}

func prepareExtensionForLifecycle(
	t *testing.T,
	client *fakeAPIClient,
	name string,
	versions ...ExtensionVersion,
) {
	t.Helper()
	client.extensionObjects[name] = objectForTest(t, extensionForTest(name))
	client.versions[name] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		versions...,
	)
	for _, version := range versions {
		client.versionObjects[version.Metadata.Name] = objectForTest(t, version)
	}
}

func lifecycleVersion(
	extension string,
	version string,
	mode string,
	dependencies ...ExternalDependency,
) ExtensionVersion {
	result := versionForTest(extension, extension+"-"+version, version)
	result.Spec.InstallationMode = mode
	result.Spec.ExternalDependencies = dependencies
	return result
}

func clusterForTest(
	name string,
	deletionTimestamp *metav1.Time,
	conditions ...ClusterCondition,
) Cluster {
	return Cluster{
		Metadata: ObjectMeta{
			Name:              name,
			DeletionTimestamp: deletionTimestamp,
		},
		Status: ClusterStatus{Conditions: conditions},
	}
}

func TestServiceInstallAllClustersUsesEligibleSortedSnapshotIncludingHost(
	t *testing.T,
) {
	client := newFakeAPIClient(t)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "Multicluster"),
	)
	client.clusters = listForTest(
		t,
		"cluster.kubesphere.io/v1alpha1",
		"ClusterList",
		clusterForTest("member-z", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"}),
		clusterForTest("host", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"}),
		clusterForTest("not-ready", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "False"}),
		clusterForTest("unschedulable", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"},
			ClusterCondition{Type: "Schedulable", Status: "False"}),
		clusterForTest("member-a", nil,
			ClusterCondition{Type: "KSCoreReady", Status: "True"}),
	)

	_, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{Version: "1.2.1", AllClusters: true},
	)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	got := client.createdPlans[0].Spec.ClusterScheduling.Placement.Clusters
	want := []string{"host", "member-a", "member-z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("clusters = %#v, want %#v", got, want)
	}
}

func TestServiceInstallAllClustersRejectsInvalidSelectionWithoutCreate(
	t *testing.T,
) {
	now := metav1.Now()
	tests := []struct {
		name      string
		mode      string
		clusters  []Cluster
		overrides map[string]string
		want      string
	}{
		{
			name: "HostOnly",
			mode: "HostOnly",
			clusters: []Cluster{clusterForTest(
				"host",
				nil,
				ClusterCondition{Type: "KSCoreReady", Status: "True"},
			)},
			want: "--all-clusters",
		},
		{
			name: "no eligible Clusters",
			mode: "Multicluster",
			clusters: []Cluster{
				clusterForTest(
					"deleting",
					&now,
					ClusterCondition{Type: "KSCoreReady", Status: "True"},
				),
				clusterForTest(
					"not-ready",
					nil,
					ClusterCondition{Type: "KSCoreReady", Status: "False"},
				),
			},
			want: "no ready, schedulable Clusters",
		},
		{
			name: "override outside selection",
			mode: "Multicluster",
			clusters: []Cluster{clusterForTest(
				"member-a",
				nil,
				ClusterCondition{Type: "KSCoreReady", Status: "True"},
			)},
			overrides: map[string]string{"member-b": "key: value\n"},
			want:      "member-b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			prepareExtensionForLifecycle(
				t,
				client,
				"demo",
				lifecycleVersion("demo", "1.2.1", test.mode),
			)
			client.clusters = listForTest(
				t,
				"cluster.kubesphere.io/v1alpha1",
				"ClusterList",
				test.clusters...,
			)
			_, err := NewService(client).Install(
				context.Background(),
				"demo",
				InstallOptions{
					Version:     "1.2.1",
					AllClusters: true,
					Overrides:   test.overrides,
				},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if len(client.createdPlans) != 0 {
				t.Fatalf("created plans = %#v", client.createdPlans)
			}
		})
	}
}

func TestServiceInstallCreatesExactEnabledManualPlan(t *testing.T) {
	client := newFakeAPIClient(t)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "Multicluster"),
	)
	extension := extensionForTest("demo")
	extension.Status.RecommendedVersion = "2.0.0"
	client.extensionObjects["demo"] = objectForTest(t, extension)
	config := "key: value\r\n"

	operation, err := NewService(client).Install(context.Background(), "demo", InstallOptions{
		Version:   "1.2.1",
		Config:    &config,
		Clusters:  []string{"member-a"},
		Overrides: map[string]string{"member-a": "key: override\r\n"},
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(client.createdPlans) != 1 {
		t.Fatalf("created plans = %#v", client.createdPlans)
	}
	created := client.createdPlans[0]
	if created.Metadata.Name != "demo" ||
		created.Spec.Extension != (ExtensionRef{Name: "demo", Version: "1.2.1"}) ||
		!created.Spec.Enabled ||
		created.Spec.UpgradeStrategy != "Manual" ||
		created.Spec.Config != "key: value\n" {
		t.Fatalf("created plan = %#v", created)
	}
	if created.Spec.ClusterScheduling == nil ||
		created.Spec.ClusterScheduling.Overrides["member-a"] != "key: override\n" {
		t.Fatalf("created scheduling = %#v", created.Spec.ClusterScheduling)
	}
	if operation.Kind != OperationInstall || operation.Name != "demo" ||
		operation.TargetVersion != "1.2.1" {
		t.Fatalf("operation = %#v", operation)
	}
}

func TestServiceInstallUsesRecommendedVersionWhenVersionIsOmitted(t *testing.T) {
	client := newFakeAPIClient(t)
	version := lifecycleVersion("demo", "1.2.1", "HostOnly")
	prepareExtensionForLifecycle(t, client, "demo", version)
	extension := extensionForTest("demo")
	extension.Status.RecommendedVersion = "1.2.1"
	client.extensionObjects["demo"] = objectForTest(t, extension)

	operation, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{},
	)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := client.createdPlans[0].Spec.Extension.Version; got != "1.2.1" {
		t.Fatalf("created version = %q, want 1.2.1", got)
	}
	if operation.TargetVersion != "1.2.1" {
		t.Fatalf("operation target = %q, want 1.2.1", operation.TargetVersion)
	}
}

func TestServiceInstallRejectsMissingRecommendedVersionWithoutWrite(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensionObjects["demo"] = objectForTest(t, extensionForTest("demo"))

	_, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "extension versions demo") {
		t.Fatalf("Install() error = %v, want versions hint", err)
	}
	if len(client.createdPlans) != 0 {
		t.Fatalf("created plans = %#v", client.createdPlans)
	}
	for _, call := range client.calls {
		if strings.HasPrefix(call, "get extension version ") {
			t.Fatalf("unexpected version lookup: %q", call)
		}
	}
}

func TestServiceInstallRejectsPreflightFailuresWithoutCreate(t *testing.T) {
	for _, test := range []struct {
		name      string
		version   ExtensionVersion
		options   InstallOptions
		configure func(*fakeAPIClient)
		want      string
	}{
		{
			name:    "exact version absent",
			version: lifecycleVersion("demo", "1.2.0", "HostOnly"),
			options: InstallOptions{Version: "1.2.1"},
			want:    "not found",
		},
		{
			name:    "HostOnly scheduling",
			version: lifecycleVersion("demo", "1.2.1", "HostOnly"),
			options: InstallOptions{Version: "1.2.1", Clusters: []string{"member-a"}},
			want:    "does not accept cluster scheduling",
		},
		{
			name: "required dependency",
			version: lifecycleVersion(
				"demo",
				"1.2.1",
				"HostOnly",
				ExternalDependency{Name: "logging", Version: ">=1", Required: true},
			),
			options: InstallOptions{Version: "1.2.1"},
			want:    "dependencies",
		},
		{
			name:    "existing plan",
			version: lifecycleVersion("demo", "1.2.1", "HostOnly"),
			options: InstallOptions{Version: "1.2.1"},
			configure: func(client *fakeAPIClient) {
				client.planObjects["demo"] = objectForTest(
					t,
					planForTest("demo", "1.0.0", "Installed"),
				)
			},
			want: "extension upgrade",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			prepareExtensionForLifecycle(t, client, "demo", test.version)
			if test.configure != nil {
				test.configure(client)
			}

			_, err := NewService(client).Install(
				context.Background(),
				"demo",
				test.options,
			)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if len(client.createdPlans) != 0 {
				t.Fatalf("created plans = %#v", client.createdPlans)
			}
		})
	}
}

func TestServiceInstallRejectsUnexpectedAcceptedPlan(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*InstallPlan)
		want   string
	}{
		{
			name: "extension reference",
			mutate: func(plan *InstallPlan) {
				plan.Spec.Extension.Name = "other"
			},
			want: `references extension "other"`,
		},
		{
			name: "version",
			mutate: func(plan *InstallPlan) {
				plan.Spec.Extension.Version = "9.9.9"
			},
			want: `accepted version "9.9.9", want "1.2.1"`,
		},
		{
			name: "disabled",
			mutate: func(plan *InstallPlan) {
				plan.Spec.Enabled = false
			},
			want: "accepted enabled=false, want true",
		},
		{
			name: "automatic strategy",
			mutate: func(plan *InstallPlan) {
				plan.Spec.UpgradeStrategy = "Automatic"
			},
			want: `accepted upgradeStrategy "Automatic", want "Manual"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			prepareExtensionForLifecycle(
				t,
				client,
				"demo",
				lifecycleVersion("demo", "1.2.1", "HostOnly"),
			)
			accepted := planForTest("demo", "1.2.1", "")
			test.mutate(&accepted)
			response := objectForTest(t, accepted)
			client.createResponse = &response

			_, err := NewService(client).Install(
				context.Background(),
				"demo",
				InstallOptions{Version: "1.2.1"},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if len(client.createdPlans) != 1 {
				t.Fatalf("created plans = %#v", client.createdPlans)
			}
		})
	}
}

func TestServiceInstallRejectsMutatedAcceptedConfiguration(t *testing.T) {
	client := newFakeAPIClient(t)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "Multicluster"),
	)
	accepted := planForTest("demo", "1.2.1", "")
	accepted.Spec.Config = "key: other\n"
	response := objectForTest(t, accepted)
	client.createResponse = &response
	config := "key: requested\n"

	_, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{
			Version:  "1.2.1",
			Config:   &config,
			Clusters: []string{"member-a"},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "accepted config differs") {
		t.Fatalf("Install() error = %v, want mutated config rejection", err)
	}
}

func TestServiceInstallRejectsMutatedAcceptedScheduling(t *testing.T) {
	client := newFakeAPIClient(t)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "Multicluster"),
	)
	accepted := planForTest("demo", "1.2.1", "")
	response := objectForTest(t, accepted)
	client.createResponse = &response

	_, err := NewService(client).Install(
		context.Background(),
		"demo",
		InstallOptions{
			Version:  "1.2.1",
			Clusters: []string{"member-a"},
		},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "accepted cluster scheduling differs") {
		t.Fatalf("Install() error = %v, want scheduling rejection", err)
	}
}

func TestServiceUpgradeSendsMinimalResourceVersionPatch(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Metadata.ResourceVersion = "19"
	client.planObjects["demo"] = objectForTest(t, current)
	target := lifecycleVersion("demo", "1.2.1", "HostOnly")
	prepareExtensionForLifecycle(t, client, "demo", target)
	updated := current
	updated.Spec.Extension.Version = "1.2.1"
	updated.Metadata.ResourceVersion = "20"
	client.patchResponses["demo"] = objectForTest(t, updated)

	operation, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
	})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(client.patches) != 1 {
		t.Fatalf("patches = %q", client.patches)
	}
	var patch map[string]any
	if err := json.Unmarshal(client.patches[0], &patch); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"metadata": map[string]any{"resourceVersion": "19"},
		"spec": map[string]any{
			"extension":       map[string]any{"version": "1.2.1"},
			"upgradeStrategy": "Manual",
		},
	}
	if !reflect.DeepEqual(patch, want) {
		t.Fatalf("patch = %#v, want %#v", patch, want)
	}
	if operation.Kind != OperationUpgrade ||
		operation.Baseline.Value.Metadata.ResourceVersion != "20" {
		t.Fatalf("operation = %#v", operation)
	}
}

func TestServiceUpgradeRejectsUnexpectedAcceptedPlan(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "HostOnly"),
	)
	accepted := current
	accepted.Spec.Extension.Version = "1.2.0"
	client.patchResponses["demo"] = objectForTest(t, accepted)

	_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
	})
	if err == nil ||
		!strings.Contains(err.Error(), `accepted version "1.2.0", want "1.2.1"`) {
		t.Fatalf("Upgrade() error = %v", err)
	}
}

func TestServiceUpgradePreservesAcceptedEnabledAndManualStrategy(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*InstallPlan)
		want   string
	}{
		{
			name: "enabled",
			mutate: func(plan *InstallPlan) {
				plan.Spec.Enabled = false
			},
			want: "accepted enabled=false, want true",
		},
		{
			name: "strategy",
			mutate: func(plan *InstallPlan) {
				plan.Spec.UpgradeStrategy = "Automatic"
			},
			want: `accepted upgradeStrategy "Automatic", want "Manual"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			current := planForTest("demo", "1.2.0", "Installed")
			client.planObjects["demo"] = objectForTest(t, current)
			prepareExtensionForLifecycle(
				t,
				client,
				"demo",
				lifecycleVersion("demo", "1.2.1", "HostOnly"),
			)
			accepted := current
			accepted.Spec.Extension.Version = "1.2.1"
			test.mutate(&accepted)
			client.patchResponses["demo"] = objectForTest(t, accepted)

			_, err := NewService(client).Upgrade(
				context.Background(),
				"demo",
				UpgradeOptions{Version: "1.2.1"},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Upgrade() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestServiceUpgradeRejectsSameBusyOrDeleting(t *testing.T) {
	for _, test := range []struct {
		name     string
		state    string
		current  string
		target   string
		deleting bool
		changes  PlanChanges
		want     string
	}{
		{
			name:    "same version",
			state:   "Installed",
			current: "1.2.1",
			target:  "1.2.1",
			want:    "extension configure",
		},
		{
			name:    "preparing",
			state:   "Preparing",
			current: "1.2.0",
			target:  "1.2.1",
			want:    "currently Preparing",
		},
		{
			name:    "installing",
			state:   "Installing",
			current: "1.2.0",
			target:  "1.2.1",
			want:    "currently Installing",
		},
		{
			name:    "upgrading",
			state:   "Upgrading",
			current: "1.2.0",
			target:  "1.2.1",
			want:    "currently Upgrading",
		},
		{
			name:    "uninstalling",
			state:   "Uninstalling",
			current: "1.2.0",
			target:  "1.2.1",
			want:    "currently Uninstalling",
		},
		{
			name:    "uninstalled",
			state:   "Uninstalled",
			current: "1.2.0",
			target:  "1.2.1",
			want:    "remove the stale plan",
		},
		{
			name:     "deleting",
			state:    "Installed",
			current:  "1.2.0",
			target:   "1.2.1",
			deleting: true,
			want:     "being deleted",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeAPIClient(t)
			current := planForTest("demo", test.current, test.state)
			current.Spec.Config = "key: old\n"
			if test.deleting {
				now := metav1.Now()
				current.Metadata.DeletionTimestamp = &now
			}
			client.planObjects["demo"] = objectForTest(t, current)
			prepareExtensionForLifecycle(
				t,
				client,
				"demo",
				lifecycleVersion("demo", test.target, "HostOnly"),
			)

			_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
				Version: test.target,
				Changes: test.changes,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Upgrade() error = %v, want %q", err, test.want)
			}
			if len(client.patches) != 0 {
				t.Fatalf("patches = %q", client.patches)
			}
		})
	}
}

func TestServiceUpgradeAllowsFailedPlanWithChangedVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "UpgradeFailed")
	current.Spec.Config = "key: old\n"
	current.Status.ConfigHash = controllerConfigHash([]byte(current.Spec.Config))
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "HostOnly"),
	)
	accepted := current
	accepted.Spec.Extension.Version = "1.2.1"
	client.patchResponses["demo"] = objectForTest(t, accepted)

	_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
	})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(client.patches) != 1 {
		t.Fatalf("patches = %q", client.patches)
	}
}

func TestServiceUpgradeRejectsFailedPlanWhenControllerInputsMatch(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "UpgradeFailed")
	current.Spec.Config = "key: old\n"
	current.Status.Version = "1.2.1"
	current.Status.ConfigHash = controllerConfigHash([]byte(current.Spec.Config))
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "HostOnly"),
	)

	_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
	})
	if err == nil ||
		!strings.Contains(err.Error(), "target version or corrected global configuration") {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(client.patches) != 0 {
		t.Fatalf("patches = %q", client.patches)
	}
}

func TestServiceRejectsWaitForSelectorOnlyPlacementBeforePatch(t *testing.T) {
	selectorScheduling := &ClusterScheduling{
		Placement: &Placement{
			ClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"environment": "production"},
			},
		},
	}

	t.Run("upgrade", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.ClusterScheduling = selectorScheduling
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)

		_, err := NewService(client).Upgrade(
			context.Background(),
			"demo",
			UpgradeOptions{
				Version:         "1.2.1",
				RequireWaitable: true,
			},
		)
		if err == nil || !strings.Contains(err.Error(), "--wait cannot track") {
			t.Fatalf("Upgrade() error = %v", err)
		}
		if len(client.patches) != 0 {
			t.Fatalf("patches = %q", client.patches)
		}
	})

	t.Run("configure", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.Config = "key: old\n"
		current.Spec.ClusterScheduling = selectorScheduling
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.0", "Multicluster"),
		)

		_, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Config:          StringChange{Mode: Replace, Value: "key: new\n"},
				RequireWaitable: true,
			},
		)
		if err == nil || !strings.Contains(err.Error(), "--wait cannot track") {
			t.Fatalf("Configure() error = %v", err)
		}
		if len(client.patches) != 0 {
			t.Fatalf("patches = %q", client.patches)
		}
	})
}

func TestServiceRejectsClusterTargetDuringUninstallLifecycle(t *testing.T) {
	for _, operation := range []string{"upgrade", "configure"} {
		t.Run(operation, func(t *testing.T) {
			client := newFakeAPIClient(t)
			current := planForTest("demo", "1.2.0", "Installed")
			current.Spec.ClusterScheduling = &ClusterScheduling{
				Placement: &Placement{Clusters: []string{"member-a"}},
			}
			current.Status.ClusterSchedulingStatuses =
				map[string]InstallationStatus{
					"member-b": {
						State:   "Uninstalling",
						Version: "1.2.0",
					},
				}
			client.planObjects["demo"] = objectForTest(t, current)
			targetVersion := "1.2.0"
			if operation == "upgrade" {
				targetVersion = "1.2.1"
			}
			prepareExtensionForLifecycle(
				t,
				client,
				"demo",
				lifecycleVersion(
					"demo",
					targetVersion,
					"Multicluster",
				),
			)

			var err error
			switch operation {
			case "upgrade":
				_, err = NewService(client).Upgrade(
					context.Background(),
					"demo",
					UpgradeOptions{
						Version: targetVersion,
						Changes: PlanChanges{
							Scheduling: SchedulingChange{
								Mode: Replace,
								Clusters: []string{
									"member-a",
									"member-b",
								},
							},
						},
					},
				)
			case "configure":
				_, err = NewService(client).Configure(
					context.Background(),
					"demo",
					PlanChanges{
						Scheduling: SchedulingChange{
							Mode: Replace,
							Clusters: []string{
								"member-a",
								"member-b",
							},
						},
					},
				)
			}
			if err == nil ||
				!strings.Contains(err.Error(), `cluster "member-b"`) ||
				!strings.Contains(err.Error(), "Uninstalling") {
				t.Fatalf(
					"%s error = %v, want uninstall lifecycle rejection",
					operation,
					err,
				)
			}
			if len(client.patches) != 0 {
				t.Fatalf("patches = %q", client.patches)
			}
		})
	}
}

func TestServiceUpgradeAllowsFailedPlanWithChangedGlobalConfig(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "UpgradeFailed")
	current.Spec.Config = "key: old\n"
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "HostOnly"),
	)
	accepted := current
	accepted.Spec.Extension.Version = "1.2.1"
	accepted.Spec.Config = "key: fixed\n"
	client.patchResponses["demo"] = objectForTest(t, accepted)

	_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
		Changes: PlanChanges{Config: StringChange{
			Mode:  Replace,
			Value: "key: fixed\n",
		}},
	})
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(client.patches) != 1 ||
		!strings.Contains(string(client.patches[0]), `"key: fixed\n"`) {
		t.Fatalf("patches = %q", client.patches)
	}
}

func TestServiceConfigureRequiresSemanticChangeAndOmitsIdentity(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Spec.Config = "key: old\n"
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.0", "HostOnly"),
	)

	if _, err := NewService(client).Configure(
		context.Background(),
		"demo",
		PlanChanges{},
	); err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("Configure(no-op) error = %v", err)
	}

	accepted := current
	accepted.Spec.Config = "key: new\n"
	client.patchResponses["demo"] = objectForTest(t, accepted)
	_, err := NewService(client).Configure(context.Background(), "demo", PlanChanges{
		Config: StringChange{Mode: Replace, Value: "key: new\n"},
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	var patch struct {
		Spec map[string]json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(client.patches[0], &patch); err != nil {
		t.Fatal(err)
	}
	if _, found := patch.Spec["extension"]; found {
		t.Fatalf("configure patch contains extension: %s", client.patches[0])
	}
	if _, found := patch.Spec["enabled"]; found {
		t.Fatalf("configure patch contains enabled: %s", client.patches[0])
	}
}

func TestServiceConfigureRejectsDroppedAcceptedChange(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Spec.Config = "key: old\n"
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.0", "HostOnly"),
	)
	client.patchResponses["demo"] = objectForTest(t, current)

	_, err := NewService(client).Configure(
		context.Background(),
		"demo",
		PlanChanges{
			Config: StringChange{Mode: Replace, Value: "key: new\n"},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "accepted config differs") {
		t.Fatalf("Configure() error = %v, want dropped change rejection", err)
	}
}

func TestServiceConfigureFailedPlanRequiresChangedGlobalConfig(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "InstallFailed")
	current.Spec.Config = "key: old\n"
	current.Status.ConfigHash = controllerConfigHash([]byte(current.Spec.Config))
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.0", "Multicluster"),
	)

	_, err := NewService(client).Configure(context.Background(), "demo", PlanChanges{
		Scheduling: SchedulingChange{
			Mode:     Replace,
			Clusters: []string{"member-a"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "corrected global configuration") {
		t.Fatalf("Configure(scheduling only) error = %v", err)
	}
	if len(client.patches) != 0 {
		t.Fatalf("patches = %q", client.patches)
	}

	accepted := current
	accepted.Spec.Config = ""
	client.patchResponses["demo"] = objectForTest(t, accepted)
	_, err = NewService(client).Configure(context.Background(), "demo", PlanChanges{
		Config: StringChange{Mode: Clear},
	})
	if err != nil {
		t.Fatalf("Configure(clear config) error = %v", err)
	}
	if len(client.patches) != 1 {
		t.Fatalf("patches = %q", client.patches)
	}
}

func TestServiceConfigureRejectsUnexpectedAcceptedPlan(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Spec.Config = "key: old\n"
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.0", "HostOnly"),
	)
	accepted := current
	accepted.Spec.Extension.Name = "other"
	client.patchResponses["demo"] = objectForTest(t, accepted)

	_, err := NewService(client).Configure(context.Background(), "demo", PlanChanges{
		Config: StringChange{Mode: Replace, Value: "key: new\n"},
	})
	if err == nil || !strings.Contains(err.Error(), `references extension "other"`) {
		t.Fatalf("Configure() error = %v", err)
	}
}

func TestServiceUpgradeReturnsConflictWithoutRetry(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "HostOnly"),
	)
	conflict := apierrors.NewConflict(
		schema.GroupResource{Group: "kubesphere.io", Resource: "installplans"},
		"demo",
		errors.New("changed"),
	)
	client.patchErr = conflict

	_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
	})
	if !apierrors.IsConflict(err) {
		t.Fatalf("Upgrade() error = %v, want Conflict", err)
	}
	if len(client.patches) != 1 {
		t.Fatalf("patch attempts = %d, want 1", len(client.patches))
	}
}

func TestServiceUpgradeRejectsMissingResourceVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Metadata.ResourceVersion = ""
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "HostOnly"),
	)

	_, err := NewService(client).Upgrade(context.Background(), "demo", UpgradeOptions{
		Version: "1.2.1",
	})
	if err == nil || !strings.Contains(err.Error(), "resourceVersion") {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if len(client.patches) != 0 {
		t.Fatalf("patches = %q", client.patches)
	}
}

func TestServiceUninstallGetsThenDeletesWithoutWaiting(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installing")
	current.Metadata.Finalizers = []string{installPlanProtectionFinalizer}
	client.planObjects["demo"] = objectForTest(t, current)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.0", "HostOnly"),
	)

	operation, err := NewService(client).Uninstall(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !reflect.DeepEqual(
		client.calls,
		[]string{
			"get install plan demo",
			"get extension version demo-1.2.0",
			"delete install plan demo",
		},
	) {
		t.Fatalf("calls = %v", client.calls)
	}
	if operation.Kind != OperationUninstall || operation.TargetVersion != "1.2.0" {
		t.Fatalf("operation = %#v", operation)
	}
	if !reflect.DeepEqual(client.deleteVersions, []string{"1"}) {
		t.Fatalf("delete resourceVersions = %v", client.deleteVersions)
	}
}

func TestServiceUninstallRejectsMissingControllerVersionBeforeDelete(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Metadata.Finalizers = []string{installPlanProtectionFinalizer}
	client.planObjects["demo"] = objectForTest(t, current)

	_, err := NewService(client).Uninstall(context.Background(), "demo")
	if err == nil || !strings.Contains(err.Error(), "cannot safely uninstall") {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if len(client.deletedPlans) != 0 {
		t.Fatalf("deleted plans = %v", client.deletedPlans)
	}
}

func TestServiceUninstallWithoutProtectionDoesNotRequireVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "")
	client.planObjects["demo"] = objectForTest(t, current)

	_, err := NewService(client).Uninstall(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if len(client.deletedPlans) != 1 {
		t.Fatalf("deleted plans = %v", client.deletedPlans)
	}
	for _, call := range client.calls {
		if strings.HasPrefix(call, "get extension version ") {
			t.Fatalf("unexpected version preflight: %v", client.calls)
		}
	}
}

func TestServiceUninstallRejectsMissingResourceVersionBeforeDelete(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "Installed")
	current.Metadata.ResourceVersion = ""
	client.planObjects["demo"] = objectForTest(t, current)

	_, err := NewService(client).Uninstall(context.Background(), "demo")
	if err == nil || !strings.Contains(err.Error(), "resourceVersion") {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if len(client.deletedPlans) != 0 {
		t.Fatalf("deleted plans = %v", client.deletedPlans)
	}
}

func TestServiceUninstallMissingReturnsNotFound(t *testing.T) {
	client := newFakeAPIClient(t)
	_, err := NewService(client).Uninstall(context.Background(), "missing")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("Uninstall() error = %v, want NotFound", err)
	}
	if len(client.deletedPlans) != 0 {
		t.Fatalf("deleted plans = %v", client.deletedPlans)
	}
}

func TestLifecycleOperationsBuildTargetLocalWaitExpectations(t *testing.T) {
	t.Run("install host and explicit clusters", func(t *testing.T) {
		client := newFakeAPIClient(t)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		config := "global: value\n"
		override := "member: value\n"

		operation, err := NewService(client).Install(
			context.Background(),
			"demo",
			InstallOptions{
				Version:   "1.2.1",
				Config:    &config,
				Clusters:  []string{"member-a"},
				Overrides: map[string]string{"member-a": override},
			},
		)
		if err != nil {
			t.Fatalf("Install() error = %v", err)
		}
		if operation.expectation.Host == nil ||
			operation.expectation.Host.Version != "1.2.1" ||
			operation.expectation.Host.ConfigHash !=
				controllerConfigHash([]byte(config)) ||
			!operation.expectation.Host.MustAdvance {
			t.Fatalf("host expectation = %#v", operation.expectation.Host)
		}
		target, found := operation.expectation.Clusters["member-a"]
		if !found ||
			target.Version != "1.2.1" ||
			target.ConfigHash != controllerConfigHash(
				mergeControllerConfig(config, override),
			) ||
			!target.MustAdvance {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
	})

	t.Run("upgrade resulting clusters", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.Config = "global: value\n"
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
			Overrides: map[string]string{"member-a": "member: value\n"},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {
				State:   "Installed",
				Version: "1.2.0",
			},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.Extension.Version = "1.2.1"
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Upgrade(
			context.Background(),
			"demo",
			UpgradeOptions{Version: "1.2.1"},
		)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		if operation.expectation.Host == nil ||
			operation.expectation.Host.Version != "1.2.1" ||
			operation.expectation.Host.ConfigHash != controllerConfigHash(
				[]byte(current.Spec.Config),
			) {
			t.Fatalf("host expectation = %#v", operation.expectation.Host)
		}
		if target, found := operation.expectation.Clusters["member-a"]; !found ||
			target.Version != "1.2.1" ||
			target.ConfigHash != controllerConfigHash(
				mergeControllerConfig(
					current.Spec.Config,
					current.Spec.ClusterScheduling.Overrides["member-a"],
				),
			) {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
	})

	t.Run("upgrade selector is marked unsafe to wait", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{
				ClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"environment": "production"},
				},
			},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {State: "Installed", Version: "1.2.0"},
			"member-b": {State: "Installed", Version: "1.2.0"},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.Extension.Version = "1.2.1"
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Upgrade(
			context.Background(),
			"demo",
			UpgradeOptions{Version: "1.2.1"},
		)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		if !operation.expectation.SelectorOnly {
			t.Fatalf("expectation = %#v", operation.expectation)
		}
		if len(operation.expectation.Clusters) != 0 {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
	})

	t.Run("upgrade explicit clusters take precedence over stale selector", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{
				Clusters: []string{"member-a"},
				ClusterSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"environment": "production"},
				},
			},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {State: "Installed", Version: "1.2.0"},
			"member-b": {State: "Installed", Version: "1.2.0"},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.Extension.Version = "1.2.1"
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Upgrade(
			context.Background(),
			"demo",
			UpgradeOptions{Version: "1.2.1"},
		)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		if _, found := operation.expectation.Clusters["member-a"]; !found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		if _, found := operation.expectation.Clusters["member-b"]; found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		if _, found := operation.expectation.RemovedClusters["member-b"]; !found {
			t.Fatalf("removed clusters = %#v", operation.expectation.RemovedClusters)
		}
	})

	t.Run("upgrade replacement tracks removed clusters", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a", "member-b"}},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {State: "Installed", Version: "1.2.0"},
			"member-b": {State: "Installed", Version: "1.2.0"},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.Extension.Version = "1.2.1"
		accepted.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
		}
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Upgrade(
			context.Background(),
			"demo",
			UpgradeOptions{
				Version: "1.2.1",
				Changes: PlanChanges{
					Scheduling: SchedulingChange{
						Mode:     Replace,
						Clusters: []string{"member-a"},
					},
				},
			},
		)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		if _, found := operation.expectation.Clusters["member-a"]; !found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		if _, found := operation.expectation.Clusters["member-b"]; found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		if _, found := operation.expectation.RemovedClusters["member-b"]; !found {
			t.Fatalf("removed clusters = %#v", operation.expectation.RemovedClusters)
		}
	})

	t.Run("upgrade clear tracks every old cluster for removal", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.0", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a", "member-b"}},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {State: "Installed", Version: "1.2.0"},
			"member-b": {State: "Installed", Version: "1.2.0"},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "HostOnly"),
		)
		accepted := current
		accepted.Spec.Extension.Version = "1.2.1"
		accepted.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{}},
		}
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Upgrade(
			context.Background(),
			"demo",
			UpgradeOptions{
				Version: "1.2.1",
				Changes: PlanChanges{
					Scheduling: SchedulingChange{Mode: Clear},
				},
			},
		)
		if err != nil {
			t.Fatalf("Upgrade() error = %v", err)
		}
		if len(operation.expectation.Clusters) != 0 {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		for _, cluster := range []string{"member-a", "member-b"} {
			if _, found := operation.expectation.RemovedClusters[cluster]; !found {
				t.Fatalf("removed clusters = %#v", operation.expectation.RemovedClusters)
			}
		}
	})

	t.Run("configure global config targets host and known clusters", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.1", "Installed")
		current.Spec.Config = "key: old\n"
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
		}
		current.Status.ConfigHash = "host-old"
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {
				State:      "Installed",
				Version:    "1.2.1",
				ConfigHash: "member-old",
			},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.Config = "key: new\n"
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Config: StringChange{Mode: Replace, Value: "key: new\n"},
			},
		)
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		if operation.expectation.Host == nil ||
			operation.expectation.Host.ConfigHash != configHashForTest("key: new\n") {
			t.Fatalf("host expectation = %#v", operation.expectation.Host)
		}
		target, found := operation.expectation.Clusters["member-a"]
		if !found || target.ConfigHash != configHashForTest("key: new\n") {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
	})

	t.Run("configure shadowed global config skips unchanged member", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.1", "Installed")
		current.Spec.Config = "key: old\n"
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
			Overrides: map[string]string{"member-a": "key: fixed\n"},
		}
		current.Status.ConfigHash = configHashForTest(current.Spec.Config)
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {
				State:      "Installed",
				Version:    "1.2.1",
				ConfigHash: configHashForTest("key: fixed\n"),
			},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.Config = "key: new\n"
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Config: StringChange{Mode: Replace, Value: "key: new\n"},
			},
		)
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		if operation.expectation.Host == nil ||
			operation.expectation.Host.ConfigHash != configHashForTest("key: new\n") {
			t.Fatalf("host expectation = %#v", operation.expectation.Host)
		}
		if _, found := operation.expectation.Clusters["member-a"]; found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
	})

	t.Run("configure override waits for exact effective hash", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.1", "Installed")
		current.Spec.Config = "key: global\n"
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
			Overrides: map[string]string{"member-a": "key: override\n"},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {
				State:      "Installed",
				Version:    "1.2.1",
				ConfigHash: configHashForTest("key: override\n"),
			},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
		}
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Scheduling: SchedulingChange{
					RemoveOverrides: []string{"member-a"},
				},
			},
		)
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		target, found := operation.expectation.Clusters["member-a"]
		if !found || target.ConfigHash != configHashForTest("key: global\n") {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
	})

	t.Run("configure placement targets only added then removes stale", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.1", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a", "member-b"}},
			Overrides: map[string]string{
				"member-b": "key: old\n",
			},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {
				State:   "Installed",
				Version: "1.2.1",
			},
			"member-b": {
				State:   "Installed",
				Version: "1.2.1",
			},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{
				Clusters: []string{"member-a", "member-c"},
			},
		}
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Scheduling: SchedulingChange{
					Mode:     Replace,
					Clusters: []string{"member-a", "member-c"},
				},
			},
		)
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		if operation.expectation.Host != nil {
			t.Fatalf("host expectation = %#v", operation.expectation.Host)
		}
		if _, found := operation.expectation.Clusters["member-a"]; found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		if _, found := operation.expectation.Clusters["member-c"]; !found {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		if _, found := operation.expectation.RemovedClusters["member-b"]; !found {
			t.Fatalf("removed clusters = %#v", operation.expectation.RemovedClusters)
		}
	})

	t.Run("configure clear scheduling waits for all old statuses to disappear", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.1", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a", "member-b"}},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {State: "Installed", Version: "1.2.1"},
			"member-b": {State: "Installed", Version: "1.2.1"},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{}},
		}
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Scheduling: SchedulingChange{Mode: Clear},
			},
		)
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		if len(operation.expectation.Clusters) != 0 {
			t.Fatalf("cluster expectations = %#v", operation.expectation.Clusters)
		}
		for _, cluster := range []string{"member-a", "member-b"} {
			if _, found := operation.expectation.RemovedClusters[cluster]; !found {
				t.Fatalf("removed clusters = %#v", operation.expectation.RemovedClusters)
			}
		}
	})

	t.Run("configure removal reports accepted uninstall failure", func(t *testing.T) {
		client := newFakeAPIClient(t)
		current := planForTest("demo", "1.2.1", "Installed")
		current.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{"member-a"}},
		}
		current.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": {State: "Installed", Version: "1.2.1"},
		}
		client.planObjects["demo"] = objectForTest(t, current)
		prepareExtensionForLifecycle(
			t,
			client,
			"demo",
			lifecycleVersion("demo", "1.2.1", "Multicluster"),
		)
		accepted := current
		accepted.Spec.ClusterScheduling = &ClusterScheduling{
			Placement: &Placement{Clusters: []string{}},
		}
		acceptedFailure := waitStatus("UninstallFailed", "1.2.1", "old")
		acceptedFailure.JobName = "accepted"
		accepted.Status.ClusterSchedulingStatuses = map[string]InstallationStatus{
			"member-a": acceptedFailure,
		}
		client.patchResponses["demo"] = objectForTest(t, accepted)

		operation, err := NewService(client).Configure(
			context.Background(),
			"demo",
			PlanChanges{
				Scheduling: SchedulingChange{Mode: Clear},
			},
		)
		if err != nil {
			t.Fatalf("Configure() error = %v", err)
		}
		target, found := operation.expectation.RemovedClusters["member-a"]
		if !found || target.Baseline != statusFingerprint(acceptedFailure) {
			t.Fatalf("removed clusters = %#v", operation.expectation.RemovedClusters)
		}

		advanced := map[string]bool{}
		done, err := evaluateOperation(operation, accepted, advanced)
		var lifecycleErr *LifecycleFailureError
		if done || !errors.As(err, &lifecycleErr) {
			t.Fatalf("accepted evaluateOperation() = (%t, %v)", done, err)
		}
	})
}
