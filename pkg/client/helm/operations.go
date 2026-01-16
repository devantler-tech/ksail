package helm

import (
	"context"
	"fmt"
	"time"
)

const (
	// ContextTimeoutBuffer is the additional time added to the Helm timeout to ensure
	// the Go context doesn't cancel prematurely while Helm's kstatus wait is running.
	ContextTimeoutBuffer = 5 * time.Minute
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

	addRepoErr := client.AddRepository(ctx, repoEntry, timeout)
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
		Wait:            !chartConfig.SkipWait,
		WaitForJobs:     !chartConfig.SkipWait,
		SetJSONVals:     chartConfig.SetJSONVals,
	}

	// Set context deadline longer than Helm timeout to ensure Helm has
	// sufficient time to complete its kstatus-based wait operation.
	// Add 5 minutes buffer to the Helm timeout.
	contextTimeout := timeout + ContextTimeoutBuffer

	timeoutCtx, cancel := context.WithTimeout(ctx, contextTimeout)
	defer cancel()

	_, err := client.InstallOrUpgradeChart(timeoutCtx, spec)
	if err != nil {
		return fmt.Errorf("failed to install %s chart: %w", repoConfig.RepoName, err)
	}

	return nil
}
