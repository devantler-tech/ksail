package render_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// countingClient embeds helm.Interface and returns a distinct manifest per
// TemplateChart call ("render-1", "render-2", …) so a test can prove a cache hit
// returned the first render rather than re-templating. It counts calls
// atomically so it is safe under the concurrency test.
type countingClient struct {
	helm.Interface

	calls atomic.Int64
}

func (c *countingClient) TemplateChart(_ context.Context, _ *helm.ChartSpec) (string, error) {
	return fmt.Sprintf("render-%d", c.calls.Add(1)), nil
}

// valuesRelease returns the chartRef podinfo release with inline values, so tests
// can vary ValuesYaml (part of the cache key) independently of the chart source.
func valuesRelease(rawValues string) *helmv2.HelmRelease {
	release := chartRefRelease()
	release.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(rawValues)}

	return release
}

func TestResolverChartCacheServesRepeatRenderAcrossResolvers(t *testing.T) {
	t.Parallel()

	client := &countingClient{}
	cache := render.NewChartCache()
	source := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})

	// Two independent resolvers sharing one cache mirror how validate builds a
	// fresh resolver per kustomization while sharing the run's chart cache.
	first, err := render.NewHelmChartResolver(client, render.WithChartCache(cache)).
		Render(context.Background(), chartRefRelease(), source)
	require.NoError(t, err)
	assert.Equal(t, "render-1", first)

	second, err := render.NewHelmChartResolver(client, render.WithChartCache(cache)).
		Render(context.Background(), chartRefRelease(), source)
	require.NoError(t, err)

	assert.Equal(t, "render-1", second, "second identical render must be served from cache")
	assert.Equal(
		t,
		int64(1),
		client.calls.Load(),
		"TemplateChart must run once for an identical spec",
	)
}

func TestResolverChartCacheMissesOnDistinctSpecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release *helmv2.HelmRelease
		source  render.SourceIndex
	}{
		{
			name:    "different chart version",
			release: chartRefRelease(),
			source:  ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.6.0"}),
		},
		{
			name:    "different values",
			release: valuesRelease(`{"replicaCount":3}`),
			source:  ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"}),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &countingClient{}
			cache := render.NewChartCache()
			resolver := render.NewHelmChartResolver(client, render.WithChartCache(cache))

			// Prime the cache with the baseline spec (tag 6.5.0, no values).
			_, err := resolver.Render(
				context.Background(),
				chartRefRelease(),
				ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"}),
			)
			require.NoError(t, err)

			_, err = resolver.Render(context.Background(), testCase.release, testCase.source)
			require.NoError(t, err)

			assert.Equal(t, int64(2), client.calls.Load(), "a distinct spec must miss the cache")
		})
	}
}

func TestResolverWithoutChartCacheAlwaysRenders(t *testing.T) {
	t.Parallel()

	client := &countingClient{}
	resolver := render.NewHelmChartResolver(client) // no cache configured
	source := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})

	for range 3 {
		_, err := resolver.Render(context.Background(), chartRefRelease(), source)
		require.NoError(t, err)
	}

	assert.Equal(t, int64(3), client.calls.Load(), "without a cache each render must template")
}

// TestResolverChartCacheConcurrentSingleRender proves the cache collapses N
// concurrent identical renders to a single TemplateChart call and is race-clean
// (run under -race): the check/store live inside the render lock, so a waiter
// finds the first result instead of re-rendering. Mirrors validate's fan-out —
// a fresh resolver per goroutine sharing one run cache.
func TestResolverChartCacheConcurrentSingleRender(t *testing.T) {
	t.Parallel()

	client := &countingClient{}
	cache := render.NewChartCache()
	source := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})

	const goroutines = 12

	var waitGroup sync.WaitGroup

	// Each goroutine writes its own slot, so no synchronization is needed to
	// collect the results; every caller must observe the single cached manifest.
	manifests := make([]string, goroutines)
	errs := make([]error, goroutines)

	waitGroup.Add(goroutines)

	for index := range goroutines {
		go func() {
			defer waitGroup.Done()

			manifests[index], errs[index] = render.
				NewHelmChartResolver(client, render.WithChartCache(cache)).
				Render(context.Background(), chartRefRelease(), source)
		}()
	}

	waitGroup.Wait()

	assert.Equal(t, int64(1), client.calls.Load(),
		"concurrent identical renders must collapse to one TemplateChart call")

	for index := range goroutines {
		require.NoErrorf(t, errs[index], "goroutine %d must not error", index)
		assert.Equalf(t, "render-1", manifests[index],
			"goroutine %d must observe the single cached manifest", index)
	}
}

// BenchmarkResolverChartCacheHit measures the cache-hit path (key build + map
// lookup) that replaces a full chart template on a repeat render. The cache is
// primed before the timer resets, so every measured iteration is a hit.
func BenchmarkResolverChartCacheHit(b *testing.B) {
	client := &countingClient{}
	resolver := render.NewHelmChartResolver(client, render.WithChartCache(render.NewChartCache()))
	source := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	release := chartRefRelease()

	// Prime the cache so the timed loop measures only cache hits.
	_, err := resolver.Render(context.Background(), release, source)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := resolver.Render(context.Background(), release, source)
		if err != nil {
			b.Fatal(err)
		}
	}
}
