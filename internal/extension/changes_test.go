package extension

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNormalizeYAMLValidatesSingleNonEmptyDocument(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		want    string
		wantErr string
	}{
		{name: "valid", value: "key: value\r\n\r\n", want: "key: value\n"},
		{name: "empty", value: " \r\n", wantErr: "non-empty"},
		{name: "malformed", value: "key: [", wantErr: "invalid"},
		{name: "multiple", value: "key: one\n---\nkey: two\n", wantErr: "one YAML document"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeYAML("global configuration", test.value)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("NormalizeYAML() error = %v, want %q", err, test.wantErr)
				}
				if strings.Contains(err.Error(), test.value) {
					t.Fatalf("NormalizeYAML() leaked input in error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeYAML() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("NormalizeYAML() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeClustersDeduplicatesWithoutReordering(t *testing.T) {
	got, err := NormalizeClusters([]string{"member-b", "member-a", "member-b"})
	if err != nil {
		t.Fatalf("NormalizeClusters() error = %v", err)
	}
	if want := []string{"member-b", "member-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeClusters() = %v, want %v", got, want)
	}

	if _, err := NormalizeClusters([]string{"team/member"}); err == nil {
		t.Fatal("NormalizeClusters() error = nil, want invalid path segment")
	}
}

func TestBuildInstallSchedulingValidatesModeAndOverrides(t *testing.T) {
	scheduling, err := BuildInstallScheduling(
		"Multicluster",
		[]string{"member-a"},
		map[string]string{"member-a": "key: value\r\n"},
	)
	if err != nil {
		t.Fatalf("BuildInstallScheduling() error = %v", err)
	}
	if scheduling == nil || scheduling.Placement == nil ||
		!reflect.DeepEqual(scheduling.Placement.Clusters, []string{"member-a"}) ||
		scheduling.Overrides["member-a"] != "key: value\n" {
		t.Fatalf("scheduling = %#v", scheduling)
	}

	if _, err := BuildInstallScheduling(
		"HostOnly",
		[]string{"member-a"},
		nil,
	); err == nil {
		t.Fatal("HostOnly scheduling error = nil")
	}
	if _, err := BuildInstallScheduling(
		"Multicluster",
		[]string{"member-a"},
		map[string]string{"member-b": "key: value"},
	); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("unplaced override error = %v", err)
	}
}

func TestBuildSpecPatchPreservesOmittedFields(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.Config = "key: value\n"
	current.Spec.ClusterScheduling = &ClusterScheduling{
		Placement: &Placement{Clusters: []string{"member-a"}},
		Overrides: map[string]string{"member-a": "key: override\n"},
	}

	patch, summary, err := BuildSpecPatch(current, "Multicluster", PlanChanges{})
	if err != nil {
		t.Fatalf("BuildSpecPatch() error = %v", err)
	}
	if summary.Changed() {
		t.Fatalf("summary = %#v, want unchanged", summary)
	}
	if want := map[string]any{"upgradeStrategy": "Manual"}; !reflect.DeepEqual(patch, want) {
		t.Fatalf("patch = %#v, want %#v", patch, want)
	}
}

func TestBuildSpecPatchReplacesAndClearsConfig(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.Config = "old: value\n"

	replaced, summary, err := BuildSpecPatch(current, "HostOnly", PlanChanges{
		Config: StringChange{Mode: Replace, Value: "new: value\r\n"},
	})
	if err != nil {
		t.Fatalf("replace BuildSpecPatch() error = %v", err)
	}
	if replaced["config"] != "new: value\n" || !summary.ConfigChanged {
		t.Fatalf("replace patch = %#v, summary = %#v", replaced, summary)
	}

	cleared, summary, err := BuildSpecPatch(current, "HostOnly", PlanChanges{
		Config: StringChange{Mode: Clear},
	})
	if err != nil {
		t.Fatalf("clear BuildSpecPatch() error = %v", err)
	}
	value, found := cleared["config"]
	if !found || value != nil || !summary.ConfigChanged {
		t.Fatalf("clear patch = %#v, summary = %#v", cleared, summary)
	}
}

func TestBuildSpecPatchReplacesPlacementAndNullsStaleOverrides(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.ClusterScheduling = &ClusterScheduling{
		Placement: &Placement{
			ClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tier": "worker"},
			},
		},
		Overrides: map[string]string{
			"member-a": "a: old\n",
			"member-b": "b: old\n",
		},
	}

	patch, summary, err := BuildSpecPatch(current, "Multicluster", PlanChanges{
		Scheduling: SchedulingChange{
			Mode:         Replace,
			Clusters:     []string{"member-a"},
			SetOverrides: map[string]string{"member-a": "a: new\n"},
		},
	})
	if err != nil {
		t.Fatalf("BuildSpecPatch() error = %v", err)
	}
	scheduling := patch["clusterScheduling"].(map[string]any)
	placement := scheduling["placement"].(map[string]any)
	if placement["clusterSelector"] != nil ||
		!reflect.DeepEqual(placement["clusters"], []string{"member-a"}) {
		t.Fatalf("placement patch = %#v", placement)
	}
	overrides := scheduling["overrides"].(map[string]any)
	if overrides["member-a"] != "a: new\n" {
		t.Fatalf("member-a override = %#v", overrides["member-a"])
	}
	if value, found := overrides["member-b"]; !found || value != nil {
		t.Fatalf("stale override patch = %#v", overrides)
	}
	if !summary.SchedulingChanged || summary.ResultScheduling == nil ||
		summary.ResultScheduling.Placement.ClusterSelector != nil {
		t.Fatalf("summary = %#v", summary)
	}

	encoded, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"member-b":null`) {
		t.Fatalf("encoded patch = %s", encoded)
	}
}

func TestBuildSpecPatchSetsAndRemovesIndividualOverrides(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.ClusterScheduling = &ClusterScheduling{
		Placement: &Placement{Clusters: []string{"member-a", "member-b"}},
		Overrides: map[string]string{"member-b": "old: value\n"},
	}

	patch, summary, err := BuildSpecPatch(current, "Multicluster", PlanChanges{
		Scheduling: SchedulingChange{
			SetOverrides:    map[string]string{"member-a": "new: value\n"},
			RemoveOverrides: []string{"member-b"},
		},
	})
	if err != nil {
		t.Fatalf("BuildSpecPatch() error = %v", err)
	}
	overrides := patch["clusterScheduling"].(map[string]any)["overrides"].(map[string]any)
	if overrides["member-a"] != "new: value\n" || overrides["member-b"] != nil {
		t.Fatalf("overrides patch = %#v", overrides)
	}
	if !summary.SchedulingChanged {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestBuildSpecPatchRequiresExplicitClustersBeforeSettingSelectorOverride(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.ClusterScheduling = &ClusterScheduling{
		Placement: &Placement{
			ClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tier": "worker"},
			},
		},
		Overrides: map[string]string{"member-a": "old: value\n"},
	}

	_, _, err := BuildSpecPatch(current, "Multicluster", PlanChanges{
		Scheduling: SchedulingChange{
			SetOverrides: map[string]string{"member-a": "new: value\n"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "--clusters") {
		t.Fatalf("BuildSpecPatch() error = %v, want --clusters", err)
	}

	patch, _, err := BuildSpecPatch(current, "Multicluster", PlanChanges{
		Scheduling: SchedulingChange{RemoveOverrides: []string{"member-a"}},
	})
	if err != nil {
		t.Fatalf("remove selector override error = %v", err)
	}
	if patch["clusterScheduling"].(map[string]any)["overrides"].(map[string]any)["member-a"] != nil {
		t.Fatalf("remove patch = %#v", patch)
	}
}

func TestBuildSpecPatchClearsSchedulingAndRejectsHostOnly(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.ClusterScheduling = &ClusterScheduling{
		Placement: &Placement{Clusters: []string{"member-a"}},
	}

	if _, _, err := BuildSpecPatch(current, "HostOnly", PlanChanges{}); err == nil {
		t.Fatal("HostOnly current scheduling error = nil")
	}
	patch, summary, err := BuildSpecPatch(current, "HostOnly", PlanChanges{
		Scheduling: SchedulingChange{Mode: Clear},
	})
	if err != nil {
		t.Fatalf("clear HostOnly scheduling error = %v", err)
	}
	if value, found := patch["clusterScheduling"]; !found || value != nil ||
		!summary.SchedulingChanged || summary.ResultScheduling != nil {
		t.Fatalf("patch = %#v, summary = %#v", patch, summary)
	}
}

func TestBuildSpecPatchRejectsSetAndRemoveSameOverride(t *testing.T) {
	current := planForTest("demo", "1.0.0", "Installed")
	current.Spec.ClusterScheduling = &ClusterScheduling{
		Placement: &Placement{Clusters: []string{"member-a"}},
	}

	_, _, err := BuildSpecPatch(current, "Multicluster", PlanChanges{
		Scheduling: SchedulingChange{
			SetOverrides:    map[string]string{"member-a": "key: value\n"},
			RemoveOverrides: []string{"member-a"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("BuildSpecPatch() error = %v", err)
	}
}
