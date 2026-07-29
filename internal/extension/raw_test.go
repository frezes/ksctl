package extension

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeObjectRetainsUnknownFields(t *testing.T) {
	raw := []byte(`{"apiVersion":"kubesphere.io/v1alpha1","kind":"Extension","metadata":{"name":"demo"},"future":{"value":7}}`)

	got, err := decodeObject[Extension](raw)
	if err != nil {
		t.Fatalf("decodeObject() error = %v", err)
	}
	if got.Value.Metadata.Name != "demo" {
		t.Fatalf("metadata.name = %q, want demo", got.Value.Metadata.Name)
	}
	if !bytes.Contains(got.RawJSON(), []byte(`"future"`)) {
		t.Fatalf("RawJSON() = %s, want unknown field", got.RawJSON())
	}
}

func TestDecodeObjectRequiresMetadataName(t *testing.T) {
	_, err := decodeObject[Extension]([]byte(`{"metadata":{},"future":true}`))
	if err == nil || !strings.Contains(err.Error(), "metadata.name") {
		t.Fatalf("decodeObject() error = %v, want metadata.name", err)
	}
}

func TestDecodeListRetainsUnknownTopLevelAndItemFields(t *testing.T) {
	raw := []byte(`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionList","metadata":{"continue":"next"},"futureList":true,"items":[{"metadata":{"name":"demo"},"futureItem":true}]}`)

	got, err := decodeList[Extension](raw)
	if err != nil {
		t.Fatalf("decodeList() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Value.Metadata.Name != "demo" {
		t.Fatalf("items = %#v", got.Items)
	}
	if !bytes.Contains(got.RawJSON(), []byte(`"futureList"`)) {
		t.Fatalf("list RawJSON() = %s, want futureList", got.RawJSON())
	}
	if !bytes.Contains(got.Items[0].RawJSON(), []byte(`"futureItem"`)) {
		t.Fatalf("item RawJSON() = %s, want futureItem", got.Items[0].RawJSON())
	}
}

func TestReplaceListItemsPreservesRetainedUnknownFields(t *testing.T) {
	raw := []byte(`{"apiVersion":"kubesphere.io/v1alpha1","kind":"ExtensionList","futureList":true,"items":[{"metadata":{"name":"keep"},"futureItem":"kept"},{"metadata":{"name":"drop"},"futureItem":"dropped"}]}`)
	list, err := decodeList[Extension](raw)
	if err != nil {
		t.Fatalf("decodeList() error = %v", err)
	}

	filtered, err := replaceListItems(list, list.Items[:1])
	if err != nil {
		t.Fatalf("replaceListItems() error = %v", err)
	}
	var document struct {
		FutureList bool `json:"futureList"`
		Items      []struct {
			Metadata   ObjectMeta `json:"metadata"`
			FutureItem string     `json:"futureItem"`
		} `json:"items"`
	}
	if err := json.Unmarshal(filtered.RawJSON(), &document); err != nil {
		t.Fatalf("unmarshal filtered list: %v", err)
	}
	if !document.FutureList || len(document.Items) != 1 {
		t.Fatalf("filtered document = %#v", document)
	}
	if document.Items[0].Metadata.Name != "keep" || document.Items[0].FutureItem != "kept" {
		t.Fatalf("retained item = %#v", document.Items[0])
	}
}

func TestRawJSONReturnsDefensiveCopy(t *testing.T) {
	object, err := decodeObject[Extension]([]byte(`{"metadata":{"name":"demo"}}`))
	if err != nil {
		t.Fatalf("decodeObject() error = %v", err)
	}

	first := object.RawJSON()
	first[0] = 'X'
	second := object.RawJSON()
	if second[0] != '{' {
		t.Fatalf("second RawJSON() = %q, want defensive copy", second)
	}
}
