package cluster_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestResolvePostCreateContext_NonEKSUsesDistributionConvention(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
					Provider:     v1alpha1.ProviderDocker,
				},
			},
		},
		KindConfig: &kindv1alpha4.Cluster{Name: "test"},
	}

	require.NoError(t, cluster.ExportResolvePostCreateContext(ctx))
	assert.Equal(t, "kind-test", ctx.ClusterCfg.Spec.Cluster.Connection.Context)
}

func TestResolvePostCreateContext_EKSMissingConfigFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionEKS},
			},
		},
	}

	err := cluster.ExportResolvePostCreateContext(ctx)
	require.ErrorContains(t, err, "EKS configuration is unavailable")
	assert.ErrorContains(t, err, "set spec.cluster.connection.context explicitly")
}

func TestResolvePostCreateContext_EKSRegionDelegatedToProfile(t *testing.T) {
	t.Parallel()

	const (
		eksctlContext      = "ci@st-eks.eu-west-1.eksctl.io"
		otherRegionContext = "ci@st-eks.us-east-1.eksctl.io"
	)

	kubeconfigPath := writeEKSContextKubeconfig(
		t,
		eksctlContext,
		eksContextFixture{name: eksctlContext, server: eksContextTestEndpoint},
		eksContextFixture{name: otherRegionContext, server: eksContextTestOtherEndpoint},
	)

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionEKS,
					Connection: v1alpha1.Connection{
						Kubeconfig: kubeconfigPath,
					},
				},
			},
		},
		EKSConfig:   &clusterprovisioner.EKSConfig{Name: "st-eks"},
		EKSEndpoint: eksContextTestEndpoint,
	}

	require.NoError(t, cluster.ExportResolvePostCreateContext(ctx))
	assert.Equal(t, eksctlContext, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
	assert.Equal(t, "eu-west-1", ctx.EKSConfig.Region)
}

func TestResolvePostCreateContext_EKSExplicitContextPinsObservedRegion(t *testing.T) {
	t.Parallel()

	const explicitContext = "ci@st-eks.eu-west-1.eksctl.io"

	ctx := newExplicitEKSPostCreateContext(
		t,
		"st-eks",
		explicitContext,
		explicitContext,
		explicitContext,
	)

	require.NoError(t, cluster.ExportResolvePostCreateContext(ctx))
	assert.Equal(t, explicitContext, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
	assert.Equal(t, "eu-west-1", ctx.EKSConfig.Region)
}

func TestResolvePostCreateContext_EKSRejectsUnobservedExplicitContext(t *testing.T) {
	t.Parallel()

	const (
		targetContext = "ci@st-eks.eu-west-1.eksctl.io"
		otherContext  = "ci@st-eks.us-east-1.eksctl.io"
		wrongTarget   = "ci@other.eu-west-1.eksctl.io"
	)

	testCases := []struct {
		name     string
		explicit string
		current  string
		contexts []string
	}{
		{
			name:     "missing from output",
			explicit: targetContext,
			current:  otherContext,
			contexts: []string{otherContext},
		},
		{
			name:     "stale non-current context",
			explicit: targetContext,
			current:  otherContext,
			contexts: []string{targetContext, otherContext},
		},
		{
			name:     "wrong cluster",
			explicit: wrongTarget,
			current:  wrongTarget,
			contexts: []string{wrongTarget},
		},
		{
			name:     "malformed context",
			explicit: "st-eks.eu.west.eksctl.io",
			current:  "st-eks.eu.west.eksctl.io",
			contexts: []string{"st-eks.eu.west.eksctl.io"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := newExplicitEKSPostCreateContext(
				t,
				"st-eks",
				testCase.explicit,
				testCase.current,
				testCase.contexts...,
			)

			err := cluster.ExportResolvePostCreateContext(ctx)
			require.ErrorContains(t, err, "explicit EKS context was not observed after creation")
			assert.Empty(t, ctx.EKSConfig.Region)
		})
	}
}

func TestResolvePostCreateContext_EKSExplicitContextPreservesConfiguredRegion(t *testing.T) {
	t.Parallel()

	const explicitContext = "ci@st-eks.eu-west-1.eksctl.io"

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
			Distribution: v1alpha1.DistributionEKS,
			Connection:   v1alpha1.Connection{Context: explicitContext},
		}}},
		EKSConfig: &clusterprovisioner.EKSConfig{Name: "st-eks", Region: "us-east-1"},
	}

	require.NoError(t, cluster.ExportResolvePostCreateContext(ctx))
	assert.Equal(t, "us-east-1", ctx.EKSConfig.Region)
}

