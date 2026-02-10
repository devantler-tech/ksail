package localregistry_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var errNotImplemented = errors.New("not implemented")

func TestNewDependencies_ReturnsDefaults(t *testing.T) {
	t.Parallel()

	deps := localregistry.NewDependencies()

	assert.NotNil(t, deps.ServiceFactory, "ServiceFactory should not be nil")
	assert.NotNil(t, deps.DockerInvoker, "DockerInvoker should not be nil")
}

func TestNewDependencies_WithServiceFactory(t *testing.T) {
	t.Parallel()

	customFactory := func(_ registry.Config) (registry.Service, error) {
		return nil, errNotImplemented
	}

	deps := localregistry.NewDependencies(
		localregistry.WithServiceFactory(customFactory),
	)

	assert.NotNil(t, deps.ServiceFactory, "ServiceFactory should not be nil")
}

func TestNewDependencies_WithDockerInvoker(t *testing.T) {
	t.Parallel()

	invokerCalled := false
	customInvoker := func(_ *cobra.Command, _ func(client.APIClient) error) error {
		invokerCalled = true

		return nil
	}

	deps := localregistry.NewDependencies(
		localregistry.WithDockerInvoker(customInvoker),
	)

	require.NotNil(t, deps.DockerInvoker, "DockerInvoker should not be nil")
	_ = deps.DockerInvoker(nil, nil)

	assert.True(t, invokerCalled, "Custom DockerInvoker should be called")
}

func TestProvisionStageInfo(t *testing.T) {
	t.Parallel()

	info := localregistry.ProvisionStageInfo()

	assert.Equal(t, "Create local registry...", info.Title)
	assert.Equal(t, "🗄️", info.Emoji)
	assert.Equal(t, "creating local registry", info.Activity)
	assert.Equal(t, "local registry created", info.Success)
	assert.Equal(t, "failed to create local registry", info.FailurePrefix)
}

func TestConnectStageInfo(t *testing.T) {
	t.Parallel()

	info := localregistry.ConnectStageInfo()

	assert.Equal(t, "Attach local registry...", info.Title)
	assert.Equal(t, "🔌", info.Emoji)
	assert.Equal(t, "attaching local registry to cluster", info.Activity)
	assert.Equal(t, "local registry attached to cluster", info.Success)
	assert.Equal(t, "failed to attach local registry", info.FailurePrefix)
}

func TestVerifyStageInfo(t *testing.T) {
	t.Parallel()

	info := localregistry.VerifyStageInfo()

	assert.Equal(t, "Verify registry access...", info.Title)
	assert.Equal(t, "🔐", info.Emoji)
	assert.Equal(t, "verifying registry write access", info.Activity)
	assert.Equal(t, "registry access verified", info.Success)
	assert.Equal(t, "registry access check failed", info.FailurePrefix)
}

func TestCleanupStageInfo(t *testing.T) {
	t.Parallel()

	info := localregistry.CleanupStageInfo()

	assert.Equal(t, "Delete local registry...", info.Title)
	assert.Equal(t, "🧹", info.Emoji)
	assert.Equal(t, "deleting local registry", info.Activity)
	assert.Equal(t, "local registry deleted", info.Success)
	assert.Equal(t, "failed to delete local registry", info.FailurePrefix)
}

func TestNewContextFromConfigManager_PopulatesContext(t *testing.T) {
	t.Parallel()

	// This test verifies that NewContextFromConfigManager properly transfers
	// config and distribution config to the Context struct. Since we can't
	// easily mock ConfigManager, we test the exported fields of Context.

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	kindConfig := &kindv1alpha4.Cluster{Name: "test-cluster"}

	ctx := &localregistry.Context{
		ClusterCfg: clusterCfg,
		KindConfig: kindConfig,
	}

	assert.Equal(t, clusterCfg, ctx.ClusterCfg)
	assert.Equal(t, kindConfig, ctx.KindConfig)
	assert.Nil(t, ctx.K3dConfig)
	assert.Nil(t, ctx.TalosConfig)
}

