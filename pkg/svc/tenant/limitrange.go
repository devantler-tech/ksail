package tenant

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// GenerateLimitRangeManifests generates a LimitRange for each namespace.
// Returns a map with a single "limitrange.yaml" (multi-doc) entry, or
// (nil, nil) when WithLimitRange is false.
func GenerateLimitRangeManifests(opts Options) (map[string]string, error) {
	if !opts.WithLimitRange {
		return nil, nil
	}

	if len(opts.Namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	defaultCPU := valueOrDefault(opts.LimitDefaultCPU, DefaultLimitDefaultCPU)
	defaultMemory := valueOrDefault(opts.LimitDefaultMemory, DefaultLimitDefaultMemory)
	requestCPU := valueOrDefault(opts.LimitRequestCPU, DefaultLimitRequestCPU)
	requestMemory := valueOrDefault(opts.LimitRequestMemory, DefaultLimitRequestMemory)

	for _, qty := range []string{defaultCPU, defaultMemory, requestCPU, requestMemory} {
		if _, err := resource.ParseQuantity(qty); err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidQuantity, qty)
		}
	}

	docs := make([]string, 0, len(opts.Namespaces))

	for _, namespace := range opts.Namespaces {
		limitRange := map[string]any{
			"apiVersion": "v1",
			"kind":       "LimitRange",
			"metadata": map[string]any{
				"name":      opts.Name + "-limits",
				"namespace": namespace,
				"labels":    ManagedByLabels(),
			},
			"spec": map[string]any{
				"limits": []map[string]any{
					{
						"type": "Container",
						"default": map[string]any{
							"cpu":    defaultCPU,
							"memory": defaultMemory,
						},
						"defaultRequest": map[string]any{
							"cpu":    requestCPU,
							"memory": requestMemory,
						},
					},
				},
			},
		}

		out, err := yaml.Marshal(limitRange)
		if err != nil {
			return nil, fmt.Errorf("marshal limit range: %w", err)
		}

		docs = append(docs, string(out))
	}

	return map[string]string{"limitrange.yaml": joinDocs(docs)}, nil
}
