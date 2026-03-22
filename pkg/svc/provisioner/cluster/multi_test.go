package clusterprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMultiProvisioner(t *testing.T) {
	t.Parallel()

	mp := clusterprovisioner.NewMultiProvisioner("test-cluster")

	require.NotNil(t, mp)
}

func TestMultiProvisioner_Create_ReturnsErrCreateNotSupported(t *testing.T) {
	t.Parallel()

	mp := clusterprovisioner.NewMultiProvisioner("test-cluster")

	err := mp.Create(context.Background(), "test-cluster")

	require.Error(t, err)
	assert.ErrorIs(t, err, clustererr.ErrCreateNotSupported)
}

func TestCreateMinimalProvisioner_K3s_Succeeds(t *testing.T) {
	t.Parallel()

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionK3s,
		"test-k3s",
		"",
		"",
	)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.IsType(t, &k3dprovisioner.Provisioner{}, provisioner)
}

func TestCreateMinimalProvisioner_DefaultsProviderToDocker(t *testing.T) {
	t.Parallel()

	// K3d does not call Docker so we can test with the default provider
	// without requiring a live Docker daemon.
	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionK3s,
		"default-provider-test",
		"",
		v1alpha1.Provider(""), // empty provider — should default to Docker internally
	)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
}

func TestCreateMinimalProvisioner_UnsupportedDistribution(t *testing.T) {
	t.Parallel()

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.Distribution("unknown-distribution"),
		"test",
		"",
		"",
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	assert.ErrorIs(t, err, clusterprovisioner.ErrUnsupportedDistribution)
}

func TestCreateMinimalProvisioner_VanillaDockerError(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionVanilla,
		"test-kind",
		"",
		"",
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	assert.Contains(t, err.Error(), "failed to create Kind provisioner")
}

func TestCreateMinimalProvisioner_TalosDockerError(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionTalos,
		"test-talos",
		"",
		"",
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	assert.Contains(t, err.Error(), "failed to create Talos provisioner")
}

func TestCreateMinimalProvisioner_VClusterDockerError(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionVCluster,
		"test-vcluster",
		"",
		"",
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	assert.Contains(t, err.Error(), "failed to create VCluster provisioner")
}
