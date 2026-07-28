package extension

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kubesphererest "kubesphere.io/client-go/rest"
)

const extensionAPIPath = "/apis/kubesphere.io/v1alpha1"

type APIClient interface {
	ListExtensions(context.Context, string) (List[Extension], error)
	GetExtension(context.Context, string) (Object[Extension], error)
	ListExtensionVersions(context.Context, string) (List[ExtensionVersion], error)
	ListInstallPlans(context.Context) (List[InstallPlan], error)
	GetInstallPlan(context.Context, string) (Object[InstallPlan], error)
	CreateInstallPlan(context.Context, InstallPlan) (Object[InstallPlan], error)
	PatchInstallPlan(context.Context, string, []byte) (Object[InstallPlan], error)
	DeleteInstallPlan(context.Context, string) error
	GetJob(context.Context, string, string) (Job, error)
	ListPodsForJob(context.Context, string, string) (PodList, error)
}

type restClient struct {
	client kubesphererest.Interface
}

func NewRESTClient(client kubesphererest.Interface) APIClient {
	return &restClient{client: client}
}

func validatePathName(kind, name string) error {
	if messages := kubesphererest.IsValidPathSegmentName(name); len(messages) != 0 {
		return fmt.Errorf("invalid %s %q: %v", kind, name, messages)
	}
	return nil
}

func resultRaw(result kubesphererest.Result) ([]byte, error) {
	if err := result.Error(); err != nil {
		return nil, err
	}
	return result.Raw()
}

