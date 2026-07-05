package kubeadmhetzner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Vanilla (kubeadm) × Hetzner provisioner must satisfy the operator's Connector capability so
// InstallComponents installs components into the provisioned cluster instead of skipping it.
var _ clusterprovisioner.Connector = (*kubeadmhetzner.Provisioner)(nil)

func TestNewProvisionerWiresConnectorSecretPrefix(t *testing.T) {
	t.Setenv("KSAIL_KUBEADMHETZNER_CONNECTOR_TEST_TOKEN", "dummy-token")

	prov, err := kubeadmhetzner.NewProvisioner(
		"test-cluster",
		"test-kubeconfig",
		"v1.34.0",
		1,
		0,
		//nolint:gosec // G101 false positive: this is an env-var NAME, not a credential.
		v1alpha1.OptionsHetzner{TokenEnvVar: "KSAIL_KUBEADMHETZNER_CONNECTOR_TEST_TOKEN"},
	)
	require.NoError(t, err)

	assert.Equal(t, "vanilla-hetzner", prov.ConnectorSecretPrefix)
}
