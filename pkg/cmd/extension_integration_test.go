package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
)

type extensionIntegrationServer struct {
	mu       sync.Mutex
	handler  http.Handler
	requests []string
}

func (s *extensionIntegrationServer) ServeHTTP(
	response http.ResponseWriter,
	request *http.Request,
) {
	s.mu.Lock()
	s.requests = append(
		s.requests,
		request.Method+" "+request.URL.RequestURI(),
	)
	handler := s.handler
	s.mu.Unlock()
	if handler == nil {
		http.Error(response, "test handler is not configured", http.StatusInternalServerError)
		return
	}
	handler.ServeHTTP(response, request)
}

func (s *extensionIntegrationServer) use(handler http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = handler
	s.requests = nil
}

func (s *extensionIntegrationServer) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func writeIntegrationJSON(
	t testing.TB,
	response http.ResponseWriter,
	status int,
	document string,
) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, document); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func integrationNotFound(
	t testing.TB,
	response http.ResponseWriter,
	resource string,
	name string,
) {
	t.Helper()
	writeIntegrationJSON(
		t,
		response,
		http.StatusNotFound,
		`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","message":"`+
			resource+` \"`+name+`\" not found","code":404}`,
	)
}

func executeIntegrationRoot(
	t *testing.T,
	alternateEntrypoint bool,
	args ...string,
) (string, string, error) {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer
	streams := IOStreams{
		In:     strings.NewReader(""),
		Out:    &out,
		ErrOut: &errOut,
	}
	var command interface {
		SetArgs([]string)
		Execute() error
	}
	if alternateEntrypoint {
		command = newTestEntrypointCommand(
			t,
			streams,
			VersionInfo{Version: "test"},
		)
	} else {
		command = NewRootCommand(
			streams,
			VersionInfo{Version: "test"},
		)
	}
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), errOut.String(), err
}

func explicitConnectionArgs(endpoint string, tail ...string) []string {
	args := []string{
		"--endpoint",
		endpoint,
		"--token",
		"secret",
	}
	return append(args, tail...)
}

