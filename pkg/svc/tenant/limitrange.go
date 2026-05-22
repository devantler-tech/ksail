package tenant

// GenerateLimitRangeManifests generates a LimitRange for each namespace.
// Returns a map with a single "limitrange.yaml" (multi-doc) entry, or an
// empty map when WithLimitRange is false.
func GenerateLimitRangeManifests(opts Options) (map[string]string, error) {
	if !opts.WithLimitRange {
		return map[string]string{}, nil
	}

	defaultCPU := valueOrDefault(opts.LimitDefaultCPU, DefaultLimitDefaultCPU)
	defaultMemory := valueOrDefault(opts.LimitDefaultMemory, DefaultLimitDefaultMemory)
	requestCPU := valueOrDefault(opts.LimitRequestCPU, DefaultLimitRequestCPU)
	requestMemory := valueOrDefault(opts.LimitRequestMemory, DefaultLimitRequestMemory)

	err := validateQuantities(defaultCPU, defaultMemory, requestCPU, requestMemory)
	if err != nil {
		return nil, err
	}

	return generateNamespacedManifest("limitrange.yaml", opts.Namespaces,
		func(namespace string) map[string]any {
			return map[string]any{
				"apiVersion": "v1",
				"kind":       "LimitRange",
				"metadata":   namespacedMeta(opts.Name+"-limits", namespace),
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
		})
}
