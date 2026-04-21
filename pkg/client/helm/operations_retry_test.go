package helm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errHelmConnectionResetByPeer = errors.New(
	"read tcp 10.0.0.1:12345->1.2.3.4:443: read: connection reset by peer",
)

func TestInstallChartWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	ctx := context.Background()

	spec := &helm.ChartSpec{
		ReleaseName: "my-release",
		ChartName:   "my-chart",
		Namespace:   "default",
	}

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).Return(&helm.ReleaseInfo{Name: "my-release"}, nil).Once()

	err := helm.InstallChartWithRetry(ctx, mockClient, spec, "test-repo")

	require.NoError(t, err)
}

func TestInstallChartWithRetry_ContextCancellation(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)

	ctx, cancel := context.WithCancel(context.Background())

	spec := &helm.ChartSpec{
		ReleaseName: "my-release",
		ChartName:   "my-chart",
		Namespace:   "default",
	}

	transientErr := fmt.Errorf("install error: %w", errHelmConnectionResetByPeer)

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).Run(func(_ context.Context, _ *helm.ChartSpec) {
		// Cancel context after first failure to trigger cancellation path
		cancel()
	}).Return(nil, transientErr).Once()

	err := helm.InstallChartWithRetry(ctx, mockClient, spec, "test-repo")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "chart install retry cancelled")
}

func TestInstallChartWithRetry_MaxRetriesExhausted(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	ctx := context.Background()

	spec := &helm.ChartSpec{
		ReleaseName: "my-release",
		ChartName:   "my-chart",
		Namespace:   "default",
	}

	transientErr := fmt.Errorf("install error: %w", errHelmConnectionResetByPeer)

	// All 5 retry attempts fail with transient error.
	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).Return(nil, transientErr).Times(5)

	err := helm.InstallChartWithRetry(ctx, mockClient, spec, "test-repo")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install test-repo chart")
	assert.Contains(t, err.Error(), "connection reset by peer")
}

func TestInstallChartWithRetry_SuccessOnThirdAttempt(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	ctx := context.Background()

	spec := &helm.ChartSpec{
		ReleaseName: "my-release",
		ChartName:   "my-chart",
		Namespace:   "default",
	}

	transientErr := fmt.Errorf("install error: %w", errHelmConnectionResetByPeer)

	// First two calls fail, third succeeds.
	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).Return(nil, transientErr).Twice()

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).Return(&helm.ReleaseInfo{Name: "my-release"}, nil).Once()

	err := helm.InstallChartWithRetry(ctx, mockClient, spec, "test-repo")

	require.NoError(t, err)
}

func TestInstallOrUpgradeChart_SkipWaitDisablesWait(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	ctx := context.Background()
	timeout := 5 * time.Minute

	repoConfig := helm.RepoConfig{
		Name:     "test-repo",
		URL:      "https://charts.example.com",
		RepoName: "Test Repository",
	}

	chartConfig := helm.ChartConfig{
		ReleaseName: "test-release",
		ChartName:   "test-chart",
		Namespace:   "default",
		RepoURL:     "https://charts.example.com",
		SkipWait:    true,
	}

	mockClient.EXPECT().AddRepository(
		mock.Anything,
		mock.Anything,
		timeout,
	).Return(nil)

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return !spec.Wait && !spec.WaitForJobs
		}),
	).Return(&helm.ReleaseInfo{Name: "test-release"}, nil)

	err := helm.InstallOrUpgradeChart(ctx, mockClient, repoConfig, chartConfig, timeout)
	require.NoError(t, err)
}

func TestInstallOrUpgradeChart_ChartSpecFields(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	ctx := context.Background()
	timeout := 10 * time.Minute

	repoConfig := helm.RepoConfig{
		Name:     "calico",
		URL:      "https://docs.tigera.io/calico/charts",
		RepoName: "Calico",
	}

	chartConfig := helm.ChartConfig{
		ReleaseName:     "calico",
		ChartName:       "calico/tigera-operator",
		Namespace:       "tigera-operator",
		Version:         "v3.31.3",
		RepoURL:         "https://docs.tigera.io/calico/charts",
		CreateNamespace: true,
		SetJSONVals:     map[string]string{"installation.cni.type": `"Calico"`},
	}

	mockClient.EXPECT().AddRepository(
		mock.Anything,
		mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
			return entry.Name == "calico" &&
				entry.URL == "https://docs.tigera.io/calico/charts"
		}),
		timeout,
	).Return(nil)

	var gotSpec *helm.ChartSpec

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).RunAndReturn(func(_ context.Context, spec *helm.ChartSpec) (*helm.ReleaseInfo, error) {
		gotSpec = spec

		return &helm.ReleaseInfo{Name: "calico"}, nil
	})

	err := helm.InstallOrUpgradeChart(ctx, mockClient, repoConfig, chartConfig, timeout)
	require.NoError(t, err)
	require.NotNil(t, gotSpec)
	assert.Equal(t, "calico", gotSpec.ReleaseName)
	assert.Equal(t, "calico/tigera-operator", gotSpec.ChartName)
	assert.Equal(t, "tigera-operator", gotSpec.Namespace)
	assert.Equal(t, "v3.31.3", gotSpec.Version)
	assert.Equal(t, "https://docs.tigera.io/calico/charts", gotSpec.RepoURL)
	assert.True(t, gotSpec.CreateNamespace)
	assert.True(t, gotSpec.Atomic)
	assert.True(t, gotSpec.Silent)
	assert.True(t, gotSpec.UpgradeCRDs)
	assert.Equal(t, timeout, gotSpec.Timeout)
	assert.Equal(t, `"Calico"`, gotSpec.SetJSONVals["installation.cni.type"])
}

func TestContextTimeoutBuffer(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5*time.Minute, helm.ContextTimeoutBuffer)
}
