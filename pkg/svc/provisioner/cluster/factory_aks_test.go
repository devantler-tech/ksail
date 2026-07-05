package clusterprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	aksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/aks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aksTestCluster returns a cluster shaped for the AKS factory path.
func aksTestCluster() *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionAKS

	return cluster
}

// TestCreateAKSProvisionerWithConfig asserts a populated AKSConfig yields an
// AKS provisioner. The AKS client dials nothing at construction time (the
// DefaultAzureCredential chain resolves lazily), so no fake credentials are
// needed.
func TestCreateAKSProvisionerWithConfig(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			AKS: &clusterprovisioner.AKSConfig{
				Name:           "test-aks",
				SubscriptionID: "test-subscription",
				ResourceGroup:  "test-rg",
			},
		},
	}

	provisioner, config, err := factory.Create(context.Background(), aksTestCluster())
	require.NoError(t, err)
	assert.IsType(t, &aksprovisioner.Provisioner{}, provisioner)

	aksConfig, isAKSConfig := config.(*clusterprovisioner.AKSConfig)
	require.True(t, isAKSConfig)
	assert.Equal(t, "test-aks", aksConfig.GetClusterName())
}

// TestCreateAKSProvisionerWithoutSubscription asserts a missing subscription
// surfaces the AKS client's clear error.
func TestCreateAKSProvisionerWithoutSubscription(t *testing.T) {
	t.Parallel()

	factory := clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			AKS: &clusterprovisioner.AKSConfig{Name: "test-aks"},
		},
	}

	provisioner, _, err := factory.Create(context.Background(), aksTestCluster())
	require.Error(t, err)
	assert.Nil(t, provisioner)
}
