package tenant

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPrintResponseTables(t *testing.T) {
	now := time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		resource Resource
		objects  []map[string]any
		want     []string
	}{
		{
			name:     "workspace template",
			resource: ResourceWorkspace,
			objects: []map[string]any{{
				"metadata": map[string]any{
					"name":              "platform",
					"creationTimestamp": "2026-07-15T14:00:00Z",
				},
				"spec": map[string]any{
					"placement": map[string]any{"clusters": []any{
						map[string]any{"name": "host"},
						map[string]any{"name": "member"},
					}},
					"template": map[string]any{
						"spec": map[string]any{"manager": "admin"},
					},
				},
			}},
			want: []string{
				"NAME", "CLUSTERS", "ADMINISTRATOR", "AGE",
				"platform", "host,member", "admin", "8d",
			},
		},
		{
			name:     "namespace",
			resource: ResourceNamespace,
			objects: []map[string]any{{
				"metadata": map[string]any{
					"name":              "demo",
					"creationTimestamp": "2026-07-03T14:00:00Z",
				},
				"status": map[string]any{"phase": "Active"},
			}},
			want: []string{"NAME", "STATUS", "AGE", "demo", "Active", "20d"},
		},
		{
			name:     "clusters",
			resource: ResourceCluster,
			objects: []map[string]any{
				{
					"metadata": map[string]any{"name": "host"},
					"spec":     map[string]any{"provider": "kubesphere"},
					"status":   map[string]any{"kubernetesVersion": "v1.33.1"},
				},
				{
					"metadata": map[string]any{"name": "member"},
					"spec":     map[string]any{},
					"status":   map[string]any{"kubernetesVersion": "v1.23.17"},
				},
			},
			want: []string{
				"NAME", "PROVIDER", "VERSION",
				"host", "kubesphere", "v1.33.1",
				"member", "v1.23.17",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := new(bytes.Buffer)
			err := printResponse(out, "table", test.resource, Response{
				Objects: test.objects,
				IsList:  true,
			}, now)
			if err != nil {
				t.Fatalf("printResponse() error = %v", err)
			}
			for _, want := range test.want {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output missing %q:\n%s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "<none>") {
				t.Fatalf("output contains non-kubectl placeholder:\n%s", out.String())
			}
		})
	}
}

func TestPrintResponseUnknownAge(t *testing.T) {
	out := new(bytes.Buffer)
	err := printResponse(out, "table", ResourceNamespace, Response{
		Objects: []map[string]any{{
			"metadata": map[string]any{"name": "demo"},
			"status":   map[string]any{"phase": "Active"},
		}},
	}, time.Now())
	if err != nil {
		t.Fatalf("printResponse() error = %v", err)
	}
	if !strings.Contains(out.String(), "<unknown>") {
		t.Fatalf("output = %q, want unknown age", out.String())
	}
}

func TestPrintResponseEmptyList(t *testing.T) {
	out := new(bytes.Buffer)
	err := printResponse(out, "table", ResourceWorkspace, Response{
		Objects: []map[string]any{},
		IsList:  true,
	}, time.Now())
	if err != nil {
		t.Fatalf("printResponse() error = %v", err)
	}
	if out.String() != "No resources found\n" {
		t.Fatalf("output = %q, want No resources found", out.String())
	}
}

func TestPrintResponseStructuredOutputPreservesEnvelope(t *testing.T) {
	raw := []byte(`{"items":[{"metadata":{"name":"demo"}}],"total_count":1}`)
	response := Response{Raw: raw, IsList: true}

	jsonOut := new(bytes.Buffer)
	if err := printResponse(jsonOut, "json", ResourceWorkspace, response, time.Now()); err != nil {
		t.Fatalf("JSON error = %v", err)
	}
	var gotJSON map[string]any
	if err := json.Unmarshal(jsonOut.Bytes(), &gotJSON); err != nil {
		t.Fatalf("JSON output is invalid: %v", err)
	}
	if gotJSON["total_count"] != float64(1) {
		t.Fatalf("JSON output = %#v, want total_count", gotJSON)
	}

	yamlOut := new(bytes.Buffer)
	if err := printResponse(yamlOut, "yaml", ResourceWorkspace, response, time.Now()); err != nil {
		t.Fatalf("YAML error = %v", err)
	}
	for _, want := range []string{"items:", "name: demo", "total_count: 1"} {
		if !strings.Contains(yamlOut.String(), want) {
			t.Fatalf("YAML output missing %q:\n%s", want, yamlOut.String())
		}
	}
}

func TestPrintResponseRejectsUnknownFormat(t *testing.T) {
	err := printResponse(new(bytes.Buffer), "wide", ResourceWorkspace, Response{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), `unsupported output format "wide"`) {
		t.Fatalf("error = %v, want unsupported output format", err)
	}
}

func TestPrintResponseReturnsWriteError(t *testing.T) {
	err := printResponse(errorWriter{}, "json", ResourceWorkspace,
		Response{Raw: []byte(`{"items":[]}`)}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "write tenant JSON output") {
		t.Fatalf("error = %v, want write context", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
