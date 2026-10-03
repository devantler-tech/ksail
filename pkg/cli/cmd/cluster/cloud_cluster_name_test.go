package cluster_test

import (
	"slices"
	"testing"

	"cloud.google.com/go/container/apiv1/containerpb"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Contexts the cloud tooling writes for a cluster named cloudClusterName:
// gcloud qualifies the name with the project and location, and
// `az aks get-credentials --admin` appends "-admin". Neither is the cluster name.
const (
	cloudClusterName = "named"
	gcloudContext    = "gke_my-project_europe-north1_" + cloudClusterName
	aksAdminContext  = cloudClusterName + "-admin"
)

// cloudNameCase is one GKE or AKS configuration and the cluster name create,
// diff and update must resolve for it.
type cloudNameCase struct {
	name         string
	distribution v1alpha1.Distribution
	// context is spec.cluster.connection.context as configured.
	context string
	// configName is the cluster name the loaded GKE or AKS configuration holds:
	// read from the distribution config file, or parsed from the context when no
	// file names the cluster. Empty means no configuration was loaded.
	configName string
	// metadataName is metadata.name. When set it is applied as the name
	// override, as the commands do before they resolve the cluster name.
	metadataName string
	want         string
}

// cloudContextCases are the contexts the cloud tooling writes, with and without
// a configured metadata.name.
func cloudContextCases() []cloudNameCase {
	return []cloudNameCase{
		{
			name:         "GKE gcloud context",
			distribution: v1alpha1.DistributionGKE,
			context:      gcloudContext,
			configName:   cloudClusterName,
			want:         cloudClusterName,
		},
		{
			name:         "GKE gcloud context with a configured name",
			distribution: v1alpha1.DistributionGKE,
			context:      gcloudContext,
			configName:   cloudClusterName,
			metadataName: cloudClusterName,
			want:         cloudClusterName,
		},
		{
			name:         "AKS admin context",
			distribution: v1alpha1.DistributionAKS,
			context:      aksAdminContext,
			configName:   cloudClusterName,
			want:         cloudClusterName,
		},
		{
			name:         "AKS admin context with a configured name",
			distribution: v1alpha1.DistributionAKS,
			context:      aksAdminContext,
			configName:   cloudClusterName,
			metadataName: cloudClusterName,
			want:         cloudClusterName,
		},
	}
}

// cloudConfigurationCases cover the configuration itself: a name override
// renames it, its name is trimmed, and without one the cluster-level fallback
// still applies.
func cloudConfigurationCases() []cloudNameCase {
	return []cloudNameCase{
		{
			name:         "GKE configured name renames the configuration",
			distribution: v1alpha1.DistributionGKE,
			context:      gcloudContext,
			configName:   "old",
			metadataName: cloudClusterName,
			want:         cloudClusterName,
		},
		{
			name:         "AKS configured name renames the configuration",
			distribution: v1alpha1.DistributionAKS,
			context:      aksAdminContext,
			configName:   "old",
			metadataName: cloudClusterName,
			want:         cloudClusterName,
		},
		{
			name:         "GKE configuration name is trimmed",
			distribution: v1alpha1.DistributionGKE,
			context:      gcloudContext,
			configName:   " " + cloudClusterName + " ",
			want:         cloudClusterName,
		},
		{
			name:         "AKS configuration name is trimmed",
			distribution: v1alpha1.DistributionAKS,
			context:      aksAdminContext,
			configName:   " " + cloudClusterName + " ",
			want:         cloudClusterName,
		},
		{
			name:         "GKE without a loaded configuration falls back to the context",
			distribution: v1alpha1.DistributionGKE,
			context:      "custom-context",
			want:         "custom-context",
		},
		{
			name:         "AKS without a loaded configuration falls back to the context",
			distribution: v1alpha1.DistributionAKS,
			context:      "custom-context",
			want:         "custom-context",
		},
	}
}

// TestResolveClusterNameFromContext_CloudConfigurationName pins that GKE and AKS
// take their cluster name from the GKE or AKS configuration rather than the
// kubeconfig context. The context only says how to reach the cluster: gcloud
// qualifies it with the project and location, and admin credentials add a
// suffix, so reading the name from it makes update look for a cluster that does
// not exist.
func TestResolveClusterNameFromContext_CloudConfigurationName(t *testing.T) {
	t.Parallel()

	for _, testCase := range slices.Concat(cloudContextCases(), cloudConfigurationCases()) {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := newCloudNameContext(testCase)

			if testCase.metadataName != "" {
				require.NoError(
					t,
					cluster.ExportApplyClusterNameOverride(ctx, testCase.metadataName),
				)
			}

			assert.Equal(t, testCase.want, cluster.ExportResolveClusterNameFromContext(ctx))
		})
	}
}

func newCloudNameContext(testCase cloudNameCase) *localregistry.Context {
	provider := v1alpha1.ProviderGCP
	if testCase.distribution == v1alpha1.DistributionAKS {
		provider = v1alpha1.ProviderAzure
	}

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: testCase.distribution,
					Provider:     provider,
					Connection:   v1alpha1.Connection{Context: testCase.context},
				},
			},
		},
	}
	ctx.ClusterCfg.Name = testCase.metadataName

	if testCase.configName == "" {
		return ctx
	}

	if testCase.distribution == v1alpha1.DistributionAKS {
		ctx.AKSConfig = &clusterprovisioner.AKSConfig{Name: testCase.configName}
	} else {
		ctx.GKEConfig = &clusterprovisioner.GKEConfig{Name: testCase.configName}
	}

	return ctx
}

