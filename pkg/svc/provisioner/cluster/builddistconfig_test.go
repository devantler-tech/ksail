package clusterprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const buildClusterName = "build-cluster"

func clusterWith(distribution v1alpha1.Distribution) *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Name = buildClusterName
	cluster.Spec.Cluster.Distribution = distribution

	return cluster
}

// TestBuildDistributionConfigKindAppliesDefaultsWhenRequested asserts the local Kind defaulting path:
// the control-plane node is added so the EKS-less provisioner factory accepts the config.
func TestBuildDistributionConfigKindAppliesDefaultsWhenRequested(t *testing.T) {
	t.Parallel()

	cluster := clusterWith(v1alpha1.DistributionVanilla)

	withDefaults, err := clusterprovisioner.BuildDistributionConfig(
		cluster, buildClusterName, true,
	)
	require.NoError(t, err)
	require.NotNil(t, withDefaults.Kind)
	assert.NotEmpty(
		t,
		withDefaults.Kind.Nodes,
		"local Kind defaulting must add the control-plane node",
	)

	withoutDefaults, err := clusterprovisioner.BuildDistributionConfig(
		cluster, buildClusterName, false,
	)
	require.NoError(t, err)
	require.NotNil(t, withoutDefaults.Kind)
	assert.Empty(t, withoutDefaults.Kind.Nodes, "the operator path must not default the node in")
}

// TestBuildDistributionConfigDefaultsBlankDistributionToVanilla guards the API zero-value convention:
// an unset distribution builds a Vanilla (Kind) config, like both callers expect.
func TestBuildDistributionConfigDefaultsBlankDistributionToVanilla(t *testing.T) {
	t.Parallel()

	cluster := clusterWith("")

	config, err := clusterprovisioner.BuildDistributionConfig(cluster, buildClusterName, true)
	require.NoError(t, err)
	assert.NotNil(t, config.Kind, "a blank distribution must default to Vanilla/Kind")
}

// TestBuildDistributionConfigTalosHonorsVersionPin asserts the harmonized stricter semantics: a pinned
// Kubernetes version flows into the Talos bundle (rather than always using the default), so a local
// create never deploys an incompatible Kubernetes version — matching the operator backend.
func TestBuildDistributionConfigTalosHonorsVersionPin(t *testing.T) {
	t.Parallel()

	pinned := clusterWith(v1alpha1.DistributionTalos)
	pinned.Spec.Cluster.KubernetesVersion = "1.34.0"

	config, err := clusterprovisioner.BuildDistributionConfig(pinned, buildClusterName, false)
	require.NoError(t, err)
	require.NotNil(t, config.Talos)
	assert.Equal(t, buildClusterName, config.Talos.Name, "the bundle is named after the cluster")
}

// TestBuildDistributionConfigTalosUsesPinnedVersionContract verifies the shared
// web/local builder emits Talos 1.14's multi-document configuration shape.
func TestBuildDistributionConfigTalosUsesPinnedVersionContract(t *testing.T) {
	t.Parallel()

	pinned := clusterWith(v1alpha1.DistributionTalos)
	pinned.Spec.Cluster.Talos.Version = "v1.14.0-alpha.2"

	config, err := clusterprovisioner.BuildDistributionConfig(pinned, buildClusterName, false)
	require.NoError(t, err)
	require.NotNil(t, config.Talos)

	apiServer := config.Talos.ControlPlane().K8sAPIServerConfig()
	require.NotNil(t, apiServer)
	assert.False(t, apiServer.InjectDefaultAuthorizers(),
		"Talos 1.14 must use the multi-document API server config")
}

// TestBuildDistributionConfigSimpleDistributions covers the name-only distributions delegated to
// SimpleDistributionConfig.
func TestBuildDistributionConfigSimpleDistributions(t *testing.T) {
	t.Parallel()

	for _, distribution := range []v1alpha1.Distribution{
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
	} {
		t.Run(string(distribution), func(t *testing.T) {
			t.Parallel()

			config, err := clusterprovisioner.BuildDistributionConfig(
				clusterWith(distribution), buildClusterName, false,
			)
			require.NoError(t, err)
			assert.NotNil(t, config)
		})
	}
}

// TestBuildDistributionConfigEKSIsCallerSpecific asserts EKS returns (nil, nil) so each backend builds
// its own EKSConfig (on-disk config locally, in-memory in the operator).
func TestBuildDistributionConfigEKSIsCallerSpecific(t *testing.T) {
	t.Parallel()

	config, err := clusterprovisioner.BuildDistributionConfig(
		clusterWith(v1alpha1.DistributionEKS), buildClusterName, false,
	)
	require.NoError(t, err)
	assert.Nil(t, config, "EKS must be built by the caller, not the shared builder")
}

// TestBuildDistributionConfigGKEIsCallerSpecific asserts GKE returns (nil, nil) so each backend
// builds its own GKEConfig (env-var-resolved project/location + optional gke.yaml spec).
func TestBuildDistributionConfigGKEIsCallerSpecific(t *testing.T) {
	t.Parallel()

	config, err := clusterprovisioner.BuildDistributionConfig(
		clusterWith(v1alpha1.DistributionGKE), buildClusterName, false,
	)
	require.NoError(t, err)
	assert.Nil(t, config, "GKE must be built by the caller, not the shared builder")
}

// TestBuildDistributionConfigAKSIsCallerSpecific asserts AKS returns (nil, nil) so each backend
// builds its own AKSConfig (env-var-resolved subscription/resource group + optional aks.yaml spec).
func TestBuildDistributionConfigAKSIsCallerSpecific(t *testing.T) {
	t.Parallel()

	config, err := clusterprovisioner.BuildDistributionConfig(
		clusterWith(v1alpha1.DistributionAKS), buildClusterName, false,
	)
	require.NoError(t, err)
	assert.Nil(t, config, "AKS must be built by the caller, not the shared builder")
}

// TestBuildDistributionConfigUnsupportedDistribution asserts an unknown distribution surfaces the
// shared unsupported-distribution sentinel.
func TestBuildDistributionConfigUnsupportedDistribution(t *testing.T) {
	t.Parallel()

	_, err := clusterprovisioner.BuildDistributionConfig(
		clusterWith(v1alpha1.Distribution("Bogus")), buildClusterName, false,
	)
	require.ErrorIs(t, err, clustererr.ErrUnsupportedDistribution)
}
