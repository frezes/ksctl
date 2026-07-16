package kubernetes

import (
	"context"
	"encoding/json"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	kubernetesscheme "k8s.io/client-go/kubernetes/scheme"
)

type fallbackDiscoveryClient struct {
	discovery.DiscoveryInterface
	coreV1Fallback discovery.DiscoveryInterface

	resourcesMu       sync.RWMutex
	fallbackResources map[string]*metav1.APIResourceList
}

func newFallbackDiscoveryClient(live, coreV1Fallback discovery.DiscoveryInterface) discovery.DiscoveryInterface {
	return &fallbackDiscoveryClient{
		DiscoveryInterface: live,
		coreV1Fallback:     coreV1Fallback,
	}
}

func (c *fallbackDiscoveryClient) ServerGroups() (*metav1.APIGroupList, error) {
	groups, err := c.DiscoveryInterface.ServerGroups()
	if err == nil {
		c.setFallbackResources(nil)
		return groups, nil
	}

	groupVersions, knownResources := c.fallbackGroupVersions()
	resources := c.probeGroupVersions(groupVersions, knownResources)
	fallback := groupsFromResources(groupVersions, resources)
	if len(fallback.Groups) == 0 {
		c.setFallbackResources(nil)
		return groups, err
	}

	c.setFallbackResources(resources)

	return fallback, nil
}

func (c *fallbackDiscoveryClient) setFallbackResources(resources map[string]*metav1.APIResourceList) {
	c.resourcesMu.Lock()
	c.fallbackResources = resources
	c.resourcesMu.Unlock()
}

func (c *fallbackDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	c.resourcesMu.RLock()
	resources := c.fallbackResources[groupVersion]
	c.resourcesMu.RUnlock()
	if resources != nil {
		return resources, nil
	}
	resources, err := c.DiscoveryInterface.ServerResourcesForGroupVersion(groupVersion)
	if err != nil && groupVersion == "v1" && c.coreV1Fallback != nil {
		return c.coreV1Fallback.ServerResourcesForGroupVersion(groupVersion)
	}
	return resources, err
}

func (c *fallbackDiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return discovery.ServerGroupsAndResources(c)
}

func (c *fallbackDiscoveryClient) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return discovery.ServerPreferredResources(c)
}

func (c *fallbackDiscoveryClient) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	return discovery.ServerPreferredNamespacedResources(c)
}

func (c *fallbackDiscoveryClient) fallbackGroupVersions() ([]schema.GroupVersion, map[string]*metav1.APIResourceList) {
	groupVersions := []schema.GroupVersion{{Version: "v1"}}
	seen := map[schema.GroupVersion]struct{}{{Version: "v1"}: {}}
	add := func(gv schema.GroupVersion) {
		if gv.Version == "" {
			return
		}
		if _, ok := seen[gv]; ok {
			return
		}
		seen[gv] = struct{}{}
		groupVersions = append(groupVersions, gv)
	}

	for _, gv := range kubernetesscheme.Scheme.PrioritizedVersionsAllGroups() {
		add(gv)
	}
	customGroupVersions, resources := c.customResourceDiscovery()
	for _, gv := range customGroupVersions {
		add(gv)
	}
	for _, path := range []string{
		"/apis/apiregistration.k8s.io/v1/apiservices",
		"/apis/extensions.kubesphere.io/v1alpha1/apiservices",
	} {
		for _, gv := range c.apiServiceGroupVersions(path) {
			add(gv)
		}
	}
	return groupVersions, resources
}

func (c *fallbackDiscoveryClient) customResourceDiscovery() ([]schema.GroupVersion, map[string]*metav1.APIResourceList) {
	var groupVersions []schema.GroupVersion
	resources := map[string]*metav1.APIResourceList{}
	continueToken := ""
	for {
		data, err := c.getList("/apis/apiextensions.k8s.io/v1/customresourcedefinitions", continueToken)
		if err != nil {
			return groupVersions, resources
		}
		list := customResourceDefinitionList{}
		if err := json.Unmarshal(data, &list); err != nil {
			return groupVersions, resources
		}
		for _, item := range list.Items {
			for _, version := range item.Spec.Versions {
				if version.Served && version.Storage {
					groupVersions = append(groupVersions, schema.GroupVersion{Group: item.Spec.Group, Version: version.Name})
				}
			}
			for _, version := range item.Spec.Versions {
				if version.Served && !version.Storage {
					groupVersions = append(groupVersions, schema.GroupVersion{Group: item.Spec.Group, Version: version.Name})
				}
			}
			for _, version := range item.Spec.Versions {
				if !version.Served {
					continue
				}
				gv := schema.GroupVersion{Group: item.Spec.Group, Version: version.Name}.String()
				list := resources[gv]
				if list == nil {
					list = &metav1.APIResourceList{GroupVersion: gv}
					resources[gv] = list
				}
				list.APIResources = append(list.APIResources, metav1.APIResource{
					Name:         item.Spec.Names.Plural,
					SingularName: item.Spec.Names.Singular,
					Namespaced:   item.Spec.Scope == "Namespaced",
					Kind:         item.Spec.Names.Kind,
					Verbs:        metav1.Verbs{"delete", "deletecollection", "get", "list", "patch", "create", "update", "watch"},
					ShortNames:   item.Spec.Names.ShortNames,
					Categories:   item.Spec.Names.Categories,
				})
			}
		}
		continueToken = list.Metadata.Continue
		if continueToken == "" {
			return groupVersions, resources
		}
	}
}

