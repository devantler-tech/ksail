package lifecycle_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// mockTimer implements the timer.Timer interface for testing.
type mockTimer struct {
	started   bool
	stages    int
	completed bool
}

func (m *mockTimer) Start()                              { m.started = true }
func (m *mockTimer) NewStage()                           { m.stages++ }
func (m *mockTimer) Stop()                               { m.completed = true }
func (m *mockTimer) GetTiming() (time.Duration, time.Duration) { return 0, 0 }

// mockFactory implements clusterprovisioner.Factory for testing.
type mockFactory struct {
	provisioner        clusterprovisioner.Provisioner
	distributionConfig any
	createErr          error
}

func (m *mockFactory) Create(_ context.Context, _ *v1alpha1.Cluster) (
	clusterprovisioner.Provisioner,
	any,
	error,
) {
	return m.provisioner, m.distributionConfig, m.createErr
}

// mockProvisioner implements clusterprovisioner.Provisioner for testing.
type mockProvisioner struct {
	actionErr error
	called    bool
}

func (m *mockProvisioner) Create(_ context.Context, _ string) error {
	m.called = true
	return m.actionErr
}

func (m *mockProvisioner) Start(_ context.Context, _ string) error {
	m.called = true
	return m.actionErr
}

func (m *mockProvisioner) Stop(_ context.Context, _ string) error {
	m.called = true
	return m.actionErr
}

func (m *mockProvisioner) Delete(_ context.Context, _ string) error {
	m.called = true
	return m.actionErr
}

func (m *mockProvisioner) List(_ context.Context) ([]string, error) {
	m.called = true
	return nil, m.actionErr
}

func (m *mockProvisioner) Exists(_ context.Context, _ string) (bool, error) {
	m.called = true
	return false, m.actionErr
}

// TestGetClusterNameFromConfig tests the GetClusterNameFromConfig function.
func TestGetClusterNameFromConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil_config_returns_error", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{}
		name, err := lifecycle.GetClusterNameFromConfig(nil, factory)

		assert.ErrorIs(t, err, lifecycle.ErrClusterConfigRequired)
		assert.Empty(t, name)
	})

	t.Run("factory_create_error", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{
			createErr: errors.New("factory error"),
		}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		}

		name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load distribution config")
		assert.Empty(t, name)
	})

	t.Run("extract_from_context_kind", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
					Connection: v1alpha1.Connection{
						Context: "kind-test-cluster",
					},
				},
			},
		}

		name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)

		require.NoError(t, err)
		assert.Equal(t, "test-cluster", name)
	})

	t.Run("extract_from_context_k3d", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionK3s,
					Connection: v1alpha1.Connection{
						Context: "k3d-my-cluster",
					},
				},
			},
		}

		name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)

		require.NoError(t, err)
		assert.Equal(t, "my-cluster", name)
	})

	t.Run("extract_from_context_talos", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionTalos,
					Connection: v1alpha1.Connection{
						Context: "admin@homelab",
					},
				},
			},
		}

		name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)

		require.NoError(t, err)
		assert.Equal(t, "homelab", name)
	})

	t.Run("extract_from_context_vcluster", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVCluster,
					Connection: v1alpha1.Connection{
						Context: "vcluster-docker_vcluster",
					},
				},
			},
		}

		name, err := lifecycle.GetClusterNameFromConfig(cfg, factory)

		require.NoError(t, err)
		assert.Equal(t, "vcluster", name)
	})
}

