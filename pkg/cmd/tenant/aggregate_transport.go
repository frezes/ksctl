package tenant

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type aggregateMode uint8

const (
	aggregateDisabled aggregateMode = iota
	aggregateOnForbidden
	aggregateWorkspace
)

type aggregationState struct {
	mode      aggregateMode
	workspace string
	used      atomic.Bool
}

type aggregatingRESTClientGetter struct {
	delegate genericclioptions.RESTClientGetter
	resolver namespaceResolver
	state    *aggregationState
}

func newAggregatingRESTClientGetter(
	delegate genericclioptions.RESTClientGetter,
	resolver namespaceResolver,
	state *aggregationState,
) genericclioptions.RESTClientGetter {
	return &aggregatingRESTClientGetter{
		delegate: delegate,
		resolver: resolver,
		state:    state,
	}
}

func (g *aggregatingRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	if g.delegate == nil {
		return nil, fmt.Errorf("configure tenant resource aggregation: Kubernetes client getter is required")
	}
	config, err := g.delegate.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	mapper, err := g.delegate.ToRESTMapper()
	if err != nil {
		return nil, err
	}
	base, err := rest.TransportFor(config)
	if err != nil {
		return nil, fmt.Errorf("configure tenant resource aggregation transport: %w", err)
	}
	host, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("parse Kubernetes API host %q: %w", config.Host, err)
	}

	result := rest.AnonymousClientConfig(config)
	result.TLSClientConfig = rest.TLSClientConfig{}
	result.UserAgent = ""
	result.Transport = &aggregatingRoundTripper{
		base:     base,
		hostPath: host.Path,
		mapper:   mapper,
		resolver: g.resolver,
		state:    g.state,
	}
	return result, nil
}

func (g *aggregatingRESTClientGetter) ToDiscoveryClient() (
	discovery.CachedDiscoveryInterface,
	error,
) {
	return g.delegate.ToDiscoveryClient()
}

func (g *aggregatingRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return g.delegate.ToRESTMapper()
}

func (g *aggregatingRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return g.delegate.ToRawKubeConfigLoader()
}

type aggregatingRoundTripper struct {
	base     http.RoundTripper
	hostPath string
	mapper   meta.RESTMapper
	resolver namespaceResolver
	state    *aggregationState

	mappings sync.Map
}

func (t *aggregatingRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	if t.state == nil ||
		t.state.mode == aggregateDisabled ||
		request.Method != http.MethodGet {
		return t.base.RoundTrip(request)
	}
	endpoint, ok := parseResourceEndpoint(t.hostPath, request.URL.Path)
	if !ok || endpoint.Namespace != "" {
		return t.base.RoundTrip(request)
	}
	mapping, err := t.mappingFor(endpoint.GVR)
	if err != nil || mapping == nil || mapping.Scope == nil {
		return t.base.RoundTrip(request)
	}
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		if t.state.mode == aggregateWorkspace {
			return nil, fmt.Errorf(
				"resource %s is cluster-scoped and cannot be filtered by workspace %q",
				endpoint.GVR.Resource,
				t.state.workspace,
			)
		}
		return t.base.RoundTrip(request)
	}

	switch t.state.mode {
	case aggregateWorkspace:
		return t.aggregate(request, endpoint, mapping)
	case aggregateOnForbidden:
		response, err := t.base.RoundTrip(request)
		if err != nil || response.StatusCode != http.StatusForbidden {
			return response, err
		}
		response.Body.Close()
		return t.aggregate(request, endpoint, mapping)
	default:
		return t.base.RoundTrip(request)
	}
}

type mappingResult struct {
	mapping *meta.RESTMapping
	err     error
}

func (t *aggregatingRoundTripper) mappingFor(
	resource schema.GroupVersionResource,
) (*meta.RESTMapping, error) {
	if cached, found := t.mappings.Load(resource); found {
		result := cached.(mappingResult)
		return result.mapping, result.err
	}
	if t.mapper == nil {
		return nil, fmt.Errorf("Kubernetes REST mapper is required")
	}
	kind, err := t.mapper.KindFor(resource)
	var mapping *meta.RESTMapping
	if err == nil {
		mapping, err = t.mapper.RESTMapping(kind.GroupKind(), resource.Version)
	}
	result := mappingResult{mapping: mapping, err: err}
	actual, _ := t.mappings.LoadOrStore(resource, result)
	stored := actual.(mappingResult)
	return stored.mapping, stored.err
}

func (t *aggregatingRoundTripper) aggregate(
	request *http.Request,
	endpoint resourceEndpoint,
	mapping *meta.RESTMapping,
) (*http.Response, error) {
	t.state.used.Store(true)
	if t.resolver == nil {
		return nil, fmt.Errorf("aggregate Kubernetes resources: namespace resolver is required")
	}
	namespaces, err := t.resolver.Namespaces(request.Context(), t.state.workspace)
	if err != nil {
		return nil, err
	}

	documents := make([]scopedDocument, 0, len(namespaces))
	for _, namespace := range namespaces {
		response, err := t.base.RoundTrip(namespaceRequest(request, endpoint, namespace))
		if err != nil {
			return nil, fmt.Errorf(
				"get %s in namespace %q: %w",
				endpoint.GVR.Resource,
				namespace,
				err,
			)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf(
				"read %s response in namespace %q: %w",
				endpoint.GVR.Resource,
				namespace,
				readErr,
			)
		}
		if closeErr != nil {
			return nil, fmt.Errorf(
				"close %s response in namespace %q: %w",
				endpoint.GVR.Resource,
				namespace,
				closeErr,
			)
		}
		if response.StatusCode < http.StatusOK ||
			response.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf(
				"get %s in namespace %q: server returned %s",
				endpoint.GVR.Resource,
				namespace,
				response.Status,
			)
		}
		documents = append(documents, scopedDocument{
			Namespace: namespace,
			Body:      body,
		})
	}

	body, err := mergeDocuments(documents, mapping, tableResponseRequested(request))
	if err != nil {
		return nil, err
	}
	return aggregatedResponse(request, body), nil
}

func namespaceRequest(
	request *http.Request,
	endpoint resourceEndpoint,
	namespace string,
) *http.Request {
	scoped := request.Clone(request.Context())
	scoped.URL = cloneURL(request.URL)
	scoped.URL.Path = endpoint.pathForNamespace(namespace)
	scoped.URL.RawPath = ""
	scoped.RequestURI = ""
	return scoped
}

func cloneURL(source *url.URL) *url.URL {
	cloned := *source
	return &cloned
}

func tableResponseRequested(request *http.Request) bool {
	for _, value := range request.Header.Values("Accept") {
		if strings.Contains(strings.ToLower(value), "as=table") {
			return true
		}
	}
	return false
}

func aggregatedResponse(request *http.Request, body []byte) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}
}