func (c *fallbackDiscoveryClient) apiServiceGroupVersions(path string) []schema.GroupVersion {
	var result []schema.GroupVersion
	continueToken := ""
	for {
		data, err := c.getList(path, continueToken)
		if err != nil {
			return result
		}
		list := apiServiceList{}
		if err := json.Unmarshal(data, &list); err != nil {
			return result
		}
		for _, item := range list.Items {
			if item.Spec.Group != "" && item.Spec.Version != "" {
				result = append(result, schema.GroupVersion{Group: item.Spec.Group, Version: item.Spec.Version})
			}
		}
		continueToken = list.Metadata.Continue
		if continueToken == "" {
			return result
		}
	}
}

func (c *fallbackDiscoveryClient) getList(path, continueToken string) ([]byte, error) {
	request := c.RESTClient().Get().
		AbsPath(path).
		Param("limit", "500").
		SetHeader("Accept", discovery.AcceptV1)
	if continueToken != "" {
		request.Param("continue", continueToken)
	}
	return request.Do(context.TODO()).Raw()
}

func (c *fallbackDiscoveryClient) probeGroupVersions(groupVersions []schema.GroupVersion, knownResources map[string]*metav1.APIResourceList) map[string]*metav1.APIResourceList {
	resources := make([]*metav1.APIResourceList, len(groupVersions))
	for index, gv := range groupVersions {
		resources[index] = knownResources[gv.String()]
	}

	var wg sync.WaitGroup
	for index, gv := range groupVersions {
		if resources[index] != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			list, err := c.ServerResourcesForGroupVersion(gv.String())
			if err == nil {
				resources[index] = list
			}
		}()
	}
	wg.Wait()

	result := make(map[string]*metav1.APIResourceList, len(resources))
	for index, list := range resources {
		if list != nil {
			result[groupVersions[index].String()] = list
		}
	}
	return result
}

func groupsFromResources(groupVersions []schema.GroupVersion, resources map[string]*metav1.APIResourceList) *metav1.APIGroupList {
	result := &metav1.APIGroupList{
		TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
	}
	groupIndexes := map[string]int{}
	for _, gv := range groupVersions {
		if resources[gv.String()] == nil {
			continue
		}
		version := metav1.GroupVersionForDiscovery{GroupVersion: gv.String(), Version: gv.Version}
		index, ok := groupIndexes[gv.Group]
		if !ok {
			index = len(result.Groups)
			groupIndexes[gv.Group] = index
			result.Groups = append(result.Groups, metav1.APIGroup{
				Name:             gv.Group,
				PreferredVersion: version,
			})
		}
		result.Groups[index].Versions = append(result.Groups[index].Versions, version)
	}
	return result
}

type customResourceDefinitionList struct {
	Metadata metav1.ListMeta            `json:"metadata"`
	Items    []customResourceDefinition `json:"items"`
}

type customResourceDefinition struct {
	Spec struct {
		Group string `json:"group"`
		Names struct {
			Plural     string   `json:"plural"`
			Singular   string   `json:"singular"`
			Kind       string   `json:"kind"`
			ShortNames []string `json:"shortNames"`
			Categories []string `json:"categories"`
		} `json:"names"`
		Scope    string `json:"scope"`
		Versions []struct {
			Name    string `json:"name"`
			Served  bool   `json:"served"`
			Storage bool   `json:"storage"`
		} `json:"versions"`
	} `json:"spec"`
}

type apiServiceList struct {
	Metadata metav1.ListMeta `json:"metadata"`
	Items    []struct {
		Spec struct {
			Group   string `json:"group"`
			Version string `json:"version"`
		} `json:"spec"`
	} `json:"items"`
}