// TestRunWithConfig tests the RunWithConfig function.
func TestRunWithConfig(t *testing.T) {
	t.Parallel()

	t.Run("factory_create_error", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{
			createErr: errors.New("factory error"),
		}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		}

		deps := lifecycle.Deps{
			Timer:   &mockTimer{},
			Factory: factory,
		}

		config := lifecycle.Config{
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
				return nil
			},
		}

		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(new(bytes.Buffer))

		err := lifecycle.RunWithConfig(cmd, deps, config, cfg)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve cluster provisioner")
	})

	t.Run("nil_provisioner_error", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{
			provisioner: nil,
		}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		}

		deps := lifecycle.Deps{
			Timer:   &mockTimer{},
			Factory: factory,
		}

		config := lifecycle.Config{
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
				return nil
			},
		}

		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(new(bytes.Buffer))

		err := lifecycle.RunWithConfig(cmd, deps, config, cfg)

		assert.ErrorIs(t, err, lifecycle.ErrMissingProvisionerDependency)
	})

	t.Run("action_error", func(t *testing.T) {
		t.Parallel()

		provisioner := &mockProvisioner{
			actionErr: errors.New("action failed"),
		}

		kindConfig := &v1alpha4.Cluster{
			Name: "test-cluster",
		}

		factory := &mockFactory{
			provisioner:        provisioner,
			distributionConfig: kindConfig,
		}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		}

		deps := lifecycle.Deps{
			Timer:   &mockTimer{},
			Factory: factory,
		}

		actionCalled := false
		config := lifecycle.Config{
			TitleEmoji:      "🚀",
			TitleContent:    "Starting Cluster",
			ActivityContent: "Cluster is starting...",
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
				actionCalled = true
				return errors.New("action failed")
			},
		}

		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(new(bytes.Buffer))

		err := lifecycle.RunWithConfig(cmd, deps, config, cfg)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "action failed")
		assert.True(t, actionCalled)
	})

	t.Run("success_with_context_extraction", func(t *testing.T) {
		t.Parallel()

		provisioner := &mockProvisioner{}

		kindConfig := &v1alpha4.Cluster{
			Name: "should-not-use-this",
		}

		factory := &mockFactory{
			provisioner:        provisioner,
			distributionConfig: kindConfig,
		}

		cfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
					Connection: v1alpha1.Connection{
						Context: "kind-from-context",
					},
				},
			},
		}

		deps := lifecycle.Deps{
			Timer:   &mockTimer{},
			Factory: factory,
		}

		var receivedClusterName string
		config := lifecycle.Config{
			TitleEmoji:   "🚀",
			TitleContent: "Starting Cluster",
			ActivityContent:     "Cluster is starting...",
			SuccessContent:      "Cluster started",
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, name string) error {
				receivedClusterName = name
				return nil
			},
		}

		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(new(bytes.Buffer))

		err := lifecycle.RunWithConfig(cmd, deps, config, cfg)

		require.NoError(t, err)
		assert.Equal(t, "from-context", receivedClusterName)
	})
}

// TestNewStandardRunE tests the NewStandardRunE function.
func TestNewStandardRunE(t *testing.T) {
	t.Parallel()

	t.Run("wraps_handler_correctly", func(t *testing.T) {
		t.Parallel()

		runtimeContainer := di.NewRuntime()
		cfgManager := ksailconfigmanager.NewConfigManager(nil)

		config := lifecycle.Config{
			TitleEmoji:   "🚀",
			TitleContent: "Testing",
			ActivityContent:     "Running test",
			SuccessContent:      "Test completed",
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
				return nil
			},
		}

		runE := lifecycle.NewStandardRunE(runtimeContainer, cfgManager, config)

		assert.NotNil(t, runE)
	})
}

// TestWrapHandler tests the WrapHandler function.
func TestWrapHandler(t *testing.T) {
	t.Parallel()

	t.Run("wraps_handler_and_returns_function", func(t *testing.T) {
		t.Parallel()

		runtimeContainer := di.NewRuntime()
		cfgManager := ksailconfigmanager.NewConfigManager(nil)

		handlerCalled := false
		handler := func(_ *cobra.Command, _ *ksailconfigmanager.ConfigManager, _ lifecycle.Deps) error {
			handlerCalled = true
			return nil
		}

		wrapped := lifecycle.WrapHandler(runtimeContainer, cfgManager, handler)

		assert.NotNil(t, wrapped)
		assert.False(t, handlerCalled) // Should not call until executed
	})
}
