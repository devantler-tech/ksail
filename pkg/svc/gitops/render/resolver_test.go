package render_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// captureClient embeds helm.Interface and records the ChartSpec passed to
// TemplateChart, so tests can assert the HelmRelease→ChartSpec mapping without
// any network or real chart. Other interface methods are never called.
type captureClient struct {
	helm.Interface

	spec *helm.ChartSpec
}

func (c *captureClient) TemplateChart(_ context.Context, spec *helm.ChartSpec) (string, error) {
	c.spec = spec

	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: rendered", nil
}

func helmReleaseObj(name string, spec helmv2.HelmReleaseSpec) *helmv2.HelmRelease {
	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "flux-system"},
		Spec:       spec,
	}
}

func resolveSpec(
	t *testing.T,
	helmRelease *helmv2.HelmRelease,
	sources render.SourceIndex,
) *helm.ChartSpec {
	t.Helper()

	capture := &captureClient{}
	resolver := render.NewHelmChartResolver(capture)

	_, err := resolver.Render(context.Background(), helmRelease, sources)
	require.NoError(t, err)
	require.NotNil(t, capture.spec)

	return capture.spec
}

func ociIndex(ref *sourcev1.OCIRepositoryRef) render.SourceIndex {
	return render.SourceIndex{
		OCIRepos: map[string]*sourcev1.OCIRepository{
			"flux-system/podinfo": {
				Spec: sourcev1.OCIRepositorySpec{
					URL:       "oci://ghcr.io/stefanprodan/charts/podinfo",
					Reference: ref,
				},
			},
		},
	}
}

func chartRefRelease() *helmv2.HelmRelease {
	return helmReleaseObj("podinfo", helmv2.HelmReleaseSpec{
		ChartRef: &helmv2.CrossNamespaceSourceReference{
			Kind: sourcev1.OCIRepositoryKind,
			Name: "podinfo",
		},
	})
}

