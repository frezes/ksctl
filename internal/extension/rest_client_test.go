package extension

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	types "k8s.io/apimachinery/pkg/types"
	kubesphererest "kubesphere.io/client-go/rest"
)

func newTestAPIClient(t *testing.T, handler http.Handler, mutate func(*kubesphererest.Config)) APIClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	config := &kubesphererest.Config{Host: server.URL}
	if mutate != nil {
		mutate(config)
	}
	rest, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(config)
	if err != nil {
		t.Fatalf("ForConfig() error = %v", err)
	}
	return NewRESTClient(rest)
}

func writeJSONResponse(t *testing.T, response http.ResponseWriter, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if _, err := io.WriteString(response, body); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func TestRESTClientUsesHostCatalogPathsAndSelectors(t *testing.T) {
	var requests []string
	client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		switch request.URL.Path {
		case "/apis/kubesphere.io/v1alpha1/extensions":
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionList","items":[]}`)
		case "/apis/kubesphere.io/v1alpha1/extensionversions":
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionVersionList","items":[]}`)
		case "/apis/kubesphere.io/v1alpha1/extensionversions/demo-1.2.1":
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionVersion","metadata":{"name":"demo-1.2.1"},"spec":{"version":"1.2.1"}}`)
		default:
			http.NotFound(response, request)
		}
	}), nil)

	if _, err := client.ListExtensions(context.Background(), "observability"); err != nil {
		t.Fatalf("ListExtensions() error = %v", err)
	}
	if _, err := client.ListExtensionVersions(context.Background(), "demo"); err != nil {
		t.Fatalf("ListExtensionVersions() error = %v", err)
	}
	if _, err := client.GetExtensionVersion(
		context.Background(),
		"demo-1.2.1",
	); err != nil {
		t.Fatalf("GetExtensionVersion() error = %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("requests = %v", requests)
	}
	first, err := url.ParseRequestURI(strings.TrimPrefix(requests[0], "GET "))
	if err != nil {
		t.Fatal(err)
	}
	if got := first.Query().Get("labelSelector"); got != "kubesphere.io/category=observability" {
		t.Fatalf("category selector = %q", got)
	}
	second, err := url.ParseRequestURI(strings.TrimPrefix(requests[1], "GET "))
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Query().Get("labelSelector"); got != "kubesphere.io/extension-ref=demo" {
		t.Fatalf("extension selector = %q", got)
	}
	for _, request := range requests {
		if strings.Contains(request, "/clusters/") {
			t.Fatalf("member-scoped request = %q", request)
		}
	}
}

func TestRESTClientInstallPlanCRUD(t *testing.T) {
	var createBody map[string]any
	var patchBody map[string]any
	var deleteBody map[string]any
	var patchContentType string
	var methods []string
	client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		methods = append(methods, request.Method+" "+request.URL.Path)
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans":
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlanList","items":[]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"8"},"spec":{"extension":{"name":"demo","version":"1.2.0"},"enabled":true,"upgradeStrategy":"Manual"}}`)
		case request.Method == http.MethodPost && request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans":
			if err := json.NewDecoder(request.Body).Decode(&createBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"9"},"spec":{"extension":{"name":"demo","version":"1.2.0"},"enabled":true,"upgradeStrategy":"Manual"}}`)
		case request.Method == http.MethodPatch && request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
			patchContentType = request.Header.Get("Content-Type")
			if err := json.NewDecoder(request.Body).Decode(&patchBody); err != nil {
				t.Errorf("decode patch body: %v", err)
			}
			writeJSONResponse(t, response, `{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"10"},"spec":{"extension":{"name":"demo","version":"1.2.1"},"enabled":true,"upgradeStrategy":"Manual"}}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
			if err := json.NewDecoder(request.Body).Decode(&deleteBody); err != nil {
				t.Errorf("decode delete body: %v", err)
			}
			writeJSONResponse(t, response, `{"apiVersion":"v1","kind":"Status","status":"Success","code":200}`)
		default:
			http.NotFound(response, request)
		}
	}), nil)

	if _, err := client.ListInstallPlans(context.Background()); err != nil {
		t.Fatalf("ListInstallPlans() error = %v", err)
	}
	if _, err := client.GetInstallPlan(context.Background(), "demo"); err != nil {
		t.Fatalf("GetInstallPlan() error = %v", err)
	}
	plan := InstallPlan{
		APIVersion: "kubesphere.io/v1alpha1",
		Kind:       "InstallPlan",
		Metadata:   ObjectMeta{Name: "demo"},
		Spec: InstallPlanSpec{
			Extension:       ExtensionRef{Name: "demo", Version: "1.2.0"},
			Enabled:         true,
			UpgradeStrategy: "Manual",
			Config:          "key: value\n",
			ClusterScheduling: &ClusterScheduling{
				Placement: &Placement{Clusters: []string{"member-a"}},
				Overrides: map[string]string{"member-a": "key: override\n"},
			},
		},
	}
	if _, err := client.CreateInstallPlan(context.Background(), plan); err != nil {
		t.Fatalf("CreateInstallPlan() error = %v", err)
	}
	patch := []byte(`{"metadata":{"resourceVersion":"9"},"spec":{"extension":{"version":"1.2.1"}}}`)
	if _, err := client.PatchInstallPlan(context.Background(), "demo", patch); err != nil {
		t.Fatalf("PatchInstallPlan() error = %v", err)
	}
	if err := client.DeleteInstallPlan(
		context.Background(),
		"demo",
		"10",
	); err != nil {
		t.Fatalf("DeleteInstallPlan() error = %v", err)
	}

	if createBody["apiVersion"] != "kubesphere.io/v1alpha1" || createBody["kind"] != "InstallPlan" {
		t.Fatalf("create body = %#v", createBody)
	}
	spec := createBody["spec"].(map[string]any)
	if spec["enabled"] != true || spec["upgradeStrategy"] != "Manual" {
		t.Fatalf("create spec = %#v", spec)
	}
	if patchContentType != string(types.MergePatchType) {
		t.Fatalf("patch Content-Type = %q, want %q", patchContentType, types.MergePatchType)
	}
	if patchBody["metadata"].(map[string]any)["resourceVersion"] != "9" {
		t.Fatalf("patch body = %#v", patchBody)
	}
	if deleteBody["preconditions"].(map[string]any)["resourceVersion"] != "10" {
		t.Fatalf("delete body = %#v", deleteBody)
	}
	if len(methods) != 5 {
		t.Fatalf("methods = %v", methods)
	}
}

