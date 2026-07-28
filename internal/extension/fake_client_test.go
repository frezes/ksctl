package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type fakeAPIClient struct {
	t testing.TB

	extensions       List[Extension]
	versions         map[string]List[ExtensionVersion]
	installPlans     List[InstallPlan]
	extensionObjects map[string]Object[Extension]
	planObjects      map[string]Object[InstallPlan]
	getExtensionErrs map[string]error
	getPlanErrs      map[string]error
	createErr        error
	patchErr         error
	deleteErr        error
	createResponse   *Object[InstallPlan]
	patchResponses   map[string]Object[InstallPlan]
	planReads        map[string][]fakePlanRead
	namespaces       map[string]Namespace
	namespaceErrs    map[string]error
	jobs             map[string]Job
	jobErrs          map[string]error
	pods             map[string]PodList
	podErrs          map[string]error

	createdPlans []InstallPlan
	patches      [][]byte
	deletedPlans []string
	calls        []string
}

type fakePlanRead struct {
	object Object[InstallPlan]
	err    error
}

func newFakeAPIClient(t testing.TB) *fakeAPIClient {
	t.Helper()
	return &fakeAPIClient{
		t:                t,
		versions:         map[string]List[ExtensionVersion]{},
		extensionObjects: map[string]Object[Extension]{},
		planObjects:      map[string]Object[InstallPlan]{},
		getExtensionErrs: map[string]error{},
		getPlanErrs:      map[string]error{},
		patchResponses:   map[string]Object[InstallPlan]{},
		planReads:        map[string][]fakePlanRead{},
		namespaces:       map[string]Namespace{},
		namespaceErrs:    map[string]error{},
		jobs:             map[string]Job{},
		jobErrs:          map[string]error{},
		pods:             map[string]PodList{},
		podErrs:          map[string]error{},
	}
}

func objectForTest[T any](t testing.TB, value T) Object[T] {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test object: %v", err)
	}
	return Object[T]{Value: value, doc: JSONDocument{data: raw}}
}

func listForTest[T any](t testing.TB, apiVersion, kind string, values ...T) List[T] {
	t.Helper()
	items := make([]Object[T], 0, len(values))
	rawItems := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		object := objectForTest(t, value)
		items = append(items, object)
		rawItems = append(rawItems, object.RawJSON())
	}
	raw, err := json.Marshal(map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"items":      rawItems,
	})
	if err != nil {
		t.Fatalf("marshal test list: %v", err)
	}
	return List[T]{Items: items, doc: JSONDocument{data: raw}}
}

func extensionForTest(name string) Extension {
	return Extension{
		APIVersion: "kubesphere.io/v1alpha1",
		Kind:       "Extension",
		Metadata:   ObjectMeta{Name: name},
	}
}

func versionForTest(extension, resourceName, version string) ExtensionVersion {
	return ExtensionVersion{
		APIVersion: "kubesphere.io/v1alpha1",
		Kind:       "ExtensionVersion",
		Metadata: ObjectMeta{
			Name: resourceName,
			Labels: map[string]string{
				"kubesphere.io/extension-ref": extension,
			},
		},
		Spec: ExtensionVersionSpec{Version: version},
	}
}

func planForTest(name, version, state string) InstallPlan {
	return InstallPlan{
		APIVersion: "kubesphere.io/v1alpha1",
		Kind:       "InstallPlan",
		Metadata:   ObjectMeta{Name: name, ResourceVersion: "1"},
		Spec: InstallPlanSpec{
			Extension:       ExtensionRef{Name: name, Version: version},
			Enabled:         true,
			UpgradeStrategy: "Manual",
		},
		Status: InstallPlanStatus{
			InstallationStatus: InstallationStatus{State: state, Version: version},
		},
	}
}

func notFound(resource, name string) error {
	return apierrors.NewNotFound(
		schema.GroupResource{Group: "kubesphere.io", Resource: resource},
		name,
	)
}

func (f *fakeAPIClient) ListExtensions(_ context.Context, category string) (List[Extension], error) {
	f.calls = append(f.calls, "list extensions category="+category)
	return f.extensions, nil
}

