package helmutil_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewBase(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	repo := &helm.RepositoryEntry{Name: "test-repo", URL: "https://example.com/charts"}
	spec := &helm.ChartSpec{ReleaseName: "test", ChartName: "test-repo/test", Namespace: "default"}

	base := helmutil.NewBase("test", client, 5*time.Minute, repo, spec)

	assert.NotNil(t, base)
}

func TestBaseInstallSuccess(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, nil)

	err := base.Install(context.Background())

	require.NoError(t, err)
}

func TestBaseInstallRepoError(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := base.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add test-repo repository")
}

func TestBaseInstallChartError(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Return(nil, assert.AnError)

	err := base.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "installing test chart")
}

func TestBaseUninstallSuccess(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "test-release", "test-namespace").
		Return(nil)

	err := base.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestBaseUninstallError(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "test-release", "test-namespace").
		Return(assert.AnError)

	err := base.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall test release")
}

func TestBaseImagesSuccess(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	manifest := "spec:\n  containers:\n    - image: nginx:1.25\n    - image: redis:7.0\n"
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(manifest, nil)

	images, err := base.Images(context.Background())

	require.NoError(t, err)
	assert.Contains(t, images, "docker.io/library/nginx:1.25")
	assert.Contains(t, images, "docker.io/library/redis:7.0")
}

func TestBaseImagesTemplateError(t *testing.T) {
	t.Parallel()

	base, client := newBaseWithDefaults(t)
	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return("", assert.AnError)

	images, err := base.Images(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "template chart")
	assert.Nil(t, images)
}

func TestImagesFromChartSuccess(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	spec := &helm.ChartSpec{ChartName: "test-repo/test", Namespace: "default"}
	manifest := "spec:\n  containers:\n    - image: ghcr.io/fluxcd/source-controller:v1.5.0\n"
	client.EXPECT().
		TemplateChart(mock.Anything, spec).
		Return(manifest, nil)

	images, err := helmutil.ImagesFromChart(context.Background(), client, spec)

	require.NoError(t, err)
	assert.Equal(t, []string{"ghcr.io/fluxcd/source-controller:v1.5.0"}, images)
}

func TestImagesFromChartTemplateError(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	spec := &helm.ChartSpec{ChartName: "test-repo/test", Namespace: "default"}
	client.EXPECT().
		TemplateChart(mock.Anything, spec).
		Return("", assert.AnError)

	images, err := helmutil.ImagesFromChart(context.Background(), client, spec)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "template chart test-repo/test")
	assert.Nil(t, images)
}

func newBaseWithDefaults(t *testing.T) (*helmutil.Base, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	repo := &helm.RepositoryEntry{Name: "test-repo", URL: "https://example.com/charts"}
	spec := &helm.ChartSpec{
		ReleaseName: "test-release",
		ChartName:   "test-repo/test",
		Namespace:   "test-namespace",
	}

	base := helmutil.NewBase("test", client, 2*time.Minute, repo, spec)

	return base, client
}
