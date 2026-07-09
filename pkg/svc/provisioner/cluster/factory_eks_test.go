package clusterprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eksTestCluster returns a cluster shaped for the EKS factory path.
func eksTestCluster() *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionEKS

	return cluster
}

// TestCreateEKSProvisionerWithConfig asserts a populated EKSConfig yields an EKS
// provisioner. The eksctl client shells out to the binary only at call time and
// the AWS provider resolves credentials lazily, so no live AWS access is needed
// at construction.
func TestCreateEKSProvisionerWithConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{
				Name:       "test-eks",
				Region:     "eu-west-1",
				ConfigPath: "eks.yaml",
			},
		},
	}

	provisioner, config, err := factory.Create(context.Background(), eksTestCluster())
	require.NoError(t, err)
	assert.IsType(t, &eksprovisioner.Provisioner{}, provisioner)

	eksConfig, isEKSConfig := config.(*clusterprovisioner.EKSConfig)
	require.True(t, isEKSConfig)
	assert.Equal(t, "test-eks", eksConfig.GetClusterName())
}

// TestCreateEKSProvisionerWithoutConfig asserts that selecting the EKS
// distribution without a pre-loaded EKSConfig surfaces
// ErrMissingDistributionConfig rather than a nil-pointer panic.
func TestCreateEKSProvisionerWithoutConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{},
	}

	provisioner, _, err := factory.Create(context.Background(), eksTestCluster())
	require.ErrorIs(t, err, clusterprovisioner.ErrMissingDistributionConfig)
	assert.Nil(t, provisioner)
}
