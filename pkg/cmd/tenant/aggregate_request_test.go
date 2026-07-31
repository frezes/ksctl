package tenant

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseResourceEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		hostPath      string
		requestPath   string
		wantGVR       schema.GroupVersionResource
		wantNamespace string
		wantPath      string
		wantOK        bool
	}{
		{
			name:        "prefixed core collection",
			hostPath:    "/proxy/clusters/member",
			requestPath: "/proxy/clusters/member/api/v1/pods",
			wantGVR: schema.GroupVersionResource{
				Version:  "v1",
				Resource: "pods",
			},
			wantPath: "/proxy/clusters/member/api/v1/namespaces/demo/pods",
			wantOK:   true,
		},
		{
			name:        "group collection",
			requestPath: "/apis/apps/v1/deployments",
			wantGVR: schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
			wantPath: "/apis/apps/v1/namespaces/demo/deployments",
			wantOK:   true,
		},
		{
			name:          "namespaced core collection",
			requestPath:   "/api/v1/namespaces/team-a/pods",
			wantGVR:       schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			wantNamespace: "team-a",
			wantPath:      "/api/v1/namespaces/demo/pods",
			wantOK:        true,
		},
		{
			name:        "named resource",
			requestPath: "/apis/apps/v1/deployments/web",
		},
		{
			name:        "discovery root",
			requestPath: "/apis",
		},
		{
			name:        "host prefix mismatch",
			hostPath:    "/proxy/clusters/member",
			requestPath: "/api/v1/pods",
		},
		{
			name:        "traversal segment",
			requestPath: "/api/v1/../pods",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseResourceEndpoint(test.hostPath, test.requestPath)
			if ok != test.wantOK {
				t.Fatalf("parseResourceEndpoint() ok = %v, want %v: %#v", ok, test.wantOK, got)
			}
			if !test.wantOK {
				return
			}
			if !reflect.DeepEqual(got.GVR, test.wantGVR) {
				t.Fatalf("GVR = %#v, want %#v", got.GVR, test.wantGVR)
			}
			if got.Namespace != test.wantNamespace {
				t.Fatalf("Namespace = %q, want %q", got.Namespace, test.wantNamespace)
			}
			if path := got.pathForNamespace("demo"); path != test.wantPath {
				t.Fatalf("pathForNamespace() = %q, want %q", path, test.wantPath)
			}
		})
	}
}
