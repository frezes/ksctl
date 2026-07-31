package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Object[T any] struct {
	Value T
	doc   JSONDocument
}

type List[T any] struct {
	Items []Object[T]
	doc   JSONDocument
}

func (d JSONDocument) RawJSON() []byte {
	return bytes.Clone(d.data)
}

func (o Object[T]) RawJSON() []byte {
	return o.doc.RawJSON()
}

func (l List[T]) RawJSON() []byte {
	return l.doc.RawJSON()
}

func decodeObject[T any](raw []byte) (Object[T], error) {
	var envelope struct {
		Metadata ObjectMeta `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Object[T]{}, err
	}
	if envelope.Metadata.Name == "" {
		return Object[T]{}, fmt.Errorf("response object is missing metadata.name")
	}

	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return Object[T]{}, err
	}
	return Object[T]{
		Value: value,
		doc:   JSONDocument{data: bytes.Clone(raw)},
	}, nil
}

func decodeList[T any](raw []byte) (List[T], error) {
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return List[T]{}, err
	}

	items := make([]Object[T], 0, len(envelope.Items))
	for index, item := range envelope.Items {
		decoded, err := decodeObject[T](item)
		if err != nil {
			return List[T]{}, fmt.Errorf("decode item %d: %w", index, err)
		}
		items = append(items, decoded)
	}
	return List[T]{
		Items: items,
		doc:   JSONDocument{data: bytes.Clone(raw)},
	}, nil
}

func replaceListItems[T any](list List[T], items []Object[T]) (List[T], error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(list.doc.data, &top); err != nil {
		return List[T]{}, err
	}

	rawItems := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		rawItems = append(rawItems, item.RawJSON())
	}
	encodedItems, err := json.Marshal(rawItems)
	if err != nil {
		return List[T]{}, err
	}
	top["items"] = encodedItems

	encodedList, err := json.Marshal(top)
	if err != nil {
		return List[T]{}, err
	}
	return List[T]{
		Items: items,
		doc:   JSONDocument{data: encodedList},
	}, nil
}
