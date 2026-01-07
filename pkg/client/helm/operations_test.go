package helm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Error variables for test cases.
var (
	errRepositoryConnection    = errors.New("failed to connect to repository")
	errChartInstallationFailed = errors.New("chart installation failed")
)

func TestInstallOrUpgradeChart_Success(t *testing.T) {
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
		ReleaseName:     "test-release",
		ChartName:       "test-chart",
		Namespace:       "default",
		RepoURL:         "https://charts.example.com",
		CreateNamespace: true,
		SetJSONVals:     map[string]string{"key": "value"},
	}

	mockClient.EXPECT().AddRepository(
		mock.Anything,
		mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
			return entry.Name == "test-repo" && entry.URL == "https://charts.example.com"
		}),
		timeout,
	).Return(nil)

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ReleaseName == "test-release" &&
				spec.ChartName == "test-chart" &&
				spec.Namespace == "default" &&
				spec.CreateNamespace &&
				spec.Atomic &&
				spec.Silent &&
				spec.Wait &&
				spec.WaitForJobs
		}),
	).Return(&helm.ReleaseInfo{Name: "test-release"}, nil)

	err := helm.InstallOrUpgradeChart(ctx, mockClient, repoConfig, chartConfig, timeout)
	require.NoError(t, err)
}

func TestInstallOrUpgradeChart_AddRepositoryError(t *testing.T) {
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
	}

	mockClient.EXPECT().AddRepository(
		mock.Anything,
		mock.Anything,
		timeout,
	).Return(fmt.Errorf("repo error: %w", errRepositoryConnection))

	err := helm.InstallOrUpgradeChart(ctx, mockClient, repoConfig, chartConfig, timeout)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add Test Repository repository")
	assert.Contains(t, err.Error(), "failed to connect to repository")
}

func TestInstallOrUpgradeChart_InstallError(t *testing.T) {
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
	}

	mockClient.EXPECT().AddRepository(
		mock.Anything,
		mock.Anything,
		timeout,
	).Return(nil)

	mockClient.EXPECT().InstallOrUpgradeChart(
		mock.Anything,
		mock.Anything,
	).Return(nil, fmt.Errorf("install error: %w", errChartInstallationFailed))

	err := helm.InstallOrUpgradeChart(ctx, mockClient, repoConfig, chartConfig, timeout)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install Test Repository chart")
	assert.Contains(t, err.Error(), "chart installation failed")
}

func TestRepoConfig_Fields(t *testing.T) {
	t.Parallel()

	config := helm.RepoConfig{
		Name:     "my-repo",
		URL:      "https://repo.example.com",
		RepoName: "My Repository",
	}

	require.Equal(t, "my-repo", config.Name)
	require.Equal(t, "https://repo.example.com", config.URL)
	require.Equal(t, "My Repository", config.RepoName)
}

func TestChartConfig_Fields(t *testing.T) {
	t.Parallel()

	config := helm.ChartConfig{
		ReleaseName:     "release",
		ChartName:       "chart",
		Namespace:       "ns",
		RepoURL:         "https://url",
		CreateNamespace: true,
		SetJSONVals:     map[string]string{"key": "val"},
	}

	require.Equal(t, "release", config.ReleaseName)
	require.Equal(t, "chart", config.ChartName)
	require.Equal(t, "ns", config.Namespace)
	require.Equal(t, "https://url", config.RepoURL)
	require.True(t, config.CreateNamespace)
	require.Equal(t, map[string]string{"key": "val"}, config.SetJSONVals)
}