func TestExtensionIntegration(t *testing.T) {
	dispatcher := &extensionIntegrationServer{}
	server := httptest.NewServer(dispatcher)
	defer server.Close()

	t.Run("list JSON uses host and preserves unknown fields", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			switch request.URL.Path {
			case "/apis/kubesphere.io/v1alpha1/extensions":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionList","futureList":true,"items":[{"metadata":{"name":"demo"},"futureItem":"kept"}]}`,
				)
			case "/apis/kubesphere.io/v1alpha1/installplans":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlanList","items":[]}`,
				)
			default:
				http.NotFound(response, request)
			}
		}))

		out, _, err := executeIntegrationRoot(
			t,
			false,
			explicitConnectionArgs(
				server.URL,
				"extension",
				"list",
				"--output",
				"json",
			)...,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		for _, want := range []string{
			`"futureList":true`,
			`"futureItem":"kept"`,
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("output = %q, want %q", out, want)
			}
		}
		assertHostOnlyExtensionRequests(t, dispatcher.paths())
	})

	t.Run("invalid context default cluster is ignored", func(t *testing.T) {
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		t.Setenv("KSCTL_CONFIG", configPath)
		cfg := config.New()
		cfg.CurrentContext = "local"
		cfg.Fleets["local"] = config.Fleet{
			Host: server.URL,
			Users: map[string]config.User{
				"admin": {BearerToken: "secret"},
			},
		}
		cfg.Contexts["local"] = config.Context{
			Fleet:          "local",
			User:           "admin",
			DefaultCluster: "invalid/member",
		}
		if err := config.Save(configPath, cfg); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			switch request.URL.Path {
			case "/apis/kubesphere.io/v1alpha1/extensions":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionList","items":[]}`,
				)
			case "/apis/kubesphere.io/v1alpha1/installplans":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlanList","items":[]}`,
				)
			default:
				http.NotFound(response, request)
			}
		}))

		_, _, err := executeIntegrationRoot(
			t,
			false,
			"extension",
			"list",
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		assertHostOnlyExtensionRequests(t, dispatcher.paths())
	})

	t.Run("alternate entrypoint status uses host", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			if request.URL.Path != "/apis/kubesphere.io/v1alpha1/installplans/demo" {
				http.NotFound(response, request)
				return
			}
			writeIntegrationJSON(
				t,
				response,
				http.StatusOK,
				`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo"},"spec":{"extension":{"name":"demo","version":"1.2.1"},"enabled":true},"status":{"state":"Installed","version":"1.2.1"}}`,
			)
		}))

		out, _, err := executeIntegrationRoot(
			t,
			true,
			explicitConnectionArgs(
				server.URL,
				"extension",
				"status",
				"demo",
			)...,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		fields := strings.Fields(out)
		if len(fields) < 12 ||
			!reflect.DeepEqual(
				fields[6:12],
				[]string{
					"demo",
					"1.2.1",
					"true",
					"Installed",
					"<none>",
					"<none>",
				},
			) {
			t.Fatalf("output fields = %v", fields)
		}
		assertHostOnlyExtensionRequests(t, dispatcher.paths())
	})

	t.Run("install creates exact enabled Manual plan", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		var created map[string]any
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			switch {
			case request.Method == http.MethodGet &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/extensions/demo":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"Extension","metadata":{"name":"demo"}}`,
				)
			case request.Method == http.MethodGet &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/extensionversions/demo-v1.2.1-build.1":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionVersion","metadata":{"name":"demo-v1.2.1-build.1"},"spec":{"version":"v1.2.1-build.1","installationMode":"HostOnly"}}`,
				)
			case request.Method == http.MethodGet &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
				integrationNotFound(t, response, "installplans", "demo")
			case request.Method == http.MethodPost &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans":
				if err := json.NewDecoder(request.Body).Decode(&created); err != nil {
					t.Errorf("decode create: %v", err)
				}
				writeIntegrationJSON(
					t,
					response,
					http.StatusCreated,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"1"},"spec":{"extension":{"name":"demo","version":"v1.2.1-build.1"},"enabled":true,"upgradeStrategy":"Manual"}}`,
				)
			default:
				http.NotFound(response, request)
			}
		}))

		out, _, err := executeIntegrationRoot(
			t,
			false,
			explicitConnectionArgs(
				server.URL,
				"extension",
				"install",
				"demo",
				"--version",
				"v1.2.1-build.1",
			)...,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if out != "extension/demo install requested\n" {
			t.Fatalf("output = %q", out)
		}
		spec := created["spec"].(map[string]any)
		if spec["enabled"] != true ||
			spec["upgradeStrategy"] != "Manual" ||
			spec["extension"].(map[string]any)["version"] != "v1.2.1-build.1" {
			t.Fatalf("created plan = %#v", created)
		}
		assertHostOnlyExtensionRequests(t, dispatcher.paths())
	})

	t.Run("upgrade sends resourceVersion guarded minimal patch", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		var patch map[string]any
		var contentType string
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			switch {
			case request.Method == http.MethodGet &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"7"},"spec":{"extension":{"name":"demo","version":"1.2.0"},"enabled":true,"upgradeStrategy":"Manual"},"status":{"state":"Installed","version":"1.2.0"}}`,
				)
			case request.Method == http.MethodGet &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/extensionversions/demo-1.2.1":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionVersion","metadata":{"name":"demo-1.2.1"},"spec":{"version":"1.2.1","installationMode":"HostOnly"}}`,
				)
			case request.Method == http.MethodPatch &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
				contentType = request.Header.Get("Content-Type")
				if err := json.NewDecoder(request.Body).Decode(&patch); err != nil {
					t.Errorf("decode patch: %v", err)
				}
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"8"},"spec":{"extension":{"name":"demo","version":"1.2.1"},"enabled":true,"upgradeStrategy":"Manual"},"status":{"state":"Installed","version":"1.2.0"}}`,
				)
			default:
				http.NotFound(response, request)
			}
		}))

		out, _, err := executeIntegrationRoot(
			t,
			false,
			explicitConnectionArgs(
				server.URL,
				"extension",
				"upgrade",
				"demo",
				"--version",
				"1.2.1",
			)...,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if out != "extension/demo upgrade requested\n" {
			t.Fatalf("output = %q", out)
		}
		wantPatch := map[string]any{
			"metadata": map[string]any{"resourceVersion": "7"},
			"spec": map[string]any{
				"extension":       map[string]any{"version": "1.2.1"},
				"upgradeStrategy": "Manual",
			},
		}
		if !reflect.DeepEqual(patch, wantPatch) {
			t.Fatalf("patch = %#v, want %#v", patch, wantPatch)
		}
		if !strings.Contains(contentType, "merge-patch+json") {
			t.Fatalf("Content-Type = %q", contentType)
		}
		assertHostOnlyExtensionRequests(t, dispatcher.paths())
	})

	t.Run("uninstall wait polls until NotFound", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		gets := 0
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			switch {
			case request.Method == http.MethodGet &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
				gets++
				if gets >= 3 {
					integrationNotFound(t, response, "installplans", "demo")
					return
				}
				state := "Installed"
				if gets == 2 {
					state = "Uninstalled"
				}
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo","resourceVersion":"7"},"spec":{"extension":{"name":"demo","version":"1.2.1"},"enabled":true},"status":{"state":"`+
						state+`","version":"1.2.1"}}`,
				)
			case request.Method == http.MethodDelete &&
				request.URL.Path == "/apis/kubesphere.io/v1alpha1/installplans/demo":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"v1","kind":"Status","status":"Success","code":200}`,
				)
			default:
				http.NotFound(response, request)
			}
		}))

		out, _, err := executeIntegrationRoot(
			t,
			false,
			explicitConnectionArgs(
				server.URL,
				"extension",
				"uninstall",
				"demo",
				"--wait",
				"--wait-timeout",
				"5s",
			)...,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if out != "extension/demo uninstalled\n" {
			t.Fatalf("output = %q", out)
		}
		if gets != 3 {
			t.Fatalf("GET count = %d, want 3", gets)
		}
		assertHostOnlyExtensionRequests(t, dispatcher.paths())
	})

	t.Run("explicit root scope performs no requests", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		for _, args := range [][]string{
			explicitConnectionArgs(
				server.URL,
				"--cluster",
				"member-a",
				"extension",
				"list",
			),
			explicitConnectionArgs(
				server.URL,
				"--namespace",
				"demo",
				"extension",
				"list",
			),
		} {
			dispatcher.use(http.HandlerFunc(func(
				http.ResponseWriter,
				*http.Request,
			) {
				t.Error("unexpected HTTP request")
			}))
			if _, _, err := executeIntegrationRoot(t, false, args...); err == nil {
				t.Fatalf("Execute(%v) error = nil", args)
			}
			if len(dispatcher.paths()) != 0 {
				t.Fatalf("requests = %v", dispatcher.paths())
			}
		}
	})

	t.Run("target cluster diagnosis uses host executor paths", func(t *testing.T) {
		t.Setenv("KSCTL_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		t.Setenv("KS_ENDPOINT", "")
		t.Setenv("KS_TOKEN", "")
		dispatcher.use(http.HandlerFunc(func(
			response http.ResponseWriter,
			request *http.Request,
		) {
			switch request.URL.Path {
			case "/apis/kubesphere.io/v1alpha1/extensions/demo":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"Extension","metadata":{"name":"demo"},"status":{"state":"Installed"}}`,
				)
			case "/apis/kubesphere.io/v1alpha1/installplans/demo":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"InstallPlan","metadata":{"name":"demo"},"spec":{"extension":{"name":"demo","version":"1.2.1"},"enabled":true},"status":{"state":"Installed","version":"1.2.1","clusterSchedulingStatuses":{"member-a":{"state":"Installed","version":"1.2.1","targetNamespace":"member-executor","jobName":"member-install"}}}}`,
				)
			case "/apis/kubesphere.io/v1alpha1/extensionversions/demo-1.2.1":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionVersion","metadata":{"name":"demo-1.2.1"},"spec":{"version":"1.2.1","installationMode":"Multicluster"}}`,
				)
			case "/api/v1/namespaces/member-executor":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"member-executor"}}`,
				)
			case "/apis/batch/v1/namespaces/member-executor/jobs/member-install":
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"batch/v1","kind":"Job","metadata":{"name":"member-install","namespace":"member-executor"},"status":{"succeeded":1,"completionTime":"2026-07-28T00:00:00Z","conditions":[{"type":"Complete","status":"True"}]}}`,
				)
			case "/api/v1/namespaces/member-executor/pods":
				if got := request.URL.Query().Get("labelSelector"); got != "job-name=member-install" {
					t.Errorf("labelSelector = %q", got)
				}
				writeIntegrationJSON(
					t,
					response,
					http.StatusOK,
					`{"apiVersion":"v1","kind":"PodList","items":[]}`,
				)
			default:
				http.NotFound(response, request)
			}
		}))

		out, _, err := executeIntegrationRoot(
			t,
			false,
			explicitConnectionArgs(
				server.URL,
				"extension",
				"diagnose",
				"demo",
				"--target-cluster",
				"member-a",
				"--verbose",
			)...,
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		for _, want := range []string{
			"cluster/member-a",
			"member-executor",
			"member-install",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("output = %q, want %q", out, want)
			}
		}
		paths := dispatcher.paths()
		assertHostOnlyExtensionRequests(t, paths)
		wantWorkloadPaths := []string{
			"GET /api/v1/namespaces/member-executor",
			"GET /apis/batch/v1/namespaces/member-executor/jobs/member-install",
			"GET /api/v1/namespaces/member-executor/pods?labelSelector=" +
				url.QueryEscape("job-name=member-install"),
		}
		for _, want := range wantWorkloadPaths {
			if !slicesContains(paths, want) {
				t.Fatalf("requests = %v, want %q", paths, want)
			}
		}
	})
}

func assertHostOnlyExtensionRequests(t testing.TB, requests []string) {
	t.Helper()
	for _, request := range requests {
		if strings.Contains(request, "/clusters/") {
			t.Fatalf("member-scoped extension request = %q", request)
		}
	}
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
