package lifecycle_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTestError = errors.New("test error")

// TestResolveClusterInfo tests the ResolveClusterInfo function.
func TestResolveClusterInfo(t *testing.T) {
	t.Parallel()

	t.Run("name_from_flag", func(t *testing.T) {
		t.Parallel()

		resolved, err := lifecycle.ResolveClusterInfo(
			"my-cluster",
			v1alpha1.ProviderDocker,
			"",
		)

		require.NoError(t, err)
		require.NotNil(t, resolved)
		assert.Equal(t, "my-cluster", resolved.ClusterName)
		assert.Equal(t, v1alpha1.ProviderDocker, resolved.Provider)
	})

	t.Run("provider_defaults_to_docker", func(t *testing.T) {
		t.Parallel()

		resolved, err := lifecycle.ResolveClusterInfo(
			"my-cluster",
			"", // Empty provider
			"",
		)

		require.NoError(t, err)
		require.NotNil(t, resolved)
		assert.Equal(t, "my-cluster", resolved.ClusterName)
		assert.Equal(t, v1alpha1.ProviderDocker, resolved.Provider)
	})

	t.Run("error_when_no_name_available", func(t *testing.T) {
		t.Parallel()

		// No flags, no config file, and an explicit invalid kubeconfig path
		resolved, err := lifecycle.ResolveClusterInfo(
			"",
			"",
			"/non-existent-kubeconfig",
		)

		require.ErrorIs(t, err, lifecycle.ErrClusterNameRequired)
		assert.Nil(t, resolved)
	})
}

// TestNewSimpleLifecycleCmd tests the NewSimpleLifecycleCmd function.
func TestNewSimpleLifecycleCmd(t *testing.T) {
	t.Parallel()

	t.Run("creates_command_with_flags", testNewSimpleLifecycleCmdWithFlags)
	t.Run("command_requires_cluster_name", testNewSimpleLifecycleCmdRequiresName)
}

func testNewSimpleLifecycleCmdWithFlags(t *testing.T) {
	t.Parallel()

	actionCalled := false
	config := lifecycle.SimpleLifecycleConfig{
		Use:          "start",
		Short:        "Start a cluster",
		Long:         "Start a Kubernetes cluster",
		TitleEmoji:   "🚀",
		TitleContent: "Starting Cluster",
		Activity:     "Cluster is starting...",
		Success:      "Cluster started",
		Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
			actionCalled = true

			return nil
		},
	}

	cmd := lifecycle.NewSimpleLifecycleCmd(config)

	require.NotNil(t, cmd)
	assert.Equal(t, "start", cmd.Use)
	assert.Equal(t, "Start a cluster", cmd.Short)
	assert.Equal(t, "Start a Kubernetes cluster", cmd.Long)

	nameFlag := cmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)
	assert.Equal(t, "n", nameFlag.Shorthand)

	providerFlag := cmd.Flags().Lookup("provider")
	require.NotNil(t, providerFlag)
	assert.Equal(t, "p", providerFlag.Shorthand)

	assert.False(t, actionCalled)
}

func testNewSimpleLifecycleCmdRequiresName(t *testing.T) {
	t.Parallel()

	config := lifecycle.SimpleLifecycleConfig{
		Use:          "start",
		Short:        "Start a cluster",
		TitleEmoji:   "🚀",
		TitleContent: "Starting Cluster",
		Activity:     "Cluster is starting...",
		Success:      "Cluster started",
		Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
			return nil
		},
	}

	cmd := lifecycle.NewSimpleLifecycleCmd(config)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{})

	err := cmd.Execute()

	require.Error(t, err)
	assert.ErrorIs(t, err, lifecycle.ErrClusterNameRequired)
}

// TestResolvedClusterInfo_Struct tests the ResolvedClusterInfo structure.
func TestResolvedClusterInfo_Struct(t *testing.T) {
	t.Parallel()

	t.Run("creates_valid_struct", func(t *testing.T) {
		t.Parallel()

		info := &lifecycle.ResolvedClusterInfo{
			ClusterName:    "test-cluster",
			Provider:       v1alpha1.ProviderDocker,
			KubeconfigPath: "/path/to/kubeconfig",
		}

		assert.Equal(t, "test-cluster", info.ClusterName)
		assert.Equal(t, v1alpha1.ProviderDocker, info.Provider)
		assert.Equal(t, "/path/to/kubeconfig", info.KubeconfigPath)
	})
}

