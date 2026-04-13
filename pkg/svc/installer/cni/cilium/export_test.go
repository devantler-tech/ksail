package ciliuminstaller

import (
	"context"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// SetGatewayAPICRDInstaller overrides the Gateway API CRD installer function for testing.
func (c *Installer) SetGatewayAPICRDInstaller(fn GatewayAPICRDInstallerFunc) {
	c.gatewayAPICRDInstaller = fn
}

// SetAPIServerCheckerForTest overrides the API server stability checker for unit testing.
// This avoids needing a live Kubernetes cluster when testing the Install path.
func (c *Installer) SetAPIServerCheckerForTest(fn func(ctx context.Context) error) {
	c.apiServerChecker = fn
}

// ParseGatewayAPICRDs exports parseGatewayAPICRDs for testing.
func ParseGatewayAPICRDs(data []byte) ([]apiextensionsv1.CustomResourceDefinition, error) {
	return parseGatewayAPICRDs(data)
}

// FetchGatewayAPICRDs exports fetchGatewayAPICRDs for testing.
func FetchGatewayAPICRDs(
	ctx context.Context,
	url string,
	timeout time.Duration,
) ([]apiextensionsv1.CustomResourceDefinition, error) {
	return fetchGatewayAPICRDs(ctx, url, timeout)
}

// FetchGatewayAPICRDsWithRetryForTest exports the configurable retry helper for testing.
func FetchGatewayAPICRDsWithRetryForTest(
	ctx context.Context,
	url string,
	timeout time.Duration,
	maxRetries int,
	baseWait, maxWait time.Duration,
) ([]apiextensionsv1.CustomResourceDefinition, error) {
	return fetchGatewayAPICRDsWithRetry(ctx, url, timeout, maxRetries, baseWait, maxWait)
}
