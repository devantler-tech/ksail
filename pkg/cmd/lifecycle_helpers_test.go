package cmd_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	pkgcmd "github.com/devantler-tech/ksail/pkg/cmd"
	ksailconfigmanager "github.com/devantler-tech/ksail/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

var errFactoryError = errors.New("factory error")

// lifecycleTimer extends recordingTimer to track NewStage calls.
type lifecycleTimer struct {
	started       bool
	newStageCalls int
}

func (r *lifecycleTimer) Start()                                    { r.started = true }
func (r *lifecycleTimer) NewStage()                                 { r.newStageCalls++ }
func (r *lifecycleTimer) GetTiming() (time.Duration, time.Duration) { return 0, 0 }
func (r *lifecycleTimer) Stop()                                     {}

func assertTimerState(t *testing.T, timer *lifecycleTimer, expectedStages int) {
	t.Helper()

	if !timer.started {
		t.Error("expected timer to be started")
	}

	if timer.newStageCalls != expectedStages {
		t.Fatalf("expected newStageCalls=%d, got %d", expectedStages, timer.newStageCalls)
	}
}

func assertLifecycleErrorContains(t *testing.T, err error, substring string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error containing %q", substring)
	}

	if !strings.Contains(err.Error(), substring) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertLifecycleFailure(
	t *testing.T,
	timer *lifecycleTimer,
	err error,
	substring string,
	expectedStages int,
) {
	t.Helper()

	assertLifecycleErrorContains(t, err, substring)
	assertTimerState(t, timer, expectedStages)
}

func TestHandleLifecycleRunE_ErrorPaths(t *testing.T) {
	t.Parallel()

	type lifecycleSetup func(*testing.T) (
		*ksailconfigmanager.ConfigManager,
		pkgcmd.LifecycleDeps,
		pkgcmd.LifecycleConfig,
		*lifecycleTimer,
		*cobra.Command,
	)

	cases := []struct {
		name           string
		setup          lifecycleSetup
		expectedErr    string
		expectedStages int
	}{
		{
			name:           "config load error",
			setup:          configLoadErrorSetup,
			expectedErr:    "failed to load cluster configuration",
			expectedStages: 0,
		},
		{
			name:           "factory create error",
			setup:          factoryErrorSetup,
			expectedErr:    "failed to resolve cluster provisioner",
			expectedStages: 1,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfgManager, deps, config, timer, cmd := testCase.setup(t)

			err := pkgcmd.HandleLifecycleRunE(cmd, cfgManager, deps, config)

			assertLifecycleFailure(t, timer, err, testCase.expectedErr, testCase.expectedStages)
		})
	}
}

func configLoadErrorSetup(t *testing.T) (
	*ksailconfigmanager.ConfigManager,
	pkgcmd.LifecycleDeps,
	pkgcmd.LifecycleConfig,
	*lifecycleTimer,
	*cobra.Command,
) {
	t.Helper()

	tempDir := t.TempDir()
	badPath := filepath.Join(tempDir, "ksail.yaml")

	err := os.WriteFile(badPath, []byte(": invalid yaml"), 0o600)
	if err != nil {
		t.Fatalf("failed to write bad config: %v", err)
	}

	cfgManager := ksailconfigmanager.NewConfigManager(io.Discard)
	cfgManager.Viper.SetConfigFile(badPath)

	timer := &lifecycleTimer{}
	factory := clusterprovisioner.NewMockFactory(t)
	deps := pkgcmd.LifecycleDeps{Timer: timer, Factory: factory}
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)

	return cfgManager, deps, pkgcmd.LifecycleConfig{}, timer, cmd
}

func factoryErrorSetup(t *testing.T) (
	*ksailconfigmanager.ConfigManager,
	pkgcmd.LifecycleDeps,
	pkgcmd.LifecycleConfig,
	*lifecycleTimer,
	*cobra.Command,
) {
	t.Helper()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "ksail.yaml")

	err := os.WriteFile(path, []byte(validClusterConfigYAML), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgManager := ksailconfigmanager.NewConfigManager(io.Discard)
	cfgManager.Viper.SetConfigFile(path)

	timer := &lifecycleTimer{}
	deps := pkgcmd.LifecycleDeps{Timer: timer, Factory: &errorFactory{err: errFactoryError}}
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)

	return cfgManager, deps, pkgcmd.LifecycleConfig{}, timer, cmd
}

// errorFactory satisfies clusterprovisioner.Factory for testing.
type errorFactory struct{ err error }

func (e *errorFactory) Create(
	context.Context,
	*v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	return nil, nil, e.err
}

// The following tests referencing removed helpers/new opts have been omitted during migration.

const validClusterConfigYAML = `apiVersion: ksail.dev/v1alpha1
kind: Cluster
metadata:
  name: sample
spec:
  distribution: Kind
  distributionConfig: kind.yaml
  sourceDirectory: k8s
`

func TestExtractClusterNameFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		context      string
		distribution v1alpha1.Distribution
		expected     string
	}{
		{
			name:         "kind cluster with valid context",
			context:      "kind-my-cluster",
			distribution: v1alpha1.DistributionKind,
			expected:     "my-cluster",
		},
		{
			name:         "k3d cluster with valid context",
			context:      "k3d-my-cluster",
			distribution: v1alpha1.DistributionK3d,
			expected:     "my-cluster",
		},
		{
			name:         "kind cluster with hyphenated name",
			context:      "kind-my-test-cluster",
			distribution: v1alpha1.DistributionKind,
			expected:     "my-test-cluster",
		},
		{
			name:         "k3d cluster with hyphenated name",
			context:      "k3d-my-test-cluster",
			distribution: v1alpha1.DistributionK3d,
			expected:     "my-test-cluster",
		},
		{
			name:         "empty context",
			context:      "",
			distribution: v1alpha1.DistributionKind,
			expected:     "",
		},
		{
			name:         "context with only prefix - kind",
			context:      "kind-",
			distribution: v1alpha1.DistributionKind,
			expected:     "",
		},
		{
			name:         "context with only prefix - k3d",
			context:      "k3d-",
			distribution: v1alpha1.DistributionK3d,
			expected:     "",
		},
		{
			name:         "context without expected prefix for kind",
			context:      "my-cluster",
			distribution: v1alpha1.DistributionKind,
			expected:     "",
		},
		{
			name:         "context without expected prefix for k3d",
			context:      "my-cluster",
			distribution: v1alpha1.DistributionK3d,
			expected:     "",
		},
		{
			name:         "wrong prefix for kind distribution",
			context:      "k3d-my-cluster",
			distribution: v1alpha1.DistributionKind,
			expected:     "",
		},
		{
			name:         "wrong prefix for k3d distribution",
			context:      "kind-my-cluster",
			distribution: v1alpha1.DistributionK3d,
			expected:     "",
		},
		{
			name:         "kind cluster with default name",
			context:      "kind-kind",
			distribution: v1alpha1.DistributionKind,
			expected:     "kind",
		},
		{
			name:         "k3d cluster with default name",
			context:      "k3d-k3d-default",
			distribution: v1alpha1.DistributionK3d,
			expected:     "k3d-default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := pkgcmd.ExtractClusterNameFromContext(tt.context, tt.distribution)
			if result != tt.expected {
				t.Errorf("extractClusterNameFromContext(%q, %v) = %q; want %q",
					tt.context, tt.distribution, result, tt.expected)
			}
		})
	}
}

func TestGetClusterNameFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(*testing.T) (*v1alpha1.Cluster, clusterprovisioner.Factory)
		expectError bool
		errorMsg    string
		expected    string
	}{
		{
			name: "nil config returns error",
			setup: func(t *testing.T) (*v1alpha1.Cluster, clusterprovisioner.Factory) {
				t.Helper()
				factory := clusterprovisioner.NewMockFactory(t)
				return nil, factory
			},
			expectError: true,
			errorMsg:    "cluster configuration is required",
		},
		{
			name: "kind cluster with explicit context",
			setup: func(t *testing.T) (*v1alpha1.Cluster, clusterprovisioner.Factory) {
				t.Helper()
				cfg := v1alpha1.NewCluster()
				cfg.Spec.Distribution = v1alpha1.DistributionKind
				cfg.Spec.Connection.Context = "kind-my-cluster"
				factory := clusterprovisioner.NewMockFactory(t)
				return cfg, factory
			},
			expectError: false,
			expected:    "my-cluster",
		},
		{
			name: "k3d cluster with explicit context",
			setup: func(t *testing.T) (*v1alpha1.Cluster, clusterprovisioner.Factory) {
				t.Helper()
				cfg := v1alpha1.NewCluster()
				cfg.Spec.Distribution = v1alpha1.DistributionK3d
				cfg.Spec.Connection.Context = "k3d-test-cluster"
				factory := clusterprovisioner.NewMockFactory(t)
				return cfg, factory
			},
			expectError: false,
			expected:    "test-cluster",
		},
		{
			name: "empty context with factory error",
			setup: func(t *testing.T) (*v1alpha1.Cluster, clusterprovisioner.Factory) {
				t.Helper()
				cfg := v1alpha1.NewCluster()
				cfg.Spec.Distribution = v1alpha1.DistributionKind
				cfg.Spec.Connection.Context = ""
				factory := &errorFactory{err: errFactoryError}
				return cfg, factory
			},
			expectError: true,
			errorMsg:    "failed to load distribution config",
		},
		{
			name: "invalid context format falls back to config",
			setup: func(t *testing.T) (*v1alpha1.Cluster, clusterprovisioner.Factory) {
				t.Helper()
				cfg := v1alpha1.NewCluster()
				cfg.Spec.Distribution = v1alpha1.DistributionKind
				cfg.Spec.Connection.Context = "invalid-context"
				// This should fall back to factory, which will error
				factory := &errorFactory{err: errFactoryError}
				return cfg, factory
			},
			expectError: true,
			errorMsg:    "failed to load distribution config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, factory := tt.setup(t)

			result, err := pkgcmd.GetClusterNameFromConfig(cfg, factory)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errorMsg)
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("GetClusterNameFromConfig() = %q; want %q", result, tt.expected)
				}
			}
		})
	}
}

