package kwokprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/nested"
)

// Kubeconfig implements the clusterprovisioner.Connector capability for a nested KWOK cluster: it
// returns the kubeconfig published at create time (see publishConnectorKubeconfig), with the API
// server already pointed at the operator-reachable exposure address, so the ksail operator can
// install components into the child cluster. It returns clustererr.ErrKubeconfigNotReady (via
// nested.ConnectorKubeconfig) when the Secret has not been published yet, so the caller requeues.
func (p *KubernetesProvisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	target := p.resolveName(name)

	conn := ConnectionFor(target)

	raw, err := nested.ConnectorKubeconfig(
		ctx, p.hostClientset, target,
		conn.Namespace, conn.SecretName, kwokKubeconfigKey,
	)
	if err != nil {
		return nil, fmt.Errorf("kwok: %w", err)
	}

	return raw, nil
}
