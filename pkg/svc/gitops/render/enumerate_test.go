package render_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const enumerateStream = `apiVersion: v1
kind: ConfigMap
metadata:
  name: not-a-helmrelease
  namespace: test
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo
  namespace: test
spec:
  url: https://example.com/charts
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: test
spec:
  chart:
    spec:
      chart: podinfo
      version: 1.2.3
      sourceRef:
        kind: HelmRepository
        name: podinfo
  values:
    replicaCount: 2
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: sourceless
  namespace: test
spec:
  interval: 1m
`

func TestEnumerateChartSpecsResolvesAndDegrades(t *testing.T) {
	t.Parallel()

	specs, degradations := render.EnumerateChartSpecs([]byte(enumerateStream))

	require.Len(t, specs, 1)
	assert.Equal(t, "podinfo", specs[0].ReleaseName)
	assert.Equal(t, "test", specs[0].Namespace)
	assert.Equal(t, "podinfo", specs[0].ChartName)
	assert.Equal(t, "1.2.3", specs[0].Version)
	assert.Equal(t, "https://example.com/charts", specs[0].RepoURL)
	assert.Contains(t, specs[0].ValuesYaml, "replicaCount")

	require.Len(t, degradations, 1)
	assert.Equal(t, "test/sourceless", degradations[0].HelmRelease)
	require.ErrorIs(t, degradations[0].Err, render.ErrNoChartSource)
}

func TestEnumerateChartSpecsEmptyStream(t *testing.T) {
	t.Parallel()

	specs, degradations := render.EnumerateChartSpecs(nil)

	assert.Empty(t, specs)
	assert.Empty(t, degradations)
}
