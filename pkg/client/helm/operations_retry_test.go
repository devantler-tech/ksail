package helm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v6/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

	transientErr := fmt.Errorf("install error: %w",
		errors.New("read tcp 10.0.0.1:12345->1.2.3.4:443: read: connection reset by peer"),
	)

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

	transientErr := fmt.Errorf("install error: %w",
		errors.New("read tcp 10.0.0.1:12345->1.2.3.4:443: read: connection reset by peer"),
	)

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

	transientErr := fmt.Errorf("install error: %w",
		errors.New("read tcp 10.0.0.1:12345->1.2.3.4:443: read: connection reset by peer"),
	)

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

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ReleaseName == "calico" &&
				spec.ChartName == "calico/tigera-operator" &&
				spec.Namespace == "tigera-operator" &&
				spec.Version == "v3.31.3" &&
				spec.RepoURL == "https://docs.tigera.io/calico/charts" &&
				spec.CreateNamespace &&
				spec.Atomic &&
				spec.Silent &&
				spec.UpgradeCRDs &&
				spec.Timeout == timeout &&
				spec.SetJSONVals["installation.cni.type"] == `"Calico"`
		}),
	).Return(&helm.ReleaseInfo{Name: "calico"}, nil)

	err := helm.InstallOrUpgradeChart(ctx, mockClient, repoConfig, chartConfig, timeout)
	require.NoError(t, err)
}

func TestContextTimeoutBuffer(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5*time.Minute, helm.ContextTimeoutBuffer)
}
