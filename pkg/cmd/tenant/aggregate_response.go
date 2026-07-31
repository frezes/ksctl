package tenant

import (
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type scopedDocument struct {
	Namespace string
	Body      []byte
}

func mergeDocuments(
	documents []scopedDocument,
	mapping *meta.RESTMapping,
	tableRequested bool,
) ([]byte, error) {
	if mapping == nil {
		return nil, fmt.Errorf("merge Kubernetes responses: REST mapping is required")
	}
	if len(documents) == 0 {
		return emptyDocument(mapping, tableRequested)
	}

	var merged *unstructured.Unstructured
	var mergedItems []any
	var mergedRows []any
	var columns []any

	for index, document := range documents {
		object := &unstructured.Unstructured{}
		if err := json.Unmarshal(document.Body, &object.Object); err != nil {
			return nil, fmt.Errorf(
				"decode Kubernetes response for namespace %q: %w",
				document.Namespace,
				err,
			)
		}
		if index == 0 {
			merged = object.DeepCopy()
		} else if object.GetAPIVersion() != merged.GetAPIVersion() ||
			object.GetKind() != merged.GetKind() {
			return nil, fmt.Errorf(
				"Kubernetes response type %s %s in namespace %q does not match %s %s",
				object.GetAPIVersion(),
				object.GetKind(),
				document.Namespace,
				merged.GetAPIVersion(),
				merged.GetKind(),
			)
		}

		if tableRequested {
			documentColumns, found, err := unstructured.NestedSlice(
				object.Object,
				"columnDefinitions",
			)
			if err != nil || !found {
				return nil, fmt.Errorf(
					"decode Kubernetes Table column definitions for namespace %q: %w",
					document.Namespace,
					requiredFieldError(err, found),
				)
			}
			if index == 0 {
				columns = documentColumns
			} else if !reflect.DeepEqual(documentColumns, columns) {
				return nil, fmt.Errorf(
					"Kubernetes Table column definitions in namespace %q do not match",
					document.Namespace,
				)
			}
			rows, found, err := unstructured.NestedSlice(object.Object, "rows")
			if err != nil || !found {
				return nil, fmt.Errorf(
					"decode Kubernetes Table rows for namespace %q: %w",
					document.Namespace,
					requiredFieldError(err, found),
				)
			}
			rows, err = addTableRowNamespaces(rows, document.Namespace, mapping)
			if err != nil {
				return nil, err
			}
			mergedRows = append(mergedRows, rows...)
			continue
		}

		items, found, err := unstructured.NestedSlice(object.Object, "items")
		if err != nil || !found {
			return nil, fmt.Errorf(
				"decode Kubernetes list items for namespace %q: %w",
				document.Namespace,
				requiredFieldError(err, found),
			)
		}
		mergedItems = append(mergedItems, items...)
	}

	if tableRequested {
		if merged.GetKind() != "Table" {
			return nil, fmt.Errorf(
				"Kubernetes response type %s %s is not a Table",
				merged.GetAPIVersion(),
				merged.GetKind(),
			)
		}
		groupVersion, err := schema.ParseGroupVersion(merged.GetAPIVersion())
		if err != nil || groupVersion.Group != "meta.k8s.io" {
			return nil, fmt.Errorf(
				"Kubernetes response type %s %s is not a meta.k8s.io Table",
				merged.GetAPIVersion(),
				merged.GetKind(),
			)
		}
		if err := unstructured.SetNestedSlice(
			merged.Object,
			columns,
			"columnDefinitions",
		); err != nil {
			return nil, fmt.Errorf("set Kubernetes Table column definitions: %w", err)
		}
		if err := unstructured.SetNestedSlice(merged.Object, mergedRows, "rows"); err != nil {
			return nil, fmt.Errorf("set Kubernetes Table rows: %w", err)
		}
	} else if err := unstructured.SetNestedSlice(
		merged.Object,
		mergedItems,
		"items",
	); err != nil {
		return nil, fmt.Errorf("set Kubernetes list items: %w", err)
	}

	clearAggregateMetadata(merged)
	body, err := json.Marshal(merged.Object)
	if err != nil {
		return nil, fmt.Errorf("encode merged Kubernetes response: %w", err)
	}
	return body, nil
}

func emptyDocument(mapping *meta.RESTMapping, tableRequested bool) ([]byte, error) {
	object := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"continue":        "",
			"resourceVersion": "",
		},
	}}
	if tableRequested {
		object.SetAPIVersion("meta.k8s.io/v1")
		object.SetKind("Table")
		object.Object["columnDefinitions"] = []any{}
		object.Object["rows"] = []any{}
	} else {
		object.SetAPIVersion(mapping.GroupVersionKind.GroupVersion().String())
		object.SetKind(mapping.GroupVersionKind.Kind + "List")
		object.Object["items"] = []any{}
	}
	body, err := json.Marshal(object.Object)
	if err != nil {
		return nil, fmt.Errorf("encode empty Kubernetes response: %w", err)
	}
	return body, nil
}

func addTableRowNamespaces(
	rows []any,
	namespace string,
	mapping *meta.RESTMapping,
) ([]any, error) {
	for index, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"Kubernetes Table row %d in namespace %q is not an object",
				index,
				namespace,
			)
		}
		if object, found := row["object"]; found && object != nil {
			continue
		}
		row["object"] = map[string]any{
			"apiVersion": mapping.GroupVersionKind.GroupVersion().String(),
			"kind":       mapping.GroupVersionKind.Kind,
			"metadata": map[string]any{
				"namespace": namespace,
			},
		}
		rows[index] = row
	}
	return rows, nil
}

func clearAggregateMetadata(object *unstructured.Unstructured) {
	_ = unstructured.SetNestedField(object.Object, "", "metadata", "continue")
	_ = unstructured.SetNestedField(object.Object, "", "metadata", "resourceVersion")
}

func requiredFieldError(err error, found bool) error {
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("required field is missing")
	}
	return nil
}
