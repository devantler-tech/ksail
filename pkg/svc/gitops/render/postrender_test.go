package render_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxkustomize "github.com/fluxcd/pkg/apis/kustomize"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type localPostRenderChart struct {
	helm.Interface

	path string
}

func (c localPostRenderChart) TemplateChart(
	ctx context.Context,
	spec *helm.ChartSpec,
) (string, error) {
	local := *spec
	local.ChartName = c.path
	local.Version = ""

	manifest, err := c.Interface.TemplateChart(ctx, &local)
	if err != nil {
		return "", fmt.Errorf("template local chart: %w", err)
	}

	return manifest, nil
}

func TestResolverAppliesFluxHelmPostRenderersInOrder(t *testing.T) {
	t.Parallel()

	chart := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(chart, "templates"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(chart, "Chart.yaml"), []byte(
		"apiVersion: v2\nname: local-postrender\nversion: 0.1.0\n",
	), 0o600))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(chart, "templates", "app.yaml"), []byte(postRenderChart), 0o600),
	)

	client, err := helm.NewTemplateOnlyClient()
	require.NoError(t, err)

	resolver := render.NewHelmChartResolver(localPostRenderChart{Interface: client, path: chart},
		render.WithChartCache(render.NewChartCache()))
	source := ociIndex(&sourcev1.OCIRepositoryRef{Tag: "6.5.0"})
	release := chartRefRelease()

	raw, err := resolver.Render(t.Context(), release, source)
	require.NoError(t, err)
	require.Contains(t, raw, "image: example.test/app:original-tag")

	target := &fluxkustomize.Selector{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
		Name:    "postrender-app",
	}
	release.Spec.PostRenderers = []helmv2.PostRenderer{
		{Kustomize: &helmv2.Kustomize{Patches: []fluxkustomize.Patch{{
			Target: target,
			Patch:  "- op: replace\n  path: /spec/replicas\n  value: 2\n",
		}}}},
		{Kustomize: &helmv2.Kustomize{
			Patches: []fluxkustomize.Patch{
				{
					Target: target,
					Patch:  "- op: test\n  path: /spec/replicas\n  value: 2\n- op: replace\n  path: /spec/replicas\n  value: 3\n",
				},
			},
			Images: []fluxkustomize.Image{{Name: "example.test/app", NewTag: "reviewed-tag"}},
		}},
	}

	effective, err := resolver.Render(t.Context(), release, source)
	require.NoError(t, err)
	assert.Contains(t, effective, "replicas: 3")
	assert.Contains(t, effective, "image: example.test/app:reviewed-tag")
	assert.NotContains(t, effective, "original-tag")

	release.Spec.PostRenderers[1].Kustomize.Patches[0].Patch = "- op: test\n  path: /spec/replicas\n  value: 999\n"
	_, err = resolver.Render(t.Context(), release, source)
	require.ErrorContains(t, err, "post-render")
}

const postRenderChart = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postrender-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postrender-app
  template:
    metadata:
      labels:
        app: postrender-app
    spec:
      containers:
        - name: app
          image: example.test/app:original-tag
`
