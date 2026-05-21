package tenant

// GenerateResourceQuotaManifests generates a ResourceQuota for each namespace.
// Returns a map with a single "resourcequota.yaml" (multi-doc) entry, or
// (nil, nil) when WithQuota is false.
func GenerateResourceQuotaManifests(opts Options) (map[string]string, error) {
	if !opts.WithQuota {
		return nil, nil
	}

	cpu := valueOrDefault(opts.QuotaCPU, DefaultQuotaCPU)
	memory := valueOrDefault(opts.QuotaMemory, DefaultQuotaMemory)

	if err := validateQuantities(cpu, memory); err != nil {
		return nil, err
	}

	return generateNamespacedManifest("resourcequota.yaml", opts.Namespaces,
		func(namespace string) map[string]any {
			return map[string]any{
				"apiVersion": "v1",
				"kind":       "ResourceQuota",
				"metadata":   namespacedMeta(opts.Name+"-quota", namespace),
				"spec": map[string]any{
					"hard": map[string]any{
						"requests.cpu":    cpu,
						"requests.memory": memory,
						"limits.cpu":      cpu,
						"limits.memory":   memory,
					},
				},
			}
		})
}
