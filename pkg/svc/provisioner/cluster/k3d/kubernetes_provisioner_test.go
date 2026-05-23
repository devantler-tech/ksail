package k3dprovisioner_test

import (
	"testing"

	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClusterCR_ServerArgs(t *testing.T) {
	t.Parallel()

	t.Run("propagates_server_args_into_cluster_spec", func(t *testing.T) {
		t.Parallel()

		serverArgs := k3dconfigmanager.APIServerFeatureGatesArgs()
		provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
			ServerArgs: serverArgs,
		})
		require.NoError(t, err)

		cluster := provisioner.BuildClusterCRForTest("test", "k3k-test", "10.0.0.1")

		assert.Equal(t, serverArgs, cluster.Spec.ServerArgs)
	})

	t.Run("omits_server_args_when_none_configured", func(t *testing.T) {
		t.Parallel()

		provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{})
		require.NoError(t, err)

		cluster := provisioner.BuildClusterCRForTest("test", "k3k-test", "10.0.0.1")

		assert.Nil(t, cluster.Spec.ServerArgs)
	})
}
