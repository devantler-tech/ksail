package render

import (
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
)

// ChartCache memoizes rendered Helm chart manifests by their resolved chart
// spec, so a chart referenced by multiple kustomizations in one validate/scan
// run is templated only once. It is safe for concurrent use.
//
// Scope it to a single run (create one per gitopsRenderer): the render output
// is fully determined by the spec key, but a HelmRelease pinned to a floating
// chart version could resolve to a different chart between runs, so a cache
// must not outlive the run it was populated in.
type ChartCache struct {
	mu      sync.Mutex
	entries map[string]string
}

// NewChartCache returns an empty, ready-to-use ChartCache.
func NewChartCache() *ChartCache {
	return &ChartCache{entries: make(map[string]string)}
}

// get returns the cached manifest for spec, if present.
func (c *ChartCache) get(spec *helm.ChartSpec) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	manifest, ok := c.entries[chartCacheKey(spec)]

	return manifest, ok
}

// put stores the rendered manifest for spec.
func (c *ChartCache) put(spec *helm.ChartSpec, manifest string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[chartCacheKey(spec)] = manifest
}

// chartCacheKey builds a deterministic, collision-free key from the only fields
// buildChartSpec populates that affect the rendered output. The NUL separator
// cannot appear in a chart reference, release name, namespace, or valid YAML,
// so the join is unambiguous. Keep this in sync with buildChartSpec: a new
// output-affecting field there must be added here.
func chartCacheKey(spec *helm.ChartSpec) string {
	return strings.Join([]string{
		spec.ChartName,
		spec.RepoURL,
		spec.Version,
		spec.ReleaseName,
		spec.Namespace,
		spec.ValuesYaml,
	}, "\x00")
}
