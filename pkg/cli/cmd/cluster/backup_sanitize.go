package cluster

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

// sanitizeYAMLOutput removes server-assigned metadata fields from kubectl
// output to produce portable, apply-able manifests. Fields stripped include
// resourceVersion, uid, selfLink, creationTimestamp, managedFields, and
// the entire status block.
func sanitizeYAMLOutput(output string) (string, error) {
	var obj unstructured.Unstructured

	err := sigsyaml.Unmarshal([]byte(output), &obj.Object)
	if err != nil {
		// If we can't parse it, return the original output unchanged.
		return output, nil //nolint:nilerr // non-parseable output is kept as-is
	}

	kind := obj.GetKind()
	if strings.HasSuffix(kind, "List") {
		return sanitizeList(&obj)
	}

	sanitizeObject(&obj)

	result, err := sigsyaml.Marshal(obj.Object)
	if err != nil {
		return output, nil //nolint:nilerr // marshal failure falls back to original
	}

	return string(result), nil
}

func sanitizeList(list *unstructured.Unstructured) (string, error) {
	items, found, err := unstructured.NestedSlice(
		list.Object, "items",
	)
	if err != nil || !found {
		// No items found; sanitize the list object itself.
		sanitizeObject(list)

		result, marshalErr := sigsyaml.Marshal(list.Object)
		if marshalErr != nil {
			return "", fmt.Errorf("failed to marshal list: %w", marshalErr)
		}

		return string(result), nil
	}

	var builder strings.Builder

	// Pre-allocate capacity: estimate ~256 bytes per item for YAML output
	// plus separator overhead.
	const estimatedBytesPerItem = 256
	builder.Grow(len(items) * estimatedBytesPerItem)

	for idx, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue // Skip malformed items that aren't maps
		}

		obj := &unstructured.Unstructured{Object: itemMap}
		sanitizeObject(obj)

		data, marshalErr := sigsyaml.Marshal(obj.Object)
		if marshalErr != nil {
			continue // Skip items that can't be marshaled
		}

		if idx > 0 {
			builder.WriteString("---\n")
		}

		builder.Write(data)
	}

	return builder.String(), nil
}

func sanitizeObject(obj *unstructured.Unstructured) {
	// Remove server-assigned metadata fields
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "resourceVersion",
	)
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "metadata", "selfLink")
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "creationTimestamp",
	)
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "managedFields",
	)
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "generation",
	)

	// Remove status block
	unstructured.RemoveNestedField(obj.Object, "status")
}
