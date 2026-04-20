package fluxinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestFluxInstaller_Images_Success(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: flux-operator
spec:
  template:
    spec:
      containers:
      - image: ghcr.io/controlplaneio-fluxcd/flux-operator:v0.12.0
`
	client := helm.NewMockInterface(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ChartName == "oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator"
		})).
		Return(manifest, nil)

	installer := fluxinstaller.NewInstaller(client, 5*time.Minute)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.NotEmpty(t, images)
	// The images should include both the operator image from the chart
	// and the distribution images appended by distributionImages().
	assert.Contains(t, images, "ghcr.io/controlplaneio-fluxcd/flux-operator:v0.12.0")
}

func TestFluxInstaller_Images_TemplateError(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", assert.AnError)

	installer := fluxinstaller.NewInstaller(client, 5*time.Minute)

	_, err := installer.Images(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing images")
}

func TestFluxInstaller_Images_EmptyManifest(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", nil)

	installer := fluxinstaller.NewInstaller(client, 5*time.Minute)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	// Even with an empty chart manifest, distributionImages() appends
	// the Flux distribution controller images from the Dockerfile.
	assert.NotEmpty(t, images, "distribution images should still be present")
}
