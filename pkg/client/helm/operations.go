package helm

import (
	"context"
	"fmt"
	"time"
)

// InstallOrUpgradeChart performs a Helm install or upgrade operation.
func InstallOrUpgradeChart(
	ctx context.Context,
	client Interface,
	repoConfig RepoConfig,
	chartConfig ChartConfig,
	timeout time.Duration,
) error {
	repoEntry := &RepositoryEntry{
		Name: repoConfig.Name,
		URL:  repoConfig.URL,
	}

	addRepoErr := client.AddRepository(ctx, repoEntry)
	if addRepoErr != nil {
		return fmt.Errorf("failed to add %s repository: %w", repoConfig.RepoName, addRepoErr)
	}

	spec := &ChartSpec{
		ReleaseName:     chartConfig.ReleaseName,
		ChartName:       chartConfig.ChartName,
		Namespace:       chartConfig.Namespace,
		RepoURL:         chartConfig.RepoURL,
		CreateNamespace: chartConfig.CreateNamespace,
		Atomic:          true,
		Silent:          true,
		UpgradeCRDs:     true,
		Timeout:         timeout,
		Wait:            true,
		WaitForJobs:     true,
		SetJSONVals:     chartConfig.SetJSONVals,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := client.InstallOrUpgradeChart(timeoutCtx, spec)
	if err != nil {
		return fmt.Errorf("failed to install %s chart: %w", repoConfig.RepoName, err)
	}

	return nil
}
