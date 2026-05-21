package tenant

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// namespacedMeta returns the standard metadata block (name, namespace, and
// managed-by labels) shared by generated namespaced manifests.
func namespacedMeta(name, namespace string) map[string]any {
	return map[string]any{
		"name":      name,
		"namespace": namespace,
		"labels":    ManagedByLabels(),
	}
}

// generateNamespacedManifest builds a single multi-document YAML file by
// applying build(namespace) for each namespace. Returns a map keyed by filename.
func generateNamespacedManifest(
	filename string,
	namespaces []string,
	build func(namespace string) map[string]any,
) (map[string]string, error) {
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	docs := make([]string, 0, len(namespaces))

	for _, namespace := range namespaces {
		out, err := yaml.Marshal(build(namespace))
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", filename, err)
		}

		docs = append(docs, string(out))
	}

	return map[string]string{filename: joinDocs(docs)}, nil
}

// validateQuantities ensures every provided value parses as a resource.Quantity.
func validateQuantities(values ...string) error {
	for _, value := range values {
		if err := validateQuantity(value); err != nil {
			return err
		}
	}

	return nil
}

// valueOrDefault returns value when non-empty, otherwise fallback.
func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
