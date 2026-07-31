package tenant

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/util/validation"
	clientkubesphere "kubesphere.io/ksctl/pkg/client/kubesphere"
)

type namespaceResolver interface {
	Namespaces(ctx context.Context, workspace string) ([]string, error)
}

type namespaceCacheEntry struct {
	once  sync.Once
	names []string
	err   error
}

type kubeSphereNamespaceResolver struct {
	getter RESTClientGetter

	mu      sync.Mutex
	entries map[string]*namespaceCacheEntry
}

func newNamespaceResolver(getter RESTClientGetter) namespaceResolver {
	return &kubeSphereNamespaceResolver{
		getter:  getter,
		entries: make(map[string]*namespaceCacheEntry),
	}
}

func (r *kubeSphereNamespaceResolver) Namespaces(
	ctx context.Context,
	workspace string,
) ([]string, error) {
	entry := r.entry(workspace)
	entry.once.Do(func() {
		entry.names, entry.err = r.load(ctx, workspace)
	})
	return slices.Clone(entry.names), entry.err
}

func (r *kubeSphereNamespaceResolver) entry(workspace string) *namespaceCacheEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, found := r.entries[workspace]
	if !found {
		entry = &namespaceCacheEntry{}
		r.entries[workspace] = entry
	}
	return entry
}

func (r *kubeSphereNamespaceResolver) load(
	ctx context.Context,
	workspace string,
) ([]string, error) {
	if r.getter == nil {
		return nil, fmt.Errorf("resolve tenant namespaces: KubeSphere REST client getter is required")
	}
	cluster, err := r.getter.KubeSphereCluster()
	if err != nil {
		return nil, fmt.Errorf("resolve tenant namespaces: resolve cluster: %w", err)
	}
	config, err := r.getter.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("resolve tenant namespaces: resolve connection: %w", err)
	}
	restClient, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant namespaces: build client: %w", err)
	}
	response, err := NewClient(restClient).Get(ctx, Request{
		Resource:  ResourceNamespace,
		Workspace: workspace,
		Cluster:   cluster,
	})
	if err != nil {
		if workspace == "" {
			return nil, fmt.Errorf("resolve tenant namespaces: %w", err)
		}
		return nil, fmt.Errorf(
			"resolve tenant namespaces in workspace %q: %w",
			workspace,
			err,
		)
	}

	names := make([]string, 0, len(response.Objects))
	seen := make(map[string]struct{}, len(response.Objects))
	for index, object := range response.Objects {
		metadata, ok := object["metadata"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"resolve tenant namespaces: item %d is missing metadata.name",
				index,
			)
		}
		name, ok := metadata["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf(
				"resolve tenant namespaces: item %d is missing metadata.name",
				index,
			)
		}
		if messages := validation.IsDNS1123Label(name); len(messages) != 0 {
			return nil, fmt.Errorf(
				"resolve tenant namespaces: invalid namespace %q: %v",
				name,
				messages,
			)
		}
		if _, found := seen[name]; found {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}