func TestRESTClientNeverUsesMemberClusterForExecutorWorkloads(t *testing.T) {
	var paths []string
	client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.RequestURI())
		switch request.URL.Path {
		case "/api/v1/namespaces/demo-system":
			writeJSONResponse(t, response, `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"demo-system"}}`)
		case "/apis/batch/v1/namespaces/demo-system/jobs/install-demo":
			writeJSONResponse(t, response, `{"apiVersion":"batch/v1","kind":"Job","metadata":{"name":"install-demo","namespace":"demo-system"}}`)
		case "/api/v1/namespaces/demo-system/pods":
			writeJSONResponse(t, response, `{"apiVersion":"v1","kind":"PodList","items":[]}`)
		default:
			http.NotFound(response, request)
		}
	}), nil)

	if _, err := client.GetNamespace(context.Background(), "demo-system"); err != nil {
		t.Fatalf("GetNamespace() error = %v", err)
	}
	if _, err := client.GetJob(context.Background(), "demo-system", "install-demo"); err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if _, err := client.ListPodsForJob(context.Background(), "demo-system", "install-demo"); err != nil {
		t.Fatalf("ListPodsForJob() error = %v", err)
	}
	want := []string{
		"/api/v1/namespaces/demo-system",
		"/apis/batch/v1/namespaces/demo-system/jobs/install-demo",
		"/api/v1/namespaces/demo-system/pods?labelSelector=job-name%3Dinstall-demo",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v", paths)
	}
	for _, path := range paths {
		if strings.Contains(path, "/clusters/") {
			t.Fatalf("member-scoped workload path = %q", path)
		}
	}
}

func TestRESTClientPreservesKubernetesStatusErrors(t *testing.T) {
	client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusNotFound)
		writeJSONResponse(t, response, `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","message":"extensions.kubesphere.io \"missing\" not found","code":404}`)
	}), nil)

	_, err := client.GetExtension(context.Background(), "missing")
	if !apierrors.IsNotFound(err) {
		t.Fatalf("GetExtension() error = %v, want recognizable NotFound", err)
	}
}

func TestRESTClientPreservesForbiddenAndConflictStatusErrors(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		client := newTestAPIClient(t, http.HandlerFunc(func(
			response http.ResponseWriter,
			_ *http.Request,
		) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusForbidden)
			writeJSONResponse(
				t,
				response,
				`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`,
			)
		}), nil)

		_, err := client.GetExtension(context.Background(), "demo")
		if !apierrors.IsForbidden(err) {
			t.Fatalf("GetExtension() error = %v, want recognizable Forbidden", err)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		client := newTestAPIClient(t, http.HandlerFunc(func(
			response http.ResponseWriter,
			_ *http.Request,
		) {
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusConflict)
			writeJSONResponse(
				t,
				response,
				`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"Conflict","message":"resourceVersion conflict","code":409}`,
			)
		}), nil)

		_, err := client.PatchInstallPlan(
			context.Background(),
			"demo",
			[]byte(`{"metadata":{"resourceVersion":"old"}}`),
		)
		if !apierrors.IsConflict(err) {
			t.Fatalf("PatchInstallPlan() error = %v, want recognizable Conflict", err)
		}
	})
}

func TestRESTClientRejectsMalformedAndMismatchedResponses(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSONResponse(t, response, `{`)
		}), nil)
		if _, err := client.GetExtension(context.Background(), "demo"); err == nil {
			t.Fatal("GetExtension() error = nil, want malformed response error")
		}
	})
	t.Run("mismatched name", func(t *testing.T) {
		client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSONResponse(t, response, `{"metadata":{"name":"other"}}`)
		}), nil)
		_, err := client.GetExtension(context.Background(), "demo")
		if err == nil || !strings.Contains(err.Error(), "other") {
			t.Fatalf("GetExtension() error = %v, want mismatched name", err)
		}
	})
}

func TestValidatePathNameRejectsEmptyAndWhitespaceOnlyNames(t *testing.T) {
	for _, name := range []string{"", " ", "\t"} {
		if err := validatePathName("extension", name); err == nil ||
			!strings.Contains(err.Error(), "non-empty") {
			t.Fatalf("validatePathName(%q) error = %v", name, err)
		}
	}
}

func TestRESTClientPropagatesCredentialsUserAgentAndTimeout(t *testing.T) {
	started := make(chan struct{})
	client := newTestAPIClient(t, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("User-Agent") != "ksctl/test" {
			t.Errorf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		close(started)
		<-request.Context().Done()
	}), func(config *kubesphererest.Config) {
		config.BearerToken = "secret"
		config.UserAgent = "ksctl/test"
		config.Timeout = 20 * time.Millisecond
	})

	_, err := client.ListExtensions(context.Background(), "")
	if err == nil {
		t.Fatal("ListExtensions() error = nil, want timeout")
	}
	select {
	case <-started:
	default:
		t.Fatal("handler was not reached")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("ListExtensions() error = %v, want timeout", err)
	}
}
