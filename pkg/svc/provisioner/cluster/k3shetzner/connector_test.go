package k3shetzner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3shetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The K3s × Hetzner provisioner must satisfy the operator's Connector capability so
// InstallComponents installs components into the provisioned cluster instead of skipping it.
var _ clusterprovisioner.Connector = (*k3shetzner.Provisioner)(nil)

func TestNewProvisionerWiresConnectorSecretPrefix(t *testing.T) {
	t.Setenv("KSAIL_K3SHETZNER_CONNECTOR_TEST_TOKEN", "dummy-token")

	prov, err := k3shetzner.NewProvisioner(
		"test-cluster",
		"test-kubeconfig",
		"v1.36.1+k3s1",
		1,
		0,
		//nolint:gosec // G101 false positive: this is an env-var NAME, not a credential.
		v1alpha1.OptionsHetzner{TokenEnvVar: "KSAIL_K3SHETZNER_CONNECTOR_TEST_TOKEN"},
	)
	require.NoError(t, err)

	assert.Equal(t, "k3s-hetzner", prov.ConnectorSecretPrefix)
}