func TestBuildChartSpecOCIRepositoryTag(t *testing.T) {
	t.Parallel()

	spec := resolveSpec(t, chartRefRelease(), ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"}))

	assert.Equal(t, "oci://ghcr.io/stefanprodan/charts/podinfo", spec.ChartName)
	assert.Equal(t, "6.5.0", spec.Version)
	assert.Empty(t, spec.RepoURL)
}

func TestBuildChartSpecOCIRepositorySemVer(t *testing.T) {
	t.Parallel()

	spec := resolveSpec(
		t,
		chartRefRelease(),
		ociIndex(&sourcev1.OCIRepositoryRef{SemVer: ">=6.0.0"}),
	)

	assert.Equal(t, ">=6.0.0", spec.Version)
}

func TestBuildChartSpecOCIRepositoryDigest(t *testing.T) {
	t.Parallel()

	spec := resolveSpec(
		t,
		chartRefRelease(),
		ociIndex(&sourcev1.OCIRepositoryRef{Digest: "sha256:abc123"}),
	)

	assert.Equal(t, "oci://ghcr.io/stefanprodan/charts/podinfo@sha256:abc123", spec.ChartName)
	assert.Empty(t, spec.Version)
}

func TestBuildChartSpecHelmRepositoryHTTP(t *testing.T) {
	t.Parallel()

	helmRelease := helmReleaseObj("podinfo", helmv2.HelmReleaseSpec{
		Chart: &helmv2.HelmChartTemplate{
			Spec: helmv2.HelmChartTemplateSpec{
				Chart:   "podinfo",
				Version: "6.5.0",
				SourceRef: helmv2.CrossNamespaceObjectReference{
					Kind: sourcev1.HelmRepositoryKind,
					Name: "podinfo",
				},
			},
		},
	})
	sources := render.SourceIndex{
		HelmRepos: map[string]*sourcev1.HelmRepository{
			"flux-system/podinfo": {
				Spec: sourcev1.HelmRepositorySpec{URL: "https://stefanprodan.github.io/podinfo"},
			},
		},
	}

	spec := resolveSpec(t, helmRelease, sources)

	assert.Equal(t, "https://stefanprodan.github.io/podinfo", spec.RepoURL)
	assert.Equal(t, "podinfo", spec.ChartName)
	assert.Equal(t, "6.5.0", spec.Version)
}

func TestBuildChartSpecHelmRepositoryOCIType(t *testing.T) {
	t.Parallel()

	helmRelease := helmReleaseObj("podinfo", helmv2.HelmReleaseSpec{
		Chart: &helmv2.HelmChartTemplate{
			Spec: helmv2.HelmChartTemplateSpec{
				Chart:   "podinfo",
				Version: "6.5.0",
				SourceRef: helmv2.CrossNamespaceObjectReference{
					Kind: sourcev1.HelmRepositoryKind,
					Name: "podinfo",
				},
			},
		},
	})
	sources := render.SourceIndex{
		HelmRepos: map[string]*sourcev1.HelmRepository{
			"flux-system/podinfo": {
				Spec: sourcev1.HelmRepositorySpec{
					Type: sourcev1.HelmRepositoryTypeOCI,
					URL:  "oci://ghcr.io/stefanprodan/charts",
				},
			},
		},
	}

	spec := resolveSpec(t, helmRelease, sources)

	assert.Equal(t, "oci://ghcr.io/stefanprodan/charts/podinfo", spec.ChartName)
	assert.Empty(t, spec.RepoURL)
	assert.Equal(t, "6.5.0", spec.Version)
}

func TestBuildChartSpecInlineValues(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.Values = &apiextensionsv1.JSON{
		Raw: []byte(`{"replicaCount":3,"image":{"tag":"x"}}`),
	}

	spec := resolveSpec(t, helmRelease, ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"}))

	assert.Contains(t, spec.ValuesYaml, "replicaCount: 3")
	assert.Contains(t, spec.ValuesYaml, "tag: x")
}

func TestBuildChartSpecValuesFromConfigMap(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.ValuesFrom = []fluxmeta.ValuesReference{
		{Kind: "ConfigMap", Name: "extra-values"},
	}

	sources := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	sources.ConfigMaps = map[string]map[string]string{
		"flux-system/extra-values": {"values.yaml": "service:\n  port: 9999"},
	}

	spec := resolveSpec(t, helmRelease, sources)

	assert.Contains(t, spec.ValuesYaml, "port: 9999")
}

func TestRenderUnsupportedSourceKindDegrades(t *testing.T) {
	t.Parallel()

	helmRelease := helmReleaseObj("app", helmv2.HelmReleaseSpec{
		Chart: &helmv2.HelmChartTemplate{
			Spec: helmv2.HelmChartTemplateSpec{
				Chart: "app",
				SourceRef: helmv2.CrossNamespaceObjectReference{
					Kind: sourcev1.GitRepositoryKind,
					Name: "repo",
				},
			},
		},
	})

	resolver := render.NewHelmChartResolver(&captureClient{})

	_, err := resolver.Render(context.Background(), helmRelease, render.SourceIndex{})
	require.ErrorIs(t, err, render.ErrUnsupportedSourceKind)
}

func TestRenderSourceNotFoundDegrades(t *testing.T) {
	t.Parallel()

	resolver := render.NewHelmChartResolver(&captureClient{})

	_, err := resolver.Render(context.Background(), chartRefRelease(), render.SourceIndex{})
	require.ErrorIs(t, err, render.ErrSourceNotFound)
}

func TestRenderNoChartSourceDegrades(t *testing.T) {
	t.Parallel()

	helmRelease := helmReleaseObj("app", helmv2.HelmReleaseSpec{})

	resolver := render.NewHelmChartResolver(&captureClient{})

	_, err := resolver.Render(context.Background(), helmRelease, render.SourceIndex{})
	require.ErrorIs(t, err, render.ErrNoChartSource)
}

func TestReleaseNameMappedToChartSpec(t *testing.T) {
	t.Parallel()

	helmRelease := chartRefRelease()
	helmRelease.Spec.ReleaseName = "custom-release"
	helmRelease.Spec.TargetNamespace = "apps"

	spec := resolveSpec(t, helmRelease, ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"}))

	assert.Equal(t, "custom-release", spec.ReleaseName)
	assert.Equal(t, "apps", spec.Namespace)
	assert.True(t, strings.HasPrefix(spec.ChartName, "oci://"))
}

// serialProbeClient embeds helm.Interface and records the peak number of
// TemplateChart calls running concurrently, so a test can assert that the
// in-process Helm render step is serialized. Other interface methods are unused.
type serialProbeClient struct {
	helm.Interface

	mu       sync.Mutex
	inFlight int
	maxSeen  int
}

func (c *serialProbeClient) TemplateChart(_ context.Context, _ *helm.ChartSpec) (string, error) {
	c.mu.Lock()

	c.inFlight++
	if c.inFlight > c.maxSeen {
		c.maxSeen = c.inFlight
	}
	c.mu.Unlock()

	// Widen the overlap window so an unserialized render is observed as
	// inFlight > 1 well within scheduler jitter, making the assertion reliable.
	time.Sleep(time.Millisecond)

	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()

	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: rendered", nil
}

// TestRenderSerializesConcurrentHelmTemplating guards the #5362 fix. validate and
// scan render kustomizations in parallel, each with its own HelmChartResolver, but
// the Helm template step shares Helm's process-global on-disk caches and must be
// serialized; without the package-level lock, concurrent renders interleaved into
// each other's manifest stream. The probe fails if two TemplateChart calls overlap.
func TestRenderSerializesConcurrentHelmTemplating(t *testing.T) {
	t.Parallel()

	probe := &serialProbeClient{}

	const goroutines = 12

	var waitGroup sync.WaitGroup

	waitGroup.Add(goroutines)

	for range goroutines {
		go func() {
			defer waitGroup.Done()

			// A fresh resolver and inputs per goroutine mirror how validate
			// constructs a client per kustomization; the package-level lock must
			// serialize across independent resolvers.
			resolver := render.NewHelmChartResolver(probe)

			_, _ = resolver.Render(
				context.Background(),
				chartRefRelease(),
				ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"}),
			)
		}()
	}

	waitGroup.Wait()

	assert.Equal(t, 1, probe.maxSeen,
		"concurrent in-process Helm renders must be serialized (issue #5362)")
}