func decodeNamedObject[T any](kind, name string, raw []byte) (Object[T], error) {
	object, err := decodeObject[T](raw)
	if err != nil {
		return Object[T]{}, fmt.Errorf("decode %s %q: %w", kind, name, err)
	}
	var envelope struct {
		Metadata ObjectMeta `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Object[T]{}, fmt.Errorf("decode %s identity %q: %w", kind, name, err)
	}
	if envelope.Metadata.Name != name {
		return Object[T]{}, fmt.Errorf("%s %q response has name %q", kind, name, envelope.Metadata.Name)
	}
	return object, nil
}

func (c *restClient) ListExtensions(ctx context.Context, category string) (List[Extension], error) {
	request := c.client.Get().AbsPath(extensionAPIPath, "extensions")
	if category != "" {
		request.Param("labelSelector", labels.Set{
			"kubesphere.io/category": category,
		}.AsSelector().String())
	}
	raw, err := resultRaw(request.Do(ctx))
	if err != nil {
		return List[Extension]{}, fmt.Errorf("list extensions: %w", err)
	}
	list, err := decodeList[Extension](raw)
	if err != nil {
		return List[Extension]{}, fmt.Errorf("decode extensions: %w", err)
	}
	return list, nil
}

func (c *restClient) GetExtension(ctx context.Context, name string) (Object[Extension], error) {
	if err := validatePathName("extension", name); err != nil {
		return Object[Extension]{}, err
	}
	raw, err := resultRaw(c.client.Get().
		AbsPath(extensionAPIPath, "extensions", name).
		Do(ctx))
	if err != nil {
		return Object[Extension]{}, fmt.Errorf("get extension %q: %w", name, err)
	}
	return decodeNamedObject[Extension]("extension", name, raw)
}

func (c *restClient) ListExtensionVersions(
	ctx context.Context,
	extensionName string,
) (List[ExtensionVersion], error) {
	if err := validatePathName("extension", extensionName); err != nil {
		return List[ExtensionVersion]{}, err
	}
	raw, err := resultRaw(c.client.Get().
		AbsPath(extensionAPIPath, "extensionversions").
		Param("labelSelector", labels.Set{
			"kubesphere.io/extension-ref": extensionName,
		}.AsSelector().String()).
		Do(ctx))
	if err != nil {
		return List[ExtensionVersion]{}, fmt.Errorf(
			"list versions for extension %q: %w",
			extensionName,
			err,
		)
	}
	list, err := decodeList[ExtensionVersion](raw)
	if err != nil {
		return List[ExtensionVersion]{}, fmt.Errorf(
			"decode versions for extension %q: %w",
			extensionName,
			err,
		)
	}
	return list, nil
}

func (c *restClient) ListInstallPlans(ctx context.Context) (List[InstallPlan], error) {
	raw, err := resultRaw(c.client.Get().
		AbsPath(extensionAPIPath, "installplans").
		Do(ctx))
	if err != nil {
		return List[InstallPlan]{}, fmt.Errorf("list install plans: %w", err)
	}
	list, err := decodeList[InstallPlan](raw)
	if err != nil {
		return List[InstallPlan]{}, fmt.Errorf("decode install plans: %w", err)
	}
	return list, nil
}

func (c *restClient) GetInstallPlan(ctx context.Context, name string) (Object[InstallPlan], error) {
	if err := validatePathName("extension", name); err != nil {
		return Object[InstallPlan]{}, err
	}
	raw, err := resultRaw(c.client.Get().
		AbsPath(extensionAPIPath, "installplans", name).
		Do(ctx))
	if err != nil {
		return Object[InstallPlan]{}, fmt.Errorf("get install plan %q: %w", name, err)
	}
	return decodeNamedObject[InstallPlan]("install plan", name, raw)
}

func (c *restClient) CreateInstallPlan(
	ctx context.Context,
	plan InstallPlan,
) (Object[InstallPlan], error) {
	if err := validatePathName("extension", plan.Metadata.Name); err != nil {
		return Object[InstallPlan]{}, err
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return Object[InstallPlan]{}, fmt.Errorf(
			"encode install plan %q: %w",
			plan.Metadata.Name,
			err,
		)
	}
	raw, err := resultRaw(c.client.Post().
		AbsPath(extensionAPIPath, "installplans").
		SetHeader("Content-Type", "application/json").
		Body(data).
		Do(ctx))
	if err != nil {
		return Object[InstallPlan]{}, fmt.Errorf(
			"create install plan %q: %w",
			plan.Metadata.Name,
			err,
		)
	}
	return decodeNamedObject[InstallPlan]("install plan", plan.Metadata.Name, raw)
}

func (c *restClient) PatchInstallPlan(
	ctx context.Context,
	name string,
	patch []byte,
) (Object[InstallPlan], error) {
	if err := validatePathName("extension", name); err != nil {
		return Object[InstallPlan]{}, err
	}
	raw, err := resultRaw(c.client.Patch(types.MergePatchType).
		AbsPath(extensionAPIPath, "installplans", name).
		Body(patch).
		Do(ctx))
	if err != nil {
		return Object[InstallPlan]{}, fmt.Errorf("patch install plan %q: %w", name, err)
	}
	return decodeNamedObject[InstallPlan]("install plan", name, raw)
}

func (c *restClient) DeleteInstallPlan(ctx context.Context, name string) error {
	if err := validatePathName("extension", name); err != nil {
		return err
	}
	result := c.client.Delete().
		AbsPath(extensionAPIPath, "installplans", name).
		Do(ctx)
	if err := result.Error(); err != nil {
		return fmt.Errorf("delete install plan %q: %w", name, err)
	}
	return nil
}

func (c *restClient) GetJob(ctx context.Context, namespace, name string) (Job, error) {
	if err := validatePathName("namespace", namespace); err != nil {
		return Job{}, err
	}
	if err := validatePathName("job", name); err != nil {
		return Job{}, err
	}
	raw, err := resultRaw(c.client.Get().
		AbsPath("/apis/batch/v1", "namespaces", namespace, "jobs", name).
		Do(ctx))
	if err != nil {
		return Job{}, fmt.Errorf("get job %q: %w", namespace+"/"+name, err)
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return Job{}, fmt.Errorf("decode job %q: %w", namespace+"/"+name, err)
	}
	return job, nil
}

func (c *restClient) ListPodsForJob(
	ctx context.Context,
	namespace string,
	jobName string,
) (PodList, error) {
	if err := validatePathName("namespace", namespace); err != nil {
		return PodList{}, err
	}
	if err := validatePathName("job", jobName); err != nil {
		return PodList{}, err
	}
	raw, err := resultRaw(c.client.Get().
		AbsPath("/api/v1", "namespaces", namespace, "pods").
		Param("labelSelector", labels.Set{"job-name": jobName}.AsSelector().String()).
		Do(ctx))
	if err != nil {
		return PodList{}, fmt.Errorf("list pods for job %q: %w", namespace+"/"+jobName, err)
	}
	var pods PodList
	if err := json.Unmarshal(raw, &pods); err != nil {
		return PodList{}, fmt.Errorf("decode pods for job %q: %w", namespace+"/"+jobName, err)
	}
	return pods, nil
}