func newExplicitEKSPostCreateContext(
	t *testing.T,
	clusterName, explicitContext, currentContext string,
	contexts ...string,
) *localregistry.Context {
	t.Helper()

	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	config := clientcmdapi.NewConfig()
	config.CurrentContext = currentContext

	for _, contextName := range contexts {
		config.Contexts[contextName] = &clientcmdapi.Context{}
	}

	require.NoError(t, clientcmd.WriteToFile(*config, kubeconfigPath))

	return &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
			Distribution: v1alpha1.DistributionEKS,
			Connection: v1alpha1.Connection{
				Context:    explicitContext,
				Kubeconfig: kubeconfigPath,
			},
		}}},
		EKSConfig: &clusterprovisioner.EKSConfig{Name: clusterName},
	}
}

func TestApplyClusterNameOverride_EKSPreservesSourceConfigAndDefersContextResolution(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		originalContext string
	}{
		{name: "empty context remains unresolved"},
		{name: "explicit context is preserved", originalContext: "operator-selected-context"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := &localregistry.Context{
				ClusterCfg: &v1alpha1.Cluster{
					Spec: v1alpha1.Spec{
						Cluster: v1alpha1.ClusterSpec{
							Distribution: v1alpha1.DistributionEKS,
							Connection: v1alpha1.Connection{
								Context: testCase.originalContext,
							},
						},
					},
				},
				EKSConfig: &clusterprovisioner.EKSConfig{Name: "old-eks-name"},
			}

			require.NoError(t, cluster.ExportApplyClusterNameOverride(ctx, "st-eks"))
			assert.Equal(t, "old-eks-name", ctx.EKSConfig.Name)
			assert.Equal(
				t,
				testCase.originalContext,
				ctx.ClusterCfg.Spec.Cluster.Connection.Context,
			)
		})
	}
}

func TestApplyClusterNameOverride_NonEKSReplacesStaleContext(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
					Provider:     v1alpha1.ProviderDocker,
					Connection: v1alpha1.Connection{
						Context: "kind-old",
					},
				},
			},
		},
		KindConfig: &kindv1alpha4.Cluster{Name: "old"},
	}

	require.NoError(t, cluster.ExportApplyClusterNameOverride(ctx, "new"))
	assert.Equal(t, "new", ctx.KindConfig.Name)
	assert.Equal(t, "kind-new", ctx.ClusterCfg.Spec.Cluster.Connection.Context)
}

func TestPrepareEKSCreateConfig_PinsKubeconfigAndEffectiveRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-2")

	kubeconfigPath := filepath.Join(t.TempDir(), "custom-kubeconfig")
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionEKS,
					Connection: v1alpha1.Connection{
						Kubeconfig: kubeconfigPath,
					},
				},
			},
		},
		EKSConfig: &clusterprovisioner.EKSConfig{
			Name:   "st-eks",
			Region: "eu-west-1",
		},
	}

	require.NoError(t, cluster.ExportPrepareEKSCreateConfig(ctx))

	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	require.NoError(t, err)
	assert.Equal(t, canonicalPath, ctx.EKSConfig.KubeconfigPath)
	assert.Equal(t, "us-east-2", ctx.EKSConfig.Region)
}

func TestPrepareEKSCreateConfig_CanonicalizesOutputPath(t *testing.T) {
	t.Parallel()

	realParent := filepath.Join(t.TempDir(), "nested", "kube")
	kubeconfigPath := filepath.Join(realParent, "config")
	ctx := newEKSPrepareContext(kubeconfigPath)

	require.NoError(t, cluster.ExportPrepareEKSCreateConfig(ctx))

	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	require.NoError(t, err)
	assert.Equal(t, canonicalPath, ctx.EKSConfig.KubeconfigPath)

	info, err := os.Stat(realParent)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestPrepareEKSCreateConfig_ResolvesSymlinkedOutputParent(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows")
	}

	realParent := t.TempDir()
	linkParent := filepath.Join(t.TempDir(), "kube")
	require.NoError(t, os.Symlink(realParent, linkParent))

	ctx := newEKSPrepareContext(filepath.Join(linkParent, "config"))
	require.NoError(t, cluster.ExportPrepareEKSCreateConfig(ctx))

	canonicalPath, err := fsutil.EvalCanonicalPath(filepath.Join(realParent, "config"))
	require.NoError(t, err)
	assert.Equal(t, canonicalPath, ctx.EKSConfig.KubeconfigPath)
}

func newEKSPrepareContext(kubeconfigPath string) *localregistry.Context {
	return &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionEKS,
					Connection: v1alpha1.Connection{
						Kubeconfig: kubeconfigPath,
					},
				},
			},
		},
		EKSConfig: &clusterprovisioner.EKSConfig{Name: "st-eks", Region: "eu-west-1"},
	}
}