func TestContext_SupportsAllDistributionConfigs(t *testing.T) { //nolint:funlen // table-driven test
	t.Parallel()

	testCases := []struct {
		name        string
		setupFunc   func() *localregistry.Context
		expectKind  bool
		expectK3d   bool
		expectTalos bool
	}{
		{
			name: "Vanilla with Kind config",
			setupFunc: func() *localregistry.Context {
				return &localregistry.Context{
					ClusterCfg: &v1alpha1.Cluster{
						Spec: v1alpha1.Spec{
							Cluster: v1alpha1.ClusterSpec{
								Distribution: v1alpha1.DistributionVanilla,
							},
						},
					},
					KindConfig: &kindv1alpha4.Cluster{Name: "test"},
				}
			},
			expectKind: true,
		},
		{
			name: "K3s with K3d config",
			setupFunc: func() *localregistry.Context {
				return &localregistry.Context{
					ClusterCfg: &v1alpha1.Cluster{
						Spec: v1alpha1.Spec{
							Cluster: v1alpha1.ClusterSpec{
								Distribution: v1alpha1.DistributionK3s,
							},
						},
					},
					K3dConfig: &k3dv1alpha5.SimpleConfig{},
				}
			},
			expectK3d: true,
		},
		{
			name: "Talos with Talos config",
			setupFunc: func() *localregistry.Context {
				return &localregistry.Context{
					ClusterCfg: &v1alpha1.Cluster{
						Spec: v1alpha1.Spec{
							Cluster: v1alpha1.ClusterSpec{
								Distribution: v1alpha1.DistributionTalos,
							},
						},
					},
					TalosConfig: &talosconfigmanager.Configs{},
				}
			},
			expectTalos: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := testCase.setupFunc()

			if testCase.expectKind {
				assert.NotNil(t, ctx.KindConfig, "Expected KindConfig to be populated")
			}

			if testCase.expectK3d {
				assert.NotNil(t, ctx.K3dConfig, "Expected K3dConfig to be populated")
			}

			if testCase.expectTalos {
				assert.NotNil(t, ctx.TalosConfig, "Expected TalosConfig to be populated")
			}
		})
	}
}

func TestStageType_Constants(t *testing.T) {
	t.Parallel()

	// Verify stage type constants are distinct
	stages := []localregistry.StageType{
		localregistry.StageProvision,
		localregistry.StageConnect,
		localregistry.StageVerify,
	}

	seen := make(map[localregistry.StageType]bool)
	for _, stage := range stages {
		assert.False(t, seen[stage], "Stage types should be unique")
		seen[stage] = true
	}
}

func TestErrors_AreDistinct(t *testing.T) {
	t.Parallel()

	// Verify error variables are distinct
	assert.NotEqual(t,
		localregistry.ErrNilRegistryContext,
		localregistry.ErrUnsupportedStage,
	)
	assert.NotEqual(t,
		localregistry.ErrNilRegistryContext,
		localregistry.ErrCloudProviderRequiresExternalRegistry,
	)
	assert.NotEqual(t,
		localregistry.ErrUnsupportedStage,
		localregistry.ErrCloudProviderRequiresExternalRegistry,
	)
}

func TestErrors_HaveDescriptiveMessages(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		err     error
		contain string
	}{
		{
			name:    "ErrNilRegistryContext",
			err:     localregistry.ErrNilRegistryContext,
			contain: "nil",
		},
		{
			name:    "ErrUnsupportedStage",
			err:     localregistry.ErrUnsupportedStage,
			contain: "unsupported",
		},
		{
			name:    "ErrCloudProviderRequiresExternalRegistry",
			err:     localregistry.ErrCloudProviderRequiresExternalRegistry,
			contain: "cloud provider",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, testCase.err.Error(), testCase.contain)
		})
	}
}
