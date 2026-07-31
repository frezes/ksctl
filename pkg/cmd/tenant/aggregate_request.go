package tenant

import (
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type resourceEndpoint struct {
	GVR       schema.GroupVersionResource
	Namespace string

	apiBase  string
	resource string
}

func parseResourceEndpoint(hostPath, requestPath string) (resourceEndpoint, bool) {
	hostPath = strings.TrimSuffix(hostPath, "/")
	if hostPath == "/" {
		hostPath = ""
	}
	if hostPath != "" {
		if requestPath == hostPath || !strings.HasPrefix(requestPath, hostPath+"/") {
			return resourceEndpoint{}, false
		}
		requestPath = strings.TrimPrefix(requestPath, hostPath)
	}
	parts := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return resourceEndpoint{}, false
		}
	}

	var endpoint resourceEndpoint
	var remaining []string
	switch {
	case len(parts) >= 3 && parts[0] == "api":
		endpoint.GVR.Version = parts[1]
		endpoint.apiBase = hostPath + "/api/" + parts[1]
		remaining = parts[2:]
	case len(parts) >= 4 && parts[0] == "apis":
		endpoint.GVR.Group = parts[1]
		endpoint.GVR.Version = parts[2]
		endpoint.apiBase = hostPath + "/apis/" + parts[1] + "/" + parts[2]
		remaining = parts[3:]
	default:
		return resourceEndpoint{}, false
	}

	switch {
	case len(remaining) == 1:
		endpoint.resource = remaining[0]
	case len(remaining) == 3 && remaining[0] == "namespaces":
		endpoint.Namespace = remaining[1]
		endpoint.resource = remaining[2]
	default:
		return resourceEndpoint{}, false
	}
	if endpoint.GVR.Version == "" || endpoint.resource == "" {
		return resourceEndpoint{}, false
	}
	endpoint.GVR.Resource = endpoint.resource
	return endpoint, true
}

func (e resourceEndpoint) pathForNamespace(namespace string) string {
	return e.apiBase +
		"/namespaces/" + url.PathEscape(namespace) +
		"/" + e.resource
}
