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

	// chartInstallMaxRetries is the maximum number of retry attempts for chart
	// installation when transient network errors occur.
	chartInstallMaxRetries = 3
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

	return installChartWithRetry(timeoutCtx, client, spec, repoConfig.RepoName)
}

// installChartWithRetry attempts to install a chart, retrying on transient network errors.
func installChartWithRetry(
	ctx context.Context,
	client Interface,
	spec *ChartSpec,
	repoName string,
) error {
	var lastErr error

	for attempt := 1; attempt <= chartInstallMaxRetries; attempt++ {
		_, lastErr = client.InstallOrUpgradeChart(ctx, spec)
		if lastErr == nil {
			return nil
		}

		if !isRetryableNetworkError(lastErr) || attempt == chartInstallMaxRetries {
			break
		}

		delay := calculateRepoRetryDelay(attempt)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return fmt.Errorf("chart install retry cancelled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return fmt.Errorf("failed to install %s chart: %w", repoName, lastErr)
}
