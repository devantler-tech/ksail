package helm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	helmv4cli "helm.sh/helm/v4/pkg/cli"
	helmv4getter "helm.sh/helm/v4/pkg/getter"
	repov1 "helm.sh/helm/v4/pkg/repo/v1"
)

const (
	repoDirMode  = 0o750
	repoFileMode = 0o640

	// Retry configuration for repository index downloads.
	// External Helm repositories may experience transient 5xx errors.
	repoIndexMaxRetries    = 3
	repoIndexRetryBaseWait = 2 * time.Second
	repoIndexRetryMaxWait  = 15 * time.Second
)

// httpStatusCodePattern matches HTTP 5xx status codes at word boundaries
// to avoid false positives on port numbers like ":5000".
var httpStatusCodePattern = regexp.MustCompile(`\b50[0-4]\b`)

var (
	errRepositoryEntryRequired = errors.New("helm: repository entry is required")
	errRepositoryNameRequired  = errors.New("helm: repository name is required")
	errRepositoryCacheUnset    = errors.New("helm: repository cache path is not set")
	errRepositoryConfigUnset   = errors.New("helm: repository config path is not set")
)

// AddRepository registers a Helm repository for the current client instance.
// The timeout parameter controls how long HTTP requests for downloading the repository index can take.
func (c *Client) AddRepository(
	ctx context.Context,
	entry *RepositoryEntry,
	timeout time.Duration,
) error {
	requestErr := validateRepositoryRequest(ctx, entry)
	if requestErr != nil {
		return requestErr
	}

	settings := c.settings

	repoFile, err := ensureRepositoryConfig(settings)
	if err != nil {
		return err
	}

	repositoryFile := loadOrInitRepositoryFile(repoFile)
	repoEntry := convertRepositoryEntry(entry)

	repoCache, err := ensureRepositoryCache(settings)
	if err != nil {
		return err
	}

	chartRepository, err := newChartRepository(settings, repoEntry, repoCache, timeout)
	if err != nil {
		return err
	}

	downloadErr := downloadRepositoryIndex(ctx, chartRepository)
	if downloadErr != nil {
		return downloadErr
	}

	repositoryFile.Update(repoEntry)

	writeErr := repositoryFile.WriteFile(repoFile, repoFileMode)
	if writeErr != nil {
		return fmt.Errorf("write repository file: %w", writeErr)
	}

	return nil
}

func validateRepositoryRequest(ctx context.Context, entry *RepositoryEntry) error {
	if entry == nil {
		return errRepositoryEntryRequired
	}

	if entry.Name == "" {
		return errRepositoryNameRequired
	}

	ctxErr := ctx.Err()
	if ctxErr != nil {
		return fmt.Errorf("add repository context cancelled: %w", ctxErr)
	}

	return nil
}

func ensureRepositoryConfig(settings *helmv4cli.EnvSettings) (string, error) {
	repoFile := settings.RepositoryConfig

	envRepoConfig := os.Getenv("HELM_REPOSITORY_CONFIG")
	if envRepoConfig != "" {
		repoFile = envRepoConfig
		settings.RepositoryConfig = envRepoConfig
	}

	if repoFile == "" {
		return "", errRepositoryConfigUnset
	}

	repoDir := filepath.Dir(repoFile)

	mkdirErr := os.MkdirAll(repoDir, repoDirMode)
	if mkdirErr != nil {
		return "", fmt.Errorf("create repository directory: %w", mkdirErr)
	}

	return repoFile, nil
}

func loadOrInitRepositoryFile(repoFile string) *repov1.File {
	repositoryFile, err := repov1.LoadFile(repoFile)
	if err != nil {
		return repov1.NewFile()
	}

	return repositoryFile
}

func convertRepositoryEntry(entry *RepositoryEntry) *repov1.Entry {
	return &repov1.Entry{
		Name:                  entry.Name,
		URL:                   entry.URL,
		Username:              entry.Username,
		Password:              entry.Password,
		CertFile:              entry.CertFile,
		KeyFile:               entry.KeyFile,
		CAFile:                entry.CaFile,
		InsecureSkipTLSVerify: entry.InsecureSkipTLSverify,
	}
}

func ensureRepositoryCache(settings *helmv4cli.EnvSettings) (string, error) {
	repoCache := settings.RepositoryCache

	if envCache := os.Getenv("HELM_REPOSITORY_CACHE"); envCache != "" {
		repoCache = envCache
		settings.RepositoryCache = envCache
	}

	if repoCache == "" {
		return "", errRepositoryCacheUnset
	}

	mkdirCacheErr := os.MkdirAll(repoCache, repoDirMode)
	if mkdirCacheErr != nil {
		return "", fmt.Errorf("create repository cache directory: %w", mkdirCacheErr)
	}

	return repoCache, nil
}

func newChartRepository(
	settings *helmv4cli.EnvSettings,
	repoEntry *repov1.Entry,
	repoCache string,
	timeout time.Duration,
) (*repov1.ChartRepository, error) {
	// Use getter.WithTimeout to configure HTTP timeout for repository index downloads.
	// This prevents hangs when the repository server is slow to respond.
	getterOpts := []helmv4getter.Option{}
	if timeout > 0 {
		getterOpts = append(getterOpts, helmv4getter.WithTimeout(timeout))
	}

	chartRepository, err := repov1.NewChartRepository(
		repoEntry,
		helmv4getter.All(settings, getterOpts...),
	)
	if err != nil {
		return nil, fmt.Errorf("create chart repository: %w", err)
	}

	chartRepository.CachePath = repoCache

	return chartRepository, nil
}

func downloadRepositoryIndex(ctx context.Context, chartRepository *repov1.ChartRepository) error {
	var lastErr error

	for attempt := 1; attempt <= repoIndexMaxRetries; attempt++ {
		indexPath, err := chartRepository.DownloadIndexFile()
		if err == nil {
			_, statErr := os.Stat(indexPath)
			if statErr == nil {
				return nil
			}

			lastErr = fmt.Errorf("failed to verify repository index file: %w", statErr)
		} else {
			lastErr = fmt.Errorf("failed to download repository index file: %w", err)
		}

		// Check if this is a retryable transient network error
		if !isRetryableNetworkError(lastErr) || attempt == repoIndexMaxRetries {
			break
		}

		// Calculate delay with exponential backoff
		delay := calculateRepoRetryDelay(attempt)

		// Use a timer so the retry loop respects context cancellation
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()

			return fmt.Errorf("download repository index cancelled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return lastErr
}

// isRetryableNetworkError returns true if the error indicates a transient network error
// that should be retried. This covers HTTP 5xx status codes and TCP-level errors
// such as connection resets, timeouts, and unexpected EOF.
func isRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// HTTP 5xx status text patterns
	textPatterns := []string{
		"Internal Server Error", "Bad Gateway",
		"Service Unavailable", "Gateway Timeout",
		// TCP-level transient network errors
		"connection reset by peer", "connection refused",
		"i/o timeout", "TLS handshake timeout",
		"unexpected EOF", "no such host",
	}

	for _, pattern := range textPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	// Match HTTP 5xx numeric codes at word boundaries to avoid false positives
	// on port numbers like ":5000". Uses regexp for precise matching.
	return httpStatusCodePattern.MatchString(errMsg)
}

// calculateRepoRetryDelay returns the delay for the given retry attempt.
// Uses exponential backoff: 2s, 4s, 8s... capped at repoIndexRetryMaxWait.
func calculateRepoRetryDelay(attempt int) time.Duration {
	return min(repoIndexRetryBaseWait*time.Duration(1<<(attempt-1)), repoIndexRetryMaxWait)
}
