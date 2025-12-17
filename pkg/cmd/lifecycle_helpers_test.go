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

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	pkgcmd "github.com/devantler-tech/ksail-go/pkg/cmd"
	ksailconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
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
