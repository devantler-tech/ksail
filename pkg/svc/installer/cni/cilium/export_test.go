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
