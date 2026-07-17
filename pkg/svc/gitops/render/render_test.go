package render_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errRegistryUnreachable is a static stand-in for a chart-fetch failure, used to
// exercise the non-silent degradation path without a dynamic error (err113).
var errRegistryUnreachable = errors.New("pull chart: registry unreachable")

// fakeResolver is a ChartResolver test double that renders via the supplied func.
type fakeResolver struct {
	render func(helmRelease *helmv2.HelmRelease, sources render.SourceIndex) (string, error)
}

func (f fakeResolver) Render(
	_ context.Context,
	helmRelease *helmv2.HelmRelease,
	sources render.SourceIndex,
) (string, error) {
	return f.render(helmRelease, sources)
}

const (
	ociRepoYAML = `apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: podinfo
  namespace: flux-system
spec:
  url: oci://ghcr.io/stefanprodan/charts/podinfo
  ref:
    tag: 6.5.0`

	helmReleaseYAML = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: flux-system
spec:
  chartRef:
    kind: OCIRepository
    name: podinfo
  values:
    replicaCount: 2`

	configMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  namespace: flux-system
data:
  key: value`

	renderedDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: podinfo
  namespace: flux-system
spec:
  replicas: 2`
)

func joinDocs(docs ...string) []byte {
	return []byte(strings.Join(docs, "\n---\n"))
}

func findDoc(docs []render.Document, kind string) (render.Document, bool) {
	for _, doc := range docs {
		if doc.Kind == kind {
			return doc, true
		}
	}

	return render.Document{}, false
}

func TestExpandReplacesHelmReleaseWithRenderedChildren(t *testing.T) {
	t.Parallel()

	resolver := fakeResolver{
		render: func(helmRelease *helmv2.HelmRelease, sources render.SourceIndex) (string, error) {
			// The source index should carry the OCIRepository from the same stream.
			assert.Contains(t, sources.OCIRepos, "flux-system/podinfo")
			assert.Equal(t, "podinfo", helmRelease.Name)

			return renderedDeployment, nil
		},
	}

	result, err := render.Expand(
		context.Background(),
		joinDocs(ociRepoYAML, helmReleaseYAML, configMapYAML),
		render.Options{Resolver: resolver},
	)
	require.NoError(t, err)
	assert.Empty(t, result.Degradations)

	// HelmRelease CR is dropped; its rendered Deployment takes its place.
	_, hasHelmRelease := findDoc(result.Documents, "HelmRelease")
	assert.False(t, hasHelmRelease, "HelmRelease CR should be replaced by rendered children")

	deployment, deploymentFound := findDoc(result.Documents, "Deployment")
	require.True(t, deploymentFound, "rendered Deployment should be present")
	assert.Equal(t, render.OriginRendered, deployment.Provenance.Origin)
	assert.Equal(t, "flux-system/podinfo", deployment.Provenance.SourceHelmRelease)
	assert.Equal(t, "podinfo", deployment.Provenance.ReleaseName)

	// Source object and plain resource are retained verbatim.
	ociRepo, ociRepoFound := findDoc(result.Documents, "OCIRepository")
	require.True(t, ociRepoFound)
	assert.Equal(t, render.OriginStream, ociRepo.Provenance.Origin)

	configMap, configMapFound := findDoc(result.Documents, "ConfigMap")
	require.True(t, configMapFound)
	assert.Equal(t, render.OriginStream, configMap.Provenance.Origin)
}

func TestExpandSilentDegradationKeepsHelmRelease(t *testing.T) {
	t.Parallel()

	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, _ render.SourceIndex) (string, error) {
			return "", render.ErrUnsupportedSourceKind
		},
	}

	result, err := render.Expand(
		context.Background(),
		joinDocs(helmReleaseYAML),
		render.Options{Resolver: resolver},
	)
	require.NoError(t, err)

	require.Len(t, result.Degradations, 1)
	assert.Equal(t, "flux-system/podinfo", result.Degradations[0].HelmRelease)
	assert.True(
		t,
		result.Degradations[0].Silent,
		"unsupported source kind should be a silent degradation",
	)

	// On degradation the HelmRelease CR is retained for CR-schema validation.
	helmRelease, ok := findDoc(result.Documents, "HelmRelease")
	require.True(t, ok)
	assert.Equal(t, render.OriginStream, helmRelease.Provenance.Origin)
}

func TestExpandNonSilentDegradation(t *testing.T) {
	t.Parallel()

	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, _ render.SourceIndex) (string, error) {
			return "", errRegistryUnreachable
		},
	}

	result, err := render.Expand(
		context.Background(),
		joinDocs(helmReleaseYAML),
		render.Options{Resolver: resolver},
	)
	require.NoError(t, err)

	require.Len(t, result.Degradations, 1)
	assert.False(t, result.Degradations[0].Silent, "a fetch failure should surface as a warning")
	assert.Contains(t, result.Degradations[0].Reason, "registry unreachable")
}

func TestExpandUnparseableHelmReleasePassedThrough(t *testing.T) {
	t.Parallel()

	called := false
	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, _ render.SourceIndex) (string, error) {
			called = true

			return "", nil
		},
	}

	// spec is a scalar, so unmarshalling into HelmRelease fails.
	malformed := `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: broken
  namespace: flux-system
spec: not-an-object`

	result, err := render.Expand(
		context.Background(),
		joinDocs(malformed),
		render.Options{Resolver: resolver},
	)
	require.NoError(t, err)

	assert.False(t, called, "resolver should not run for an unparseable HelmRelease")
	assert.Empty(t, result.Degradations)

	helmRelease, ok := findDoc(result.Documents, "HelmRelease")
	require.True(t, ok)
	assert.Equal(t, render.OriginStream, helmRelease.Provenance.Origin)
}

func TestExpandRequiresResolver(t *testing.T) {
	t.Parallel()

	_, err := render.Expand(context.Background(), joinDocs(configMapYAML), render.Options{})
	require.ErrorIs(t, err, render.ErrNoResolver)
}

func TestRenderResultBytesIsDeterministicAndSorted(t *testing.T) {
	t.Parallel()

	// Resolver emits children in a non-sorted order to prove Bytes() is stable.
	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, _ render.SourceIndex) (string, error) {
			return joinDocsString(renderedDeployment, `apiVersion: v1
kind: Service
metadata:
  name: podinfo
  namespace: flux-system`), nil
		},
	}

	stream := joinDocs(helmReleaseYAML)

	first, err := render.Expand(context.Background(), stream, render.Options{Resolver: resolver})
	require.NoError(t, err)

	second, err := render.Expand(context.Background(), stream, render.Options{Resolver: resolver})
	require.NoError(t, err)

	assert.Equal(t, string(first.Bytes()), string(second.Bytes()), "output must be reproducible")

	// Sorted by kind: Deployment precedes Service.
	out := string(first.Bytes())
	assert.Less(t, strings.Index(out, "kind: Deployment"), strings.Index(out, "kind: Service"))
	assert.Contains(t, out, "\n---\n")
}

func joinDocsString(docs ...string) string {
	return strings.Join(docs, "\n---\n")
}

// helmReleaseValuesFromYAML references a ConfigMap "app-config" via valuesFrom.
// Whether that ConfigMap is in the stream determines if the offline render is
// full-fidelity.
const (
	helmReleaseValuesFromYAML = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: flux-system
spec:
  chartRef:
    kind: OCIRepository
    name: podinfo
  valuesFrom:
    - kind: ConfigMap
      name: app-config`

	helmReleaseValuesFromOptionalYAML = helmReleaseValuesFromYAML + `
      optional: true`

	appConfigYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: flux-system
data:
  values.yaml: |
    replicaCount: 3`
)

// TestExpandDegradesOnUnresolvableValuesFrom pins the render-fidelity honesty
// contract: a HelmRelease whose non-optional valuesFrom ConfigMap/Secret is
// absent from the offline stream still renders, but records a
// DegradationPartialValues so the shift-left gate does not silently under-report
// its coverage. An optional reference, or a resolvable one, degrades nothing.
func TestExpandDegradesOnUnresolvableValuesFrom(t *testing.T) {
	t.Parallel()

	resolver := fakeResolver{
		render: func(_ *helmv2.HelmRelease, _ render.SourceIndex) (string, error) {
			return renderedDeployment, nil
		},
	}

	tests := []struct {
		name         string
		stream       []byte
		wantDegraded bool
	}{
		{
			name:         "non-optional valuesFrom absent from the stream degrades",
			stream:       joinDocs(helmReleaseValuesFromYAML),
			wantDegraded: true,
		},
		{
			name:         "optional valuesFrom absent from the stream is tolerated",
			stream:       joinDocs(helmReleaseValuesFromOptionalYAML),
			wantDegraded: false,
		},
		{
			name:         "resolvable valuesFrom does not degrade",
			stream:       joinDocs(helmReleaseValuesFromYAML, appConfigYAML),
			wantDegraded: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := render.Expand(
				context.Background(),
				testCase.stream,
				render.Options{Resolver: resolver},
			)
			require.NoError(t, err)
			assertValuesFromDegradation(t, result, testCase.wantDegraded)
		})
	}
}

// assertValuesFromDegradation checks that the chart rendered and that a partial-
// values degradation is present exactly when wantDegraded is true.
func assertValuesFromDegradation(t *testing.T, result render.Result, wantDegraded bool) {
	t.Helper()

	// The chart renders regardless of values coverage.
	_, hasDeployment := findDoc(result.Documents, "Deployment")
	require.True(t, hasDeployment, "chart should render regardless of values coverage")

	if !wantDegraded {
		assert.Empty(t, result.Degradations)

		return
	}

	require.Len(t, result.Degradations, 1)
	degradation := result.Degradations[0]
	assert.Equal(t, "flux-system/podinfo", degradation.HelmRelease)
	assert.Equal(t, render.DegradationPartialValues, degradation.Kind)
	assert.False(t, degradation.Silent, "an unresolvable non-optional valuesFrom should warn")
	assert.Contains(t, degradation.Reason, "app-config")
}
