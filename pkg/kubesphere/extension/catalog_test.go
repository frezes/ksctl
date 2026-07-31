package extension

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestServiceListJoinsPlansFiltersAndSorts(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensions = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionList",
		extensionForTest("zeta"),
		extensionForTest("failed"),
		extensionForTest("deleting"),
		extensionForTest("removed"),
		extensionForTest("alpha"),
	)
	failed := planForTest("failed", "1.0.0", "InstallFailed")
	deleting := planForTest("deleting", "1.0.0", "Installed")
	now := metav1.Now()
	deleting.Metadata.DeletionTimestamp = &now
	removed := planForTest("removed", "1.0.0", "Uninstalled")
	alpha := planForTest("alpha", "1.0.0", "Installed")
	client.installPlans = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"InstallPlanList",
		failed,
		deleting,
		removed,
		alpha,
	)

	result, err := NewService(client).List(context.Background(), ListOptions{
		Category:      "observability",
		InstalledOnly: true,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var names []string
	for _, item := range result.Items {
		names = append(names, item.Extension.Value.Metadata.Name)
		if item.InstallPlan == nil {
			t.Fatalf("item %q has nil InstallPlan", item.Extension.Value.Metadata.Name)
		}
	}
	if want := []string{"alpha", "failed"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if client.calls[0] != "list extensions category=observability" {
		t.Fatalf("calls = %v", client.calls)
	}
}

func TestServiceListFilteredRawJSONRetainsUnknownFields(t *testing.T) {
	client := newFakeAPIClient(t)
	raw := []byte(`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionList","futureList":true,"items":[{"metadata":{"name":"keep"},"futureItem":"kept"},{"metadata":{"name":"drop"},"futureItem":"dropped"}]}`)
	list, err := decodeList[Extension](raw)
	if err != nil {
		t.Fatal(err)
	}
	client.extensions = list
	client.installPlans = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"InstallPlanList",
		planForTest("keep", "1.0.0", "Installed"),
	)

	result, err := NewService(client).List(context.Background(), ListOptions{InstalledOnly: true})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var document struct {
		FutureList bool `json:"futureList"`
		Items      []struct {
			Metadata   ObjectMeta `json:"metadata"`
			FutureItem string     `json:"futureItem"`
		} `json:"items"`
	}
	if err := json.Unmarshal(result.RawJSON(), &document); err != nil {
		t.Fatal(err)
	}
	if !document.FutureList || len(document.Items) != 1 ||
		document.Items[0].Metadata.Name != "keep" ||
		document.Items[0].FutureItem != "kept" {
		t.Fatalf("document = %#v", document)
	}
}

func TestServiceListRejectsMismatchedInstallPlanIdentity(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensions = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionList",
		extensionForTest("demo"),
	)
	plan := planForTest("demo", "1.0.0", "Installed")
	plan.Spec.Extension.Name = "other"
	client.installPlans = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"InstallPlanList",
		plan,
	)

	_, err := NewService(client).List(context.Background(), ListOptions{})
	if err == nil ||
		!strings.Contains(err.Error(), `references extension "other"`) {
		t.Fatalf("List() error = %v, want identity rejection", err)
	}
}

func TestServiceShowJoinsVersionsAndInstallPlan(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensionObjects["demo"] = objectForTest(t, extensionForTest("demo"))
	client.versions["demo"] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		versionForTest("demo", "demo-1-0-0", "1.0.0"),
		versionForTest("demo", "demo-1-2-0", "1.2.0"),
	)
	plan := planForTest("demo", "1.0.0", "Installed")
	client.planObjects["demo"] = objectForTest(t, plan)

	result, err := NewService(client).Show(context.Background(), "demo", "")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if result.InstallPlan == nil || len(result.Versions.Items) != 2 || result.SelectedVersion != nil {
		t.Fatalf("Show() result = %#v", result)
	}
	if !strings.Contains(string(result.RawJSON()), `"name":"demo"`) {
		t.Fatalf("RawJSON() = %s", result.RawJSON())
	}
}

func TestServiceShowRejectsMismatchedInstallPlanIdentity(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensionObjects["demo"] = objectForTest(t, extensionForTest("demo"))
	client.versions["demo"] = listForTest[ExtensionVersion](
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
	)
	plan := planForTest("demo", "1.0.0", "Installed")
	plan.Spec.Extension.Name = "other"
	client.planObjects["demo"] = objectForTest(t, plan)

	_, err := NewService(client).Show(context.Background(), "demo", "")
	if err == nil ||
		!strings.Contains(err.Error(), `references extension "other"`) {
		t.Fatalf("Show() error = %v, want identity rejection", err)
	}
}

