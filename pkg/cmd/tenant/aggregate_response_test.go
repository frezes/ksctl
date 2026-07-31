package tenant

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMergeDocumentsCombinesListsAndClearsSharedMetadata(t *testing.T) {
	mapping := podRESTMapping()
	documents := []scopedDocument{
		{
			Namespace: "team-b",
			Body: []byte(`{
				"apiVersion":"v1",
				"kind":"PodList",
				"metadata":{"resourceVersion":"20","continue":"b-next"},
				"items":[{"metadata":{"namespace":"team-b","name":"pod-b"}}]
			}`),
		},
		{
			Namespace: "team-a",
			Body: []byte(`{
				"apiVersion":"v1",
				"kind":"PodList",
				"metadata":{"resourceVersion":"10","continue":"a-next"},
				"items":[{"metadata":{"namespace":"team-a","name":"pod-a"}}]
			}`),
		},
	}

	body, err := mergeDocuments(documents, mapping, false)
	if err != nil {
		t.Fatalf("mergeDocuments() error = %v", err)
	}
	var list unstructured.UnstructuredList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode merged list: %v", err)
	}
	if list.GetAPIVersion() != "v1" || list.GetKind() != "PodList" {
		t.Fatalf("merged type = %s %s", list.GetAPIVersion(), list.GetKind())
	}
	if list.GetContinue() != "" || list.GetResourceVersion() != "" {
		t.Fatalf(
			"aggregate metadata = continue %q resourceVersion %q",
			list.GetContinue(),
			list.GetResourceVersion(),
		)
	}
	if len(list.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(list.Items))
	}
	got := []string{
		list.Items[0].GetNamespace() + "/" + list.Items[0].GetName(),
		list.Items[1].GetNamespace() + "/" + list.Items[1].GetName(),
	}
	want := []string{"team-b/pod-b", "team-a/pod-a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("items = %v, want %v", got, want)
	}
}

func TestMergeDocumentsCombinesTablesAndInjectsNamespaceMetadata(t *testing.T) {
	columns := `[{"name":"Name","type":"string","format":"name"}]`
	documents := []scopedDocument{
		{
			Namespace: "team-b",
			Body: []byte(`{
				"apiVersion":"meta.k8s.io/v1",
				"kind":"Table",
				"metadata":{"resourceVersion":"20"},
				"columnDefinitions":` + columns + `,
				"rows":[{"cells":["pod-b"]}]
			}`),
		},
		{
			Namespace: "team-a",
			Body: []byte(`{
				"apiVersion":"meta.k8s.io/v1",
				"kind":"Table",
				"metadata":{"resourceVersion":"10"},
				"columnDefinitions":` + columns + `,
				"rows":[{"cells":["pod-a"]}]
			}`),
		},
	}

	body, err := mergeDocuments(documents, podRESTMapping(), true)
	if err != nil {
		t.Fatalf("mergeDocuments() error = %v", err)
	}
	var table map[string]any
	if err := json.Unmarshal(body, &table); err != nil {
		t.Fatalf("decode merged table: %v", err)
	}
	rows, ok := table["rows"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("rows = %#v, want two rows", table["rows"])
	}
	for index, want := range []string{"team-b", "team-a"} {
		row := rows[index].(map[string]any)
		object := row["object"].(map[string]any)
		metadata := object["metadata"].(map[string]any)
		if got := metadata["namespace"]; got != want {
			t.Fatalf("row %d namespace = %#v, want %q", index, got, want)
		}
	}
	metadata := table["metadata"].(map[string]any)
	if metadata["resourceVersion"] != "" {
		t.Fatalf("resourceVersion = %#v, want empty", metadata["resourceVersion"])
	}
}

func TestMergeDocumentsRejectsMismatchedTypeOrColumns(t *testing.T) {
	tests := []struct {
		name      string
		documents []scopedDocument
		table     bool
		want      string
	}{
		{
			name: "list kind",
			documents: []scopedDocument{
				{Namespace: "team-a", Body: []byte(`{"apiVersion":"v1","kind":"PodList","items":[]}`)},
				{Namespace: "team-b", Body: []byte(`{"apiVersion":"v1","kind":"ServiceList","items":[]}`)},
			},
			want: "does not match",
		},
		{
			name: "table columns",
			documents: []scopedDocument{
				{Namespace: "team-a", Body: []byte(`{"apiVersion":"meta.k8s.io/v1","kind":"Table","columnDefinitions":[{"name":"Name","type":"string"}],"rows":[]}`)},
				{Namespace: "team-b", Body: []byte(`{"apiVersion":"meta.k8s.io/v1","kind":"Table","columnDefinitions":[{"name":"Pod","type":"string"}],"rows":[]}`)},
			},
			table: true,
			want:  "column definitions",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := mergeDocuments(test.documents, podRESTMapping(), test.table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("mergeDocuments() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMergeDocumentsBuildsEmptyListAndTable(t *testing.T) {
	tests := []struct {
		name       string
		table      bool
		wantAPI    string
		wantKind   string
		wantMember string
	}{
		{name: "list", wantAPI: "v1", wantKind: "PodList", wantMember: "items"},
		{name: "table", table: true, wantAPI: "meta.k8s.io/v1", wantKind: "Table", wantMember: "rows"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := mergeDocuments(nil, podRESTMapping(), test.table)
			if err != nil {
				t.Fatalf("mergeDocuments() error = %v", err)
			}
			var object map[string]any
			if err := json.Unmarshal(body, &object); err != nil {
				t.Fatalf("decode empty response: %v", err)
			}
			if object["apiVersion"] != test.wantAPI || object["kind"] != test.wantKind {
				t.Fatalf("type = %v %v, want %s %s", object["apiVersion"], object["kind"], test.wantAPI, test.wantKind)
			}
			member, ok := object[test.wantMember].([]any)
			if !ok || len(member) != 0 {
				t.Fatalf("%s = %#v, want empty array", test.wantMember, object[test.wantMember])
			}
		})
	}
}

func TestMergeDocumentsRejectsMalformedOrNonListJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed", body: `{`, want: "decode"},
		{name: "non-list", body: `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod-a"}}`, want: "items"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := mergeDocuments(
				[]scopedDocument{{Namespace: "team-a", Body: []byte(test.body)}},
				podRESTMapping(),
				false,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("mergeDocuments() error = %v, want %q", err, test.want)
			}
		})
	}
}

func podRESTMapping() *meta.RESTMapping {
	return &meta.RESTMapping{
		Resource: schema.GroupVersionResource{Version: "v1", Resource: "pods"},
		GroupVersionKind: schema.GroupVersionKind{
			Version: "v1",
			Kind:    "Pod",
		},
		Scope: meta.RESTScopeNamespace,
	}
}