// TestSimpleLifecycleConfig_Struct tests the SimpleLifecycleConfig structure.
func TestSimpleLifecycleConfig_Struct(t *testing.T) {
	t.Parallel()

	t.Run("creates_valid_config", func(t *testing.T) {
		t.Parallel()

		actionCalled := false

		config := lifecycle.SimpleLifecycleConfig{
			Use:          "start",
			Short:        "Start cluster",
			Long:         "Start a Kubernetes cluster",
			TitleEmoji:   "🚀",
			TitleContent: "Starting",
			Activity:     "Starting...",
			Success:      "Started",
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
				actionCalled = true

				return nil
			},
		}

		assert.Equal(t, "start", config.Use)
		assert.Equal(t, "Start cluster", config.Short)
		assert.Equal(t, "Start a Kubernetes cluster", config.Long)
		assert.Equal(t, "🚀", config.TitleEmoji)
		assert.Equal(t, "Starting", config.TitleContent)
		assert.Equal(t, "Starting...", config.Activity)
		assert.Equal(t, "Started", config.Success)
		assert.NotNil(t, config.Action)

		// Execute the action to verify it works
		err := config.Action(context.Background(), nil, "test")
		require.NoError(t, err)
		assert.True(t, actionCalled)
	})
}

// TestLifecycleConfig_Struct tests the Config structure.
func TestLifecycleConfig_Struct(t *testing.T) {
	t.Parallel()

	t.Run("creates_valid_config", func(t *testing.T) {
		t.Parallel()

		actionCalled := false

		config := lifecycle.Config{
			TitleEmoji:         "🚀",
			TitleContent:       "Starting Cluster",
			ActivityContent:    "Cluster is starting...",
			SuccessContent:     "Cluster started",
			ErrorMessagePrefix: "Failed to start",
			Action: func(_ context.Context, _ clusterprovisioner.Provisioner, _ string) error {
				actionCalled = true

				return nil
			},
		}

		assert.Equal(t, "🚀", config.TitleEmoji)
		assert.Equal(t, "Starting Cluster", config.TitleContent)
		assert.Equal(t, "Cluster is starting...", config.ActivityContent)
		assert.Equal(t, "Cluster started", config.SuccessContent)
		assert.Equal(t, "Failed to start", config.ErrorMessagePrefix)
		assert.NotNil(t, config.Action)

		// Execute the action to verify it works
		err := config.Action(context.Background(), nil, "test")
		require.NoError(t, err)
		assert.True(t, actionCalled)
	})
}

// TestLifecycleDeps_Struct tests the Deps structure.
func TestLifecycleDeps_Struct(t *testing.T) {
	t.Parallel()

	t.Run("creates_valid_deps", func(t *testing.T) {
		t.Parallel()

		tmr := &mockTimer{}
		factory := &mockFactory{}

		deps := lifecycle.Deps{
			Timer:   tmr,
			Factory: factory,
		}

		assert.NotNil(t, deps.Timer)
		assert.NotNil(t, deps.Factory)
		assert.Same(t, tmr, deps.Timer)
		assert.Same(t, factory, deps.Factory)
	})

	t.Run("nil_timer_is_valid", func(t *testing.T) {
		t.Parallel()

		factory := &mockFactory{}

		deps := lifecycle.Deps{
			Timer:   nil,
			Factory: factory,
		}

		assert.Nil(t, deps.Timer)
		assert.NotNil(t, deps.Factory)
	})
}

// TestSimpleLifecycleErrors tests error handling in simple lifecycle commands.
func TestSimpleLifecycleErrors(t *testing.T) {
	t.Parallel()

	t.Run("error_missing_cluster_name", func(t *testing.T) {
		t.Parallel()

		require.Error(t, lifecycle.ErrClusterNameRequired)
		assert.Contains(t, lifecycle.ErrClusterNameRequired.Error(), "cluster name is required")
	})
}

// TestActionSignature tests the Action type signature.
func TestActionSignature(t *testing.T) {
	t.Parallel()

	t.Run("action_receives_correct_parameters", func(t *testing.T) {
		t.Parallel()

		var (
			receivedProvisioner clusterprovisioner.Provisioner
			receivedClusterName string
			ctxReceived         bool
		)

		action := lifecycle.Action(func(
			ctx context.Context,
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			ctxReceived = ctx != nil
			receivedProvisioner = provisioner
			receivedClusterName = clusterName

			return nil
		})

		testProvisioner := &mockProvisioner{}
		testClusterName := "test-cluster"

		err := action(context.Background(), testProvisioner, testClusterName)

		require.NoError(t, err)
		assert.True(t, ctxReceived)
		assert.Equal(t, testProvisioner, receivedProvisioner)
		assert.Equal(t, testClusterName, receivedClusterName)
	})

	t.Run("action_can_return_error", func(t *testing.T) {
		t.Parallel()

		action := lifecycle.Action(func(
			_ context.Context,
			_ clusterprovisioner.Provisioner,
			_ string,
		) error {
			return errTestError
		})

		err := action(context.Background(), nil, "test")

		assert.ErrorIs(t, err, errTestError)
	})
}