func (f *fakeAPIClient) GetExtension(_ context.Context, name string) (Object[Extension], error) {
	f.calls = append(f.calls, "get extension "+name)
	if err := f.getExtensionErrs[name]; err != nil {
		return Object[Extension]{}, err
	}
	if object, found := f.extensionObjects[name]; found {
		return object, nil
	}
	return Object[Extension]{}, notFound("extensions", name)
}

func (f *fakeAPIClient) ListExtensionVersions(
	_ context.Context,
	name string,
) (List[ExtensionVersion], error) {
	f.calls = append(f.calls, "list versions "+name)
	if list, found := f.versions[name]; found {
		return list, nil
	}
	return listForTest[ExtensionVersion](
		f.t,
		"kubesphere.io/v1alpha1",
		"ExtensionVersionList",
	), nil
}

func (f *fakeAPIClient) ListInstallPlans(context.Context) (List[InstallPlan], error) {
	f.calls = append(f.calls, "list install plans")
	return f.installPlans, nil
}

func (f *fakeAPIClient) GetInstallPlan(_ context.Context, name string) (Object[InstallPlan], error) {
	f.calls = append(f.calls, "get install plan "+name)
	if reads := f.planReads[name]; len(reads) != 0 {
		read := reads[0]
		f.planReads[name] = reads[1:]
		return read.object, read.err
	}
	if err := f.getPlanErrs[name]; err != nil {
		return Object[InstallPlan]{}, err
	}
	if object, found := f.planObjects[name]; found {
		return object, nil
	}
	return Object[InstallPlan]{}, notFound("installplans", name)
}

func (f *fakeAPIClient) CreateInstallPlan(
	_ context.Context,
	plan InstallPlan,
) (Object[InstallPlan], error) {
	f.calls = append(f.calls, "create install plan "+plan.Metadata.Name)
	f.createdPlans = append(f.createdPlans, plan)
	if f.createErr != nil {
		return Object[InstallPlan]{}, f.createErr
	}
	if f.createResponse != nil {
		return *f.createResponse, nil
	}
	return objectForTest(f.t, plan), nil
}

func (f *fakeAPIClient) PatchInstallPlan(
	_ context.Context,
	name string,
	patch []byte,
) (Object[InstallPlan], error) {
	f.calls = append(f.calls, "patch install plan "+name)
	f.patches = append(f.patches, append([]byte(nil), patch...))
	if f.patchErr != nil {
		return Object[InstallPlan]{}, f.patchErr
	}
	if object, found := f.patchResponses[name]; found {
		return object, nil
	}
	if object, found := f.planObjects[name]; found {
		return object, nil
	}
	return Object[InstallPlan]{}, fmt.Errorf("no patch response for %q", name)
}

func (f *fakeAPIClient) DeleteInstallPlan(_ context.Context, name string) error {
	f.calls = append(f.calls, "delete install plan "+name)
	f.deletedPlans = append(f.deletedPlans, name)
	return f.deleteErr
}

func (f *fakeAPIClient) GetNamespace(_ context.Context, name string) (Namespace, error) {
	f.calls = append(f.calls, "get namespace "+name)
	if err := f.namespaceErrs[name]; err != nil {
		return Namespace{}, err
	}
	if namespace, found := f.namespaces[name]; found {
		return namespace, nil
	}
	return Namespace{}, notFound("namespaces", name)
}

func (f *fakeAPIClient) GetJob(
	_ context.Context,
	namespace string,
	name string,
) (Job, error) {
	key := namespace + "/" + name
	f.calls = append(f.calls, "get job "+key)
	if err := f.jobErrs[key]; err != nil {
		return Job{}, err
	}
	if job, found := f.jobs[key]; found {
		return job, nil
	}
	return Job{}, notFound("jobs", name)
}

func (f *fakeAPIClient) ListPodsForJob(
	_ context.Context,
	namespace string,
	job string,
) (PodList, error) {
	key := namespace + "/" + job
	f.calls = append(f.calls, "list pods "+key)
	if err := f.podErrs[key]; err != nil {
		return PodList{}, err
	}
	return f.pods[key], nil
}
