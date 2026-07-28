package extension

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

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

func TestServiceInstallCreatesExactEnabledManualPlan(t *testing.T) {
	client := newFakeAPIClient(t)
	prepareExtensionForLifecycle(
		t,
		client,
		"demo",
		lifecycleVersion("demo", "1.2.1", "Multicluster"),
	)
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

func TestServiceUpgradeRejectsSameBusyDeletingAndFailedWithoutConfigChange(t *testing.T) {
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
			name:     "deleting",
			state:    "Installed",
			current:  "1.2.0",
			target:   "1.2.1",
			deleting: true,
			want:     "being deleted",
		},
		{
			name:    "failed version only",
			state:   "UpgradeFailed",
			current: "1.2.0",
			target:  "1.2.1",
			want:    "corrected global configuration",
		},
		{
			name:    "failed same config",
			state:   "InstallFailed",
			current: "1.2.0",
			target:  "1.2.1",
			changes: PlanChanges{Config: StringChange{
				Mode:  Replace,
				Value: "key: old\n",
			}},
			want: "corrected global configuration",
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
	client.patchResponses["demo"] = objectForTest(t, current)

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

	client.patchResponses["demo"] = objectForTest(t, current)
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

func TestServiceConfigureFailedPlanRequiresChangedGlobalConfig(t *testing.T) {
	client := newFakeAPIClient(t)
	current := planForTest("demo", "1.2.0", "InstallFailed")
	current.Spec.Config = "key: old\n"
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

	client.patchResponses["demo"] = objectForTest(t, current)
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
	client.planObjects["demo"] = objectForTest(t, current)

	operation, err := NewService(client).Uninstall(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !reflect.DeepEqual(
		client.calls,
		[]string{"get install plan demo", "delete install plan demo"},
	) {
		t.Fatalf("calls = %v", client.calls)
	}
	if operation.Kind != OperationUninstall || operation.TargetVersion != "1.2.0" {
		t.Fatalf("operation = %#v", operation)
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