func TestServiceShowSelectsOnlyExactOpaqueVersion(t *testing.T) {
	client := newFakeAPIClient(t)
	client.extensionObjects["demo"] = objectForTest(t, extensionForTest("demo"))
	selected := versionForTest(
		"demo",
		"demo-v1.0.0+build",
		"v1.0.0+build",
	)
	client.versionObjects[selected.Metadata.Name] = objectForTest(t, selected)
	client.versions["demo"] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		versionForTest("demo", "wrong-name", "v1.0.0+build"),
	)

	result, err := NewService(client).Show(
		context.Background(),
		"demo",
		"v1.0.0+build",
	)
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if result.SelectedVersion == nil ||
		result.SelectedVersion.Value.Metadata.Name != "demo-v1.0.0+build" {
		t.Fatalf("SelectedVersion = %#v", result.SelectedVersion)
	}
	if !strings.Contains(
		string(result.RawJSON()),
		`"version":"v1.0.0+build"`,
	) {
		t.Fatalf("RawJSON() = %s", result.RawJSON())
	}
	if want := []string{
		"get extension demo",
		"get extension version demo-v1.0.0+build",
	}; !reflect.DeepEqual(client.calls, want) {
		t.Fatalf("calls = %v, want %v", client.calls, want)
	}
}

func TestServiceExactVersionRequiresControllerResourceIdentity(t *testing.T) {
	client := newFakeAPIClient(t)
	client.versions["demo"] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		versionForTest("demo", "demo-1-2-1", "1.2.1"),
	)

	_, err := NewService(client).exactVersion(
		context.Background(),
		"demo",
		"1.2.1",
	)
	if err == nil ||
		!strings.Contains(err.Error(), `controller requires resource "demo-1.2.1"`) {
		t.Fatalf("exactVersion() error = %v", err)
	}
}

func TestServiceVersionsSortsSemanticVersionsDescending(t *testing.T) {
	client := newFakeAPIClient(t)
	client.versions["demo"] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		versionForTest("demo", "demo-1-2", "1.2.0"),
		versionForTest("demo", "demo-2", "2.0.0"),
		versionForTest("demo", "demo-1-10", "1.10.0"),
	)

	result, err := NewService(client).Versions(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	var versions []string
	for _, item := range result.Items.Items {
		versions = append(versions, item.Value.Spec.Version)
	}
	if want := []string{"2.0.0", "1.10.0", "1.2.0"}; !reflect.DeepEqual(versions, want) {
		t.Fatalf("versions = %v, want %v", versions, want)
	}
}

func TestServiceVersionsUsesLexicalOrderWhenAnyVersionIsInvalid(t *testing.T) {
	client := newFakeAPIClient(t)
	client.versions["demo"] = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
		versionForTest("demo", "demo-z", "z"),
		versionForTest("demo", "demo-2", "2.0.0"),
		versionForTest("demo", "demo-10", "10.0.0"),
	)

	result, err := NewService(client).Versions(context.Background(), "demo")
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	var versions []string
	for _, item := range result.Items.Items {
		versions = append(versions, item.Value.Spec.Version)
	}
	if want := []string{"z", "2.0.0", "10.0.0"}; !reflect.DeepEqual(versions, want) {
		t.Fatalf("versions = %v, want lexical %v", versions, want)
	}
}

func TestServiceStatusReturnsSortedListOrNamedObject(t *testing.T) {
	client := newFakeAPIClient(t)
	alpha := planForTest("alpha", "1.0.0", "Installed")
	zeta := planForTest("zeta", "2.0.0", "Installing")
	client.installPlans = listForTest(
		t,
		"kubesphere.io/v1alpha1",
		"InstallPlanList",
		zeta,
		alpha,
	)
	client.planObjects["zeta"] = objectForTest(t, zeta)
	service := NewService(client)

	list, err := service.Status(context.Background(), "")
	if err != nil {
		t.Fatalf("Status(list) error = %v", err)
	}
	if got := list.List.Items[0].Value.Metadata.Name; got != "alpha" {
		t.Fatalf("first status name = %q, want alpha", got)
	}
	named, err := service.Status(context.Background(), "zeta")
	if err != nil {
		t.Fatalf("Status(named) error = %v", err)
	}
	if named.Object == nil || named.Object.Value.Metadata.Name != "zeta" {
		t.Fatalf("named status = %#v", named)
	}
}

func TestServiceStatusRejectsMismatchedInstallPlanIdentity(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "named"}[named], func(t *testing.T) {
			client := newFakeAPIClient(t)
			plan := planForTest("demo", "1.0.0", "Installed")
			plan.Spec.Extension.Name = "other"
			client.installPlans = listForTest(
				t,
				"kubesphere.io/v1alpha1",
				"InstallPlanList",
				plan,
			)
			client.planObjects["demo"] = objectForTest(t, plan)
			name := ""
			if named {
				name = "demo"
			}

			_, err := NewService(client).Status(context.Background(), name)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					`references extension "other"`,
				) {
				t.Fatalf("Status(%q) error = %v", name, err)
			}
		})
	}
}
