package lifecycle_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// TestRunWithConfig_SuccessWithTimer verifies the full success path
// including timer and success message output.
func TestRunWithConfig_SuccessWithTimer(t *testing.T) {
	t.Parallel()

	provisioner := &mockProvisioner{}
	kindConfig := &v1alpha4.Cluster{Name: "timer-test"}
	factory := &mockFactory{provisioner: provisioner, distributionConfig: kindConfig}

	deps := lifecycle.Deps{Timer: &mockTimer{}, Factory: factory}

	var receivedName string

	config := lifecycle.Config{
		TitleEmoji:         "⏱️",
		TitleContent:       "Timer Test",
		ActivityContent:    "testing timer",
		SuccessContent:     "done",
		ErrorMessagePrefix: "failed",
		Action: func(_ context.Context, _ clusterprovisioner.Provisioner, name string) error {
			receivedName = name

			return nil
		},
	}

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&buf)

	err := lifecycle.RunWithConfig(cmd, deps, config, cfg)

	require.NoError(t, err)
	assert.Equal(t, "timer-test", receivedName)
}

// TestRunWithConfig_NilTimer verifies RunWithConfig works without a timer.
func TestRunWithConfig_NilTimer(t *testing.T) {
	t.Parallel()

	provisioner := &mockProvisioner{}
	kindConfig := &v1alpha4.Cluster{Name: "no-timer"}
	factory := &mockFactory{provisioner: provisioner, distributionConfig: kindConfig}

	deps := lifecycle.Deps{Timer: nil, Factory: factory}

	config := lifecycle.Config{
		TitleEmoji:      "🔇",
		TitleContent:    "No Timer",
		ActivityContent: "no timer test",
		SuccessContent:  "passed",
		Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
			return nil
		},
	}

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))

	err := lifecycle.RunWithConfig(cmd, deps, config, cfg)
	require.NoError(t, err)
}

// TestRunWithConfig_ErrorWrapping verifies that action errors are wrapped with prefix.
func TestRunWithConfig_ErrorWrapping(t *testing.T) {
	t.Parallel()

	provisioner := &mockProvisioner{}
	kindConfig := &v1alpha4.Cluster{Name: "error-test"}
	factory := &mockFactory{provisioner: provisioner, distributionConfig: kindConfig}

	deps := lifecycle.Deps{Timer: &mockTimer{}, Factory: factory}

	config := lifecycle.Config{
		TitleEmoji:         "❌",
		TitleContent:       "Error Test",
		ActivityContent:    "testing error",
		ErrorMessagePrefix: "deletion failed",
		Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
			return errActionFailed
		},
	}

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(new(bytes.Buffer))

	err := lifecycle.RunWithConfig(cmd, deps, config, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deletion failed")
	assert.Contains(t, err.Error(), "action failed")
}

// TestExtractClusterNameFromContext_VCluster verifies vcluster context extraction.
func TestExtractClusterNameFromContext_VCluster(t *testing.T) {
	t.Parallel()

	name := lifecycle.ExtractClusterNameFromContext(
		"vcluster-docker_my-vcluster",
		v1alpha1.DistributionVCluster,
	)
	assert.Equal(t, "my-vcluster", name)
}

// TestExtractClusterNameFromContext_VClusterWrongPrefix verifies wrong prefix returns empty.
func TestExtractClusterNameFromContext_VClusterWrongPrefix(t *testing.T) {
	t.Parallel()

	name := lifecycle.ExtractClusterNameFromContext(
		"kind-test",
		v1alpha1.DistributionVCluster,
	)
	assert.Empty(t, name)
}

// TestGetClusterNameFromConfig_ContextPriority verifies context takes priority
// over distribution config.
func TestGetClusterNameFromConfig_ContextPriority(t *testing.T) {
	t.Parallel()

	kindConfig := &v1alpha4.Cluster{Name: "from-config"}
	factory := &mockFactory{
		provisioner:        &mockProvisioner{},
		distributionConfig: kindConfig,
	}

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				Connection:   v1alpha1.Connection{Context: "kind-from-context"},
			},
		},
	}

	name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)
	require.NoError(t, err)
	// Context should take priority
	assert.Equal(t, "from-context", name)
}

// TestGetClusterNameFromConfig_FallsBackToDistConfig verifies fallback to
// distribution config when no context is set.
func TestGetClusterNameFromConfig_FallsBackToDistConfig(t *testing.T) {
	t.Parallel()

	kindConfig := &v1alpha4.Cluster{Name: "dist-name"}
	factory := &mockFactory{
		provisioner:        &mockProvisioner{},
		distributionConfig: kindConfig,
	}

	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
			},
		},
	}

	name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)
	require.NoError(t, err)
	assert.Equal(t, "dist-name", name)
}
