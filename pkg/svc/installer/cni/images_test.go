package cni_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestImagesFromChart_NilClient(t *testing.T) {
	t.Parallel()

	base := cni.NewInstallerBase(nil, "", "", time.Second)
	_, err := base.ImagesFromChart(context.Background(), &helm.ChartSpec{
		ChartName: "test/chart",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestImagesFromChart_TemplateError(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", assert.AnError)

	base := cni.NewInstallerBase(client, "", "", time.Second)
	_, err := base.ImagesFromChart(context.Background(), &helm.ChartSpec{
		ChartName: "test/chart",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "images from chart")
}

func TestImagesFromChart_Success(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test
spec:
  template:
    spec:
      containers:
      - image: nginx:1.25
`
	client := helm.NewMockInterface(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(manifest, nil)

	base := cni.NewInstallerBase(client, "", "", time.Second)
	images, err := base.ImagesFromChart(context.Background(), &helm.ChartSpec{
		ChartName: "test/chart",
	})

	require.NoError(t, err)
	assert.Contains(t, images, "docker.io/library/nginx:1.25")
}
