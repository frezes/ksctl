package tenant

import (
	"context"
	"encoding/json"
	"fmt"

	kubesphererest "kubesphere.io/client-go/rest"
)

const apiPath = "/kapis/tenant.kubesphere.io/v1beta1"

type Resource string

const (
	ResourceWorkspace Resource = "workspace"
	ResourceNamespace Resource = "namespace"
	ResourceCluster   Resource = "cluster"
)

type Request struct {
	Resource  Resource
	Name      string
	Workspace string
	Cluster   string
}

type Response struct {
	Raw     []byte
	Objects []map[string]any
	IsList  bool
}

type Client struct {
	restClient kubesphererest.Interface
}

func NewClient(restClient kubesphererest.Interface) *Client {
	return &Client{restClient: restClient}
}

func (c *Client) Get(ctx context.Context, request Request) (Response, error) {
	if c == nil || c.restClient == nil {
		return Response{}, fmt.Errorf("KubeSphere REST client is required")
	}
	segments, list, err := requestSegments(request)
	if err != nil {
		return Response{}, err
	}
	get := c.restClient.Get()
	if request.Resource == ResourceNamespace && request.Cluster != "" {
		get.Cluster(request.Cluster)
	}
	raw, err := get.AbsPath(segments...).Do(ctx).Raw()
	if err != nil {
		return Response{}, fmt.Errorf("get tenant %s: %w", request.Resource, err)
	}
	objects, err := decodeObjects(raw, list, request.Resource)
	if err != nil {
		return Response{}, err
	}
	return Response{Raw: raw, Objects: objects, IsList: list}, nil
}

func requestSegments(request Request) ([]string, bool, error) {
	if request.Name != "" && request.Resource != ResourceWorkspace {
		return nil, false, fmt.Errorf("tenant resource name is only supported for workspace")
	}
	if request.Workspace != "" && request.Resource == ResourceWorkspace {
		return nil, false, fmt.Errorf("workspace scope is not supported for tenant workspace")
	}
	if err := validateSegment("workspace name", request.Name); err != nil {
		return nil, false, err
	}
	if err := validateSegment("workspace", request.Workspace); err != nil {
		return nil, false, err
	}
	if request.Resource == ResourceNamespace {
		if err := validateSegment("cluster", request.Cluster); err != nil {
			return nil, false, err
		}
	}

	switch request.Resource {
	case ResourceWorkspace:
		if request.Name == "" {
			return []string{apiPath, "workspacetemplates"}, true, nil
		}
		return []string{apiPath, "workspacetemplates", request.Name}, false, nil
	case ResourceNamespace:
		if request.Workspace == "" {
			return []string{apiPath, "namespaces"}, true, nil
		}
		return []string{apiPath, "workspaces", request.Workspace, "namespaces"}, true, nil
	case ResourceCluster:
		if request.Workspace == "" {
			return []string{apiPath, "clusters"}, true, nil
		}
		return []string{apiPath, "workspaces", request.Workspace, "clusters"}, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported tenant resource %q", request.Resource)
	}
}

func validateSegment(label, value string) error {
	if value == "" {
		return nil
	}
	if messages := kubesphererest.IsValidPathSegmentName(value); len(messages) != 0 {
		return fmt.Errorf("invalid %s %q: %v", label, value, messages)
	}
	return nil
}

func decodeObjects(raw []byte, list bool, resource Resource) ([]map[string]any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode tenant %s response: %w", resource, err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tenant %s response is not an object", resource)
	}
	if !list {
		return []map[string]any{object}, nil
	}
	rawItems, found := object["items"]
	if !found {
		return nil, fmt.Errorf(`tenant %s list response is missing "items" array`, resource)
	}
	items, ok := rawItems.([]any)
	if !ok {
		return nil, fmt.Errorf(`tenant %s list response "items" is not an array`, resource)
	}
	objects := make([]map[string]any, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tenant %s list item %d is not an object", resource, index)
		}
		objects = append(objects, object)
	}
	return objects, nil
}
