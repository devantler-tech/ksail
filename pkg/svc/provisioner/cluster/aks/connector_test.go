package aksprovisioner_test

import (
	"testing"

	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	aksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/aks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The AKS provisioner exposes the operator's Connector capability.
var _ clusterprovisioner.Connector = (*aksprovisioner.Provisioner)(nil)

// provisionedCluster assembles a ManagedCluster in the given ARM provisioning
// state.
func provisionedCluster(state string) armcontainerservice.ManagedCluster {
	return armcontainerservice.ManagedCluster{
		Properties: &armcontainerservice.ManagedClusterProperties{
			ProvisioningState: new(state),
		},
	}
}

func TestKubeconfig_ReturnsARMServedKubeconfig(t *testing.T) {
	t.Parallel()

	kubeconfig := []byte("apiVersion: v1\nkind: Config\n")
	fake := &fakeClusterClient{
		cluster:    provisionedCluster("Succeeded"),
		kubeconfig: kubeconfig,
	}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	raw, err := provisioner.Kubeconfig(t.Context(), testClusterName)

	require.NoError(t, err)
	assert.Equal(t, kubeconfig, raw)
	require.Len(t, fake.gets, 1)
	assert.Equal(t, clusterCall{resourceGroup: testGroup, name: testClusterName}, fake.gets[0])
	require.Len(t, fake.credentials, 1)
	assert.Equal(
		t, clusterCall{resourceGroup: testGroup, name: testClusterName}, fake.credentials[0],
	)
}

func TestKubeconfig_ResolvesResourceGroupWhenUnpinned(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{
		clusters: []*armcontainerservice.ManagedCluster{
			managedCluster(testClusterName, testGroup),
		},
		cluster:    provisionedCluster("Succeeded"),
		kubeconfig: []byte("kubeconfig"),
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	_, err := provisioner.Kubeconfig(t.Context(), testClusterName)

	require.NoError(t, err)
	require.Len(t, fake.credentials, 1)
	assert.Equal(t, testGroup, fake.credentials[0].resourceGroup)
}

func TestKubeconfig_NotReadyWhileProvisioning(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{cluster: provisionedCluster("Creating")}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	_, err := provisioner.Kubeconfig(t.Context(), testClusterName)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
	require.ErrorContains(t, err, "Creating")
	assert.Empty(t, fake.credentials)
}

func TestKubeconfig_NotReadyWithoutProvisioningState(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{cluster: armcontainerservice.ManagedCluster{}}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	_, err := provisioner.Kubeconfig(t.Context(), testClusterName)

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
	require.ErrorContains(t, err, "unknown")
	assert.Empty(t, fake.credentials)
}

func TestKubeconfig_RequiresClusterName(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner, err := aksprovisioner.NewProvisioner("", testGroup, nil, fake, nil)
	require.NoError(t, err)

	_, err = provisioner.Kubeconfig(t.Context(), "")

	require.ErrorIs(t, err, aksprovisioner.ErrClusterNotFound)
	assert.Empty(t, fake.gets)
}

func TestKubeconfig_WrapsGetClusterError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{getErr: errBoom}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	_, err := provisioner.Kubeconfig(t.Context(), testClusterName)

	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "aks get cluster")
	assert.Empty(t, fake.credentials)
}

func TestKubeconfig_WrapsCredentialsError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{
		cluster:        provisionedCluster("Succeeded"),
		credentialsErr: errBoom,
	}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	_, err := provisioner.Kubeconfig(t.Context(), testClusterName)

	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "aks cluster user credentials")
}
