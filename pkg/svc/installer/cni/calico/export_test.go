package calicoinstaller

import "context"

// SetAPIServerCheckerForTest overrides the API server stability checker for unit testing.
// This avoids needing a live Kubernetes cluster when testing the Install path.
func (c *Installer) SetAPIServerCheckerForTest(fn func(ctx context.Context) error) {
	c.apiServerChecker = fn
}

// SetRetryBackoffForTest overrides the retry backoff function for unit testing.
func (c *Installer) SetRetryBackoffForTest(fn func(ctx context.Context) error) {
	c.retryBackoff = fn
}