// TestApplyClusterNameOverride_RenamesCloudClusterSpec pins that a name override
// renames the cluster spec too. GKE creates the cluster the spec names, whatever
// name the command passes, so a spec left behind would create one cluster and
// track another.
func TestApplyClusterNameOverride_RenamesCloudClusterSpec(t *testing.T) {
	t.Parallel()

	t.Run("GKE", func(t *testing.T) {
		t.Parallel()

		ctx := &localregistry.Context{GKEConfig: &clusterprovisioner.GKEConfig{
			Name:        "old",
			ClusterSpec: &containerpb.Cluster{Name: "old"},
		}}

		require.NoError(t, cluster.ExportApplyClusterNameOverride(ctx, cloudClusterName))
		assert.Equal(t, cloudClusterName, ctx.GKEConfig.Name)
		assert.Equal(t, cloudClusterName, ctx.GKEConfig.ClusterSpec.GetName())
	})

	t.Run("AKS", func(t *testing.T) {
		t.Parallel()

		oldName := "old"
		ctx := &localregistry.Context{AKSConfig: &clusterprovisioner.AKSConfig{
			Name:        oldName,
			ClusterSpec: &armcontainerservice.ManagedCluster{Name: &oldName},
		}}

		require.NoError(t, cluster.ExportApplyClusterNameOverride(ctx, cloudClusterName))
		assert.Equal(t, cloudClusterName, ctx.AKSConfig.Name)
		require.NotNil(t, ctx.AKSConfig.ClusterSpec.Name)
		assert.Equal(t, cloudClusterName, *ctx.AKSConfig.ClusterSpec.Name)
	})

	t.Run("without a cluster spec", func(t *testing.T) {
		t.Parallel()

		ctx := &localregistry.Context{
			GKEConfig: &clusterprovisioner.GKEConfig{Name: "old"},
			AKSConfig: &clusterprovisioner.AKSConfig{Name: "old"},
		}

		require.NoError(t, cluster.ExportApplyClusterNameOverride(ctx, cloudClusterName))
		assert.Equal(t, cloudClusterName, ctx.GKEConfig.Name)
		assert.Nil(t, ctx.GKEConfig.ClusterSpec)
		assert.Equal(t, cloudClusterName, ctx.AKSConfig.Name)
		assert.Nil(t, ctx.AKSConfig.ClusterSpec)
	})
}

// Names for the distributions the GKE and AKS change leaves alone: each holds
// otherConfigName in its distribution config and is overridden with
// otherOverrideName.
const (
	otherConfigName   = "old"
	otherOverrideName = "renamed"
)

// otherDistributionCase is a distribution other than GKE and AKS, with the
// distribution config that names its cluster.
type otherDistributionCase struct {
	distribution v1alpha1.Distribution
	configure    func(ctx *localregistry.Context)
	// wantRenamed is the name resolved after a name override.
	wantRenamed string
}

func otherDistributionCases() []otherDistributionCase {
	return []otherDistributionCase{
		{
			distribution: v1alpha1.DistributionVanilla,
			configure: func(ctx *localregistry.Context) {
				ctx.KindConfig = &kindv1alpha4.Cluster{Name: otherConfigName}
			},
			wantRenamed: otherOverrideName,
		},
		{
			distribution: v1alpha1.DistributionK3s,
			configure: func(ctx *localregistry.Context) {
				ctx.K3dConfig = &v1alpha5.SimpleConfig{
					ObjectMeta: types.ObjectMeta{Name: otherConfigName},
				}
			},
			wantRenamed: otherOverrideName,
		},
		{
			distribution: v1alpha1.DistributionVCluster,
			configure: func(ctx *localregistry.Context) {
				ctx.VClusterConfig = &clusterprovisioner.VClusterConfig{Name: otherConfigName}
			},
			wantRenamed: otherOverrideName,
		},
		{
			distribution: v1alpha1.DistributionKWOK,
			configure: func(ctx *localregistry.Context) {
				ctx.KWOKConfig = &clusterprovisioner.KWOKConfig{Name: otherConfigName}
			},
			wantRenamed: otherOverrideName,
		},
		{
			distribution: v1alpha1.DistributionEKS,
			configure: func(ctx *localregistry.Context) {
				ctx.EKSConfig = &clusterprovisioner.EKSConfig{Name: otherConfigName}
			},
			wantRenamed: otherConfigName,
		},
	}
}

// TestResolveClusterNameFromContext_OtherDistributionsUnchanged pins the other
// distributions around the GKE and AKS change: each still resolves its
// distribution config name, a name override still renames it, and EKS still
// keeps the name its eksctl source declares.
func TestResolveClusterNameFromContext_OtherDistributionsUnchanged(t *testing.T) {
	t.Parallel()

	for _, testCase := range otherDistributionCases() {
		t.Run(string(testCase.distribution), func(t *testing.T) {
			t.Parallel()

			ctx := &localregistry.Context{
				ClusterCfg: &v1alpha1.Cluster{
					Spec: v1alpha1.Spec{
						Cluster: v1alpha1.ClusterSpec{
							Distribution: testCase.distribution,
							Connection:   v1alpha1.Connection{Context: "custom-context"},
						},
					},
				},
			}
			testCase.configure(ctx)

			assert.Equal(t, otherConfigName, cluster.ExportResolveClusterNameFromContext(ctx))

			require.NoError(t, cluster.ExportApplyClusterNameOverride(ctx, otherOverrideName))
			assert.Equal(t, testCase.wantRenamed, cluster.ExportResolveClusterNameFromContext(ctx))
		})
	}
}
