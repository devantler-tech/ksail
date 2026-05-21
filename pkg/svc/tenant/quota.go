package tenant

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// GenerateResourceQuotaManifests generates a ResourceQuota for each namespace.
// Returns a map with a single "resourcequota.yaml" (multi-doc) entry, or
// (nil, nil) when WithQuota is false.
func GenerateResourceQuotaManifests(opts Options) (map[string]string, error) {
	if !opts.WithQuota {
		return nil, nil
	}

	if len(opts.Namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	cpu := valueOrDefault(opts.QuotaCPU, DefaultQuotaCPU)
	memory := valueOrDefault(opts.QuotaMemory, DefaultQuotaMemory)

	for _, qty := range []string{cpu, memory} {
		if _, err := resource.ParseQuantity(qty); err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidQuantity, qty)
		}
	}

	docs := make([]string, 0, len(opts.Namespaces))

	for _, namespace := range opts.Namespaces {
		quota := map[string]any{
			"apiVersion": "v1",
			"kind":       "ResourceQuota",
			"metadata": map[string]any{
				"name":      opts.Name + "-quota",
				"namespace": namespace,
				"labels":    ManagedByLabels(),
			},
			"spec": map[string]any{
				"hard": map[string]any{
					"requests.cpu":    cpu,
					"requests.memory": memory,
					"limits.cpu":      cpu,
					"limits.memory":   memory,
				},
			},
		}

		out, err := yaml.Marshal(quota)
		if err != nil {
			return nil, fmt.Errorf("marshal resource quota: %w", err)
		}

		docs = append(docs, string(out))
	}

	return map[string]string{"resourcequota.yaml": joinDocs(docs)}, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
