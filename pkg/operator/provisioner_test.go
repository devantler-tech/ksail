package operator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func clusterWithDistribution(name string, distribution v1alpha1.Distribution) *v1alpha1.Cluster {
	return &v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{Distribution: distribution},
		},
	}
}

func TestBuildDistributionConfig_VCluster(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(
		clusterWithDistribution("my-cluster", v1alpha1.DistributionVCluster),
	)
	require.NoError(t, err)
	require.NotNil(t, config.VCluster)
	assert.Equal(t, "my-cluster", config.VCluster.Name)
}

func TestBuildDistributionConfig_DefaultsToVanilla(t *testing.T) {
	t.Parallel()

	// An unset distribution is Vanilla per the API zero-value convention (its default serializes to
	// empty via omitzero), so the operator must not silently substitute a different distribution.
	config, err := operator.BuildDistributionConfig(clusterWithDistribution("c1", ""))
	require.NoError(t, err)
	require.NotNil(t, config.Kind)
	assert.Nil(t, config.VCluster)
	assert.Equal(t, "c1", config.Kind.Name)
}

func TestBuildDistributionConfig_Vanilla(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(
		clusterWithDistribution("c1", v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)
	require.NotNil(t, config.Kind)
	assert.Equal(t, "c1", config.Kind.Name)
}

func TestBuildDistributionConfig_TalosCapsKubernetesVersion(t *testing.T) {
	t.Parallel()

	// Talos 1.12 supports Kubernetes <= 1.35, so the built-in default must be capped.
	cluster := clusterWithDistribution("c1", v1alpha1.DistributionTalos)
	cluster.Spec.Cluster.Talos.Version = "v1.12.4"

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.Talos)
	assert.Equal(t, "1.35.0", config.Talos.KubernetesVersion(),
		"operator must cap the default Kubernetes version to the pinned Talos version")
}

func TestBuildDistributionConfig_TalosHonorsKubernetesVersionPin(t *testing.T) {
	t.Parallel()

	cluster := clusterWithDistribution("c1", v1alpha1.DistributionTalos)
	cluster.Spec.Cluster.KubernetesVersion = "v1.31.0"

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.Talos)
	assert.Equal(t, "1.31.0", config.Talos.KubernetesVersion())
}

func TestBuildDistributionConfig_TalosUsesPinnedVersionContract(t *testing.T) {
	t.Parallel()

	cluster := clusterWithDistribution("c1", v1alpha1.DistributionTalos)
	cluster.Spec.Cluster.Talos.Version = "v1.14.0-alpha.2"

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.Talos)

	apiServer := config.Talos.ControlPlane().K8sAPIServerConfig()
	require.NotNil(t, apiServer)
	assert.False(t, apiServer.InjectDefaultAuthorizers(),
		"Talos 1.14 must use the multi-document API server config")
}

func TestBuildDistributionConfig_K3s(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(
		clusterWithDistribution("c1", v1alpha1.DistributionK3s),
	)
	require.NoError(t, err)
	require.NotNil(t, config.K3d)
	assert.Equal(t, "c1", config.K3d.Name)
}

func TestBuildDistributionConfig_KWOK(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(
		clusterWithDistribution("c1", v1alpha1.DistributionKWOK),
	)
	require.NoError(t, err)
	require.NotNil(t, config.KWOK)
	assert.Equal(t, "c1", config.KWOK.Name)
}

func TestBuildDistributionConfig_Talos(t *testing.T) {
	t.Parallel()

	config, err := operator.BuildDistributionConfig(
		clusterWithDistribution("c1", v1alpha1.DistributionTalos),
	)
	require.NoError(t, err)
	require.NotNil(t, config.Talos)
	assert.Equal(t, "c1", config.Talos.GetClusterName())
}

func TestBuildDistributionConfig_EKS(t *testing.T) {
	// No t.Parallel(): this test uses t.Setenv, which is incompatible with parallel tests.
	cluster := clusterWithDistribution("c1", v1alpha1.DistributionEKS)
	cluster.Spec.Provider.AWS.RegionEnvVar = "KSAIL_TEST_REGION"

	t.Setenv("KSAIL_TEST_REGION", "eu-west-1")

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.EKS)
	assert.Equal(t, "c1", config.EKS.Name)
	assert.Equal(t, "eu-west-1", config.EKS.Region)
}

