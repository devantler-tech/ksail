//nolint:gochecknoglobals // export_test.go pattern requires a global to expose the internal seam.
package kubeconform

import "time"

// ValidateWithRetry exposes validateWithRetry for black-box tests.
var ValidateWithRetry = (*Client).validateWithRetry

// SetRetryConfig overrides the internal retry-tuning fields for black-box tests.
func (c *Client) SetRetryConfig(maxAttempts int, baseWait, maxWait time.Duration) {
	c.maxRetryAttempts = maxAttempts
	c.retryBaseWait = baseWait
	c.retryMaxWait = maxWait
}

// MaxRetryAttempts exposes maxRetryAttempts for black-box test assertions.
func (c *Client) MaxRetryAttempts() int {
	return c.maxRetryAttempts
}
