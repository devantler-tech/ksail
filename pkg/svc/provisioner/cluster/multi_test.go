package clusterprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
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

func TestCreateMinimalProvisioner_KWOK_Succeeds(t *testing.T) {
	t.Parallel()

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionKWOK,
		"test-kwok",
		"",
		"",
	)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.IsType(t, &kwokprovisioner.Provisioner{}, provisioner)
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

// TestCreateMinimalProvisioner_VanillaForwardsKubeconfigPath verifies richer
// callers can isolate Kind from the shared kubeconfig by asserting the
// kubeconfig path the minimal path hands to the Kind factory.
//
// It is not parallel: it swaps the package-level Kind provisioner factory.
//
//nolint:paralleltest // mutates the package-level Kind provisioner factory seam; must run serially.
func TestCreateMinimalProvisioner_VanillaForwardsKubeconfigPath(t *testing.T) {
	const kubeconfigPath = "/tmp/ephemeral-kind-kubeconfig"

	var gotKubeconfig string

	restore := clusterprovisioner.SetKindProvisionerFactory(
		func(_ *v1alpha4.Cluster, kubeconfig string) (*kindprovisioner.Provisioner, error) {
			gotKubeconfig = kubeconfig

			return &kindprovisioner.Provisioner{}, nil
		},
	)
	defer restore()

	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionVanilla,
		"test-kind",
		kubeconfigPath,
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	require.NotNil(t, provisioner)
	assert.Equal(t, kubeconfigPath, gotKubeconfig)
}

// TestCreateMinimalProvisioner_VanillaBuildsValidKindConfig verifies the
// minimal path still emits the TypeMeta required by Kind create, by asserting
// the kindConfig it hands to the Kind factory.
//
// It is not parallel: it swaps the package-level Kind provisioner factory.
//
//nolint:paralleltest // mutates the package-level Kind provisioner factory seam; must run serially.
func TestCreateMinimalProvisioner_VanillaBuildsValidKindConfig(t *testing.T) {
	const clusterName = "test-kind"

	var gotConfig *v1alpha4.Cluster

	restore := clusterprovisioner.SetKindProvisionerFactory(
		func(config *v1alpha4.Cluster, _ string) (*kindprovisioner.Provisioner, error) {
			gotConfig = config

			return &kindprovisioner.Provisioner{}, nil
		},
	)
	defer restore()

	_, err := clusterprovisioner.CreateMinimalProvisioner(
		v1alpha1.DistributionVanilla,
		clusterName,
		"/tmp/ephemeral-kind-kubeconfig",
		v1alpha1.ProviderDocker,
	)

	require.NoError(t, err)
	require.NotNil(t, gotConfig)
	assert.Equal(t, "kind.x-k8s.io/v1alpha4", gotConfig.APIVersion)
	assert.Equal(t, "Cluster", gotConfig.Kind)
	assert.Equal(t, clusterName, gotConfig.Name)
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
		"", // empty — should default to ProviderDocker, not return ErrUnsupportedProvider
	)

	require.Error(t, err)
	require.Nil(t, provisioner)
	// The error must originate from the Docker code path, proving the provider
	// defaulted to Docker (not an ErrUnsupportedProvider from an unknown provider).
	require.NotErrorIs(t, err, clusterprovisioner.ErrUnsupportedProvider)
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
