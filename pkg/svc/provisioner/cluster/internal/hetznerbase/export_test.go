package hetznerbase

import "context"

// PublishConnectorKubeconfigForTest exposes publishConnectorKubeconfig for unit testing.
func (b *Base) PublishConnectorKubeconfigForTest(
	ctx context.Context, clusterName string, kubeconfig []byte,
) error {
	return b.publishConnectorKubeconfig(ctx, clusterName, kubeconfig)
}

// DeleteConnectorKubeconfigForTest exposes deleteConnectorKubeconfig for unit testing.
func (b *Base) DeleteConnectorKubeconfigForTest(ctx context.Context, clusterName string) error {
	return b.deleteConnectorKubeconfig(ctx, clusterName)
}

// ConnectorSecretNameForTest exposes connectorSecretName for unit testing.
func (b *Base) ConnectorSecretNameForTest(clusterName string) string {
	return b.connectorSecretName(clusterName)
}