func TestBuildDistributionConfig_GKEIgnoresCustomEnvVarNames(t *testing.T) {
	// No t.Parallel(): this test uses t.Setenv, which is incompatible with parallel tests.
	cluster := clusterWithDistribution("c1", v1alpha1.DistributionGKE)
	cluster.Spec.Provider.GCP.ProjectEnvVar = "KSAIL_TEST_GCP_SECRET_PROJECT"
	cluster.Spec.Provider.GCP.LocationEnvVar = "KSAIL_TEST_GCP_SECRET_LOCATION"

	t.Setenv("GOOGLE_CLOUD_PROJECT", "safe-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "safe-location")
	t.Setenv("KSAIL_TEST_GCP_SECRET_PROJECT", "secret-project")
	t.Setenv("KSAIL_TEST_GCP_SECRET_LOCATION", "secret-location")

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.GKE)
	assert.Equal(t, "safe-project", config.GKE.Project)
	assert.Equal(t, "safe-location", config.GKE.Location)
}

func TestBuildDistributionConfig_GKE(t *testing.T) {
	// No t.Parallel(): this test uses t.Setenv, which is incompatible with parallel tests.
	cluster := clusterWithDistribution("c1", v1alpha1.DistributionGKE)

	t.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "europe-north1")

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.GKE)
	assert.Equal(t, "c1", config.GKE.Name)
	assert.Equal(t, "my-project", config.GKE.Project)
	assert.Equal(t, "europe-north1", config.GKE.Location)
}

func TestBuildDistributionConfig_AKS(t *testing.T) {
	// No t.Parallel(): this test uses t.Setenv, which is incompatible with parallel tests.
	cluster := clusterWithDistribution("c1", v1alpha1.DistributionAKS)
	cluster.Spec.Provider.Azure.SubscriptionIDEnvVar = "KSAIL_TEST_AZURE_SUBSCRIPTION"
	cluster.Spec.Provider.Azure.ResourceGroupEnvVar = "KSAIL_TEST_AZURE_RG"

	t.Setenv("KSAIL_TEST_AZURE_SUBSCRIPTION", "my-subscription")
	t.Setenv("KSAIL_TEST_AZURE_RG", "my-rg")

	config, err := operator.BuildDistributionConfig(cluster)
	require.NoError(t, err)
	require.NotNil(t, config.AKS)
	assert.Equal(t, "c1", config.AKS.Name)
	assert.Equal(t, "my-subscription", config.AKS.SubscriptionID)
	assert.Equal(t, "my-rg", config.AKS.ResourceGroup)
}

func TestBuildDistributionConfig_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := operator.BuildDistributionConfig(
		clusterWithDistribution("c1", v1alpha1.Distribution("Nonexistent")),
	)
	require.ErrorIs(t, err, operator.ErrUnsupportedDistribution)
}

func TestResolveProvider(t *testing.T) {
	t.Parallel()

	// Explicit provider is respected.
	explicit := clusterWithDistribution("c1", v1alpha1.DistributionVanilla)
	explicit.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	assert.Equal(t, v1alpha1.ProviderDocker, operator.ResolveProvider(explicit))

	// Unset provider defaults to Docker (the Provider zero value) for non-EKS distributions.
	assert.Equal(
		t,
		v1alpha1.ProviderDocker,
		operator.ResolveProvider(clusterWithDistribution("c1", v1alpha1.DistributionVCluster)),
	)

	// EKS defaults to AWS, its only provider.
	assert.Equal(
		t,
		v1alpha1.ProviderAWS,
		operator.ResolveProvider(clusterWithDistribution("c1", v1alpha1.DistributionEKS)),
	)

	// GKE defaults to GCP, its only provider.
	assert.Equal(
		t,
		v1alpha1.ProviderGCP,
		operator.ResolveProvider(clusterWithDistribution("c1", v1alpha1.DistributionGKE)),
	)

	// AKS defaults to Azure, its only provider.
	assert.Equal(
		t,
		v1alpha1.ProviderAzure,
		operator.ResolveProvider(clusterWithDistribution("c1", v1alpha1.DistributionAKS)),
	)
}
