package helm

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/netretry"
)

const (
	// ContextTimeoutBuffer is the additional time added to the Helm timeout to ensure
	// the Go context doesn't cancel prematurely while Helm's kstatus wait is running.
	ContextTimeoutBuffer = 5 * time.Minute

	// Retry configuration for chart installation.
	// Transient 429/5xx errors can occur during Helm installs in CI with many
	// parallel jobs hitting the same chart registries.
	chartInstallMaxRetries    = 5
	chartInstallRetryBaseWait = 3 * time.Second
	chartInstallRetryMaxWait  = 30 * time.Second
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
		Version:         chartConfig.Version,
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

	return InstallChartWithRetry(timeoutCtx, client, spec, repoConfig.RepoName)
}

// InstallChartWithRetry attempts to install a chart, retrying on transient
// network errors (429 rate limits, 5xx server errors, connection resets, etc.).
// It is exported so that installers that bypass [InstallOrUpgradeChart] (e.g.
// OCI-based charts) can still benefit from retry logic.
func InstallChartWithRetry(
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

		if !netretry.IsRetryable(lastErr) || attempt == chartInstallMaxRetries {
			break
		}

		delay := netretry.ExponentialDelay(
			attempt,
			chartInstallRetryBaseWait,
			chartInstallRetryMaxWait,
		)

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
