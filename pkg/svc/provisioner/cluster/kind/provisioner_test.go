package kindprovisioner_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	cmdrunner "github.com/devantler-tech/ksail/v5/pkg/runner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	errCreateClusterFailed = errors.New("create cluster failed")
	errDeleteClusterFailed = errors.New("delete cluster failed")
	errListClustersFailed  = errors.New("list clusters failed")
	errStartClusterFailed  = errors.New("start cluster failed")
	errStopClusterFailed   = errors.New("stop cluster failed")
)

// mockCommandRunner is a test helper that mocks the command runner.
type mockCommandRunner struct {
	mock.Mock

	lastArgs []string
}

func (m *mockCommandRunner) Run(
	_ context.Context,
	_ *cobra.Command,
	args []string,
) (cmdrunner.CommandResult, error) {
	callArgs := m.Called()

	// capture last arguments for tests that need to assert CLI flags
	m.lastArgs = append([]string(nil), args...)

	result, ok := callArgs.Get(0).(cmdrunner.CommandResult)
	if !ok {
		err := callArgs.Error(1)
		if err != nil {
			return cmdrunner.CommandResult{}, fmt.Errorf("mock run error: %w", err)
		}

		return cmdrunner.CommandResult{}, nil
	}

	err := callArgs.Error(1)
	if err != nil {
		return result, fmt.Errorf("mock run error: %w", err)
	}

	return result, nil
}

func TestCreateSuccess(t *testing.T) {
	t.Parallel()

	runProvisionerRunnerSuccessTest(
		t,
		"Create",
		func(ctx context.Context, provisioner *kindprovisioner.Provisioner, name string) error {
			return provisioner.Create(ctx, name)
		},
	)
}

func TestCreateErrorCreateFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	// Mock command runner to return error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errCreateClusterFailed)

	err := provisioner.Create(context.Background(), "my-cluster")

	require.ErrorIs(t, err, errCreateClusterFailed, "Create()")
}

func TestDeleteSuccess(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		inputName   string
		clusterName string
	}{
		{
			name:        "without_name_uses_cfg",
			inputName:   "",
			clusterName: "cfg-name", // from newProvisionerForTest cfg.Name
		},
		{
			name:        "with_name",
			inputName:   "my-cluster",
			clusterName: "my-cluster",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			provisioner, _, runner := newProvisionerForTest(t)

			// First call: List (via Exists) - return cluster name so cluster exists
			runner.On("Run").Return(cmdrunner.CommandResult{
				Stdout: testCase.clusterName + "\n",
			}, nil).Once()
			// Second call: Delete
			runner.On("Run").Return(cmdrunner.CommandResult{}, nil).Once()

			err := provisioner.Delete(context.Background(), testCase.inputName)
			require.NoError(t, err, "Delete()")
		})
	}
}

func TestDeleteIncludesKubeconfigFlag(t *testing.T) {
	t.Parallel()

	provisioner, _, runner := newProvisionerForTest(t)
	// First call: List (via Exists) - return cluster name so cluster exists
	runner.On("Run").Return(cmdrunner.CommandResult{
		Stdout: "cfg-name\n",
	}, nil).Once()
	// Second call: Delete
	runner.On("Run").Return(cmdrunner.CommandResult{}, nil).Once()

	err := provisioner.Delete(context.Background(), "")

	require.NoError(t, err, "Delete()")
	require.Contains(t, runner.lastArgs, "--kubeconfig", "Delete() should pass kubeconfig flag")
}

func TestCreateUsesProvidedName(t *testing.T) {
	t.Parallel()

	assertNameFlagPropagation(t, func(p *kindprovisioner.Provisioner) error {
		return p.Create(context.Background(), "custom-cluster")
	}, "custom-cluster")
}

func TestCreateUsesConfigNameWhenEmpty(t *testing.T) {
	t.Parallel()

	assertNameFlagPropagation(t, func(p *kindprovisioner.Provisioner) error {
		return p.Create(context.Background(), "")
	}, "cfg-name")
}

func TestDeleteUsesProvidedName(t *testing.T) {
	t.Parallel()

	provisioner, _, runner := newProvisionerForTest(t)
	// First call: List (via Exists) - return cluster name so cluster exists
	runner.On("Run").Return(cmdrunner.CommandResult{
		Stdout: "delete-me\n",
	}, nil).Once()
	// Second call: Delete
	runner.On("Run").Return(cmdrunner.CommandResult{}, nil).Once()

	err := provisioner.Delete(context.Background(), "delete-me")

	require.NoError(t, err)
	assertFlagValue(t, runner.lastArgs, "--name", "delete-me")
}

func TestDeleteErrorDeleteFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	// First call: List (via Exists) - return cluster name so cluster exists
	runner.On("Run").Return(cmdrunner.CommandResult{
		Stdout: "bad\n",
	}, nil).Once()
	// Second call: Delete - returns error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errDeleteClusterFailed).Once()

	err := provisioner.Delete(context.Background(), "bad")

	require.ErrorIs(t, err, errDeleteClusterFailed, "Delete()")
}

func TestDeleteErrorClusterNotFound(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	// Mock List to return no clusters
	runner.On("Run").Return(cmdrunner.CommandResult{
		Stdout: "",
	}, nil).Once()

	err := provisioner.Delete(context.Background(), "nonexistent")

	require.ErrorIs(t, err, clustererr.ErrClusterNotFound, "Delete()")
}

func TestExistsSuccessFalse(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	// Mock command runner to return cluster names that don't include "not-here"
	runner.On("Run").
		Return(cmdrunner.CommandResult{Stdout: "x\ny\n", Stderr: ""}, nil)

	exists, err := provisioner.Exists(context.Background(), "not-here")
	if err != nil {
		t.Fatalf("Exists() unexpected error: %v", err)
	}

	if exists {
		t.Fatalf("Exists() got true, want false")
	}
}

func TestExistsSuccessTrue(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	// Mock command runner to return cluster names including cfg-name
	runner.On("Run").
		Return(cmdrunner.CommandResult{Stdout: "x\ncfg-name\n", Stderr: ""}, nil)

	exists, err := provisioner.Exists(context.Background(), "")
	if err != nil {
		t.Fatalf("Exists() unexpected error: %v", err)
	}

	if !exists {
		t.Fatalf("Exists() got false, want true")
	}
}

func TestExistsErrorListFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	// Mock command runner to return error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errListClustersFailed)

	exists, err := provisioner.Exists(context.Background(), "any")

	if exists {
		t.Fatalf("Exists() got true, want false when error occurs")
	}

	require.ErrorIs(t, err, errListClustersFailed, "Exists()")
	assert.ErrorContains(t, err, "failed to list kind clusters", "Exists()")
}

func TestListSuccess(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)
	// Mock command runner to return cluster names
	runner.On("Run").
		Return(cmdrunner.CommandResult{Stdout: "a\nb\n", Stderr: ""}, nil)

	got, err := provisioner.List(context.Background())

	require.NoError(t, err, "List()")
	assert.Equal(t, []string{"a", "b"}, got, "List()")
}

func TestListErrorListFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)
	// Mock command runner to return error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errListClustersFailed)

	_, err := provisioner.List(context.Background())

	require.ErrorIs(t, err, errListClustersFailed, "List()")
	assert.ErrorContains(t, err, "failed to list kind clusters", "List()")
}

func TestListFiltersNoKindClustersMessage(t *testing.T) {
	t.Parallel()
	provisioner, _, runner := newProvisionerForTest(t)

	runner.On("Run").Return(cmdrunner.CommandResult{
		Stdout: "No kind clusters found.\n",
		Stderr: "",
	}, nil)

	got, err := provisioner.List(context.Background())

	require.NoError(t, err, "List()")
	require.Empty(t, got, "List() should ignore 'No kind clusters found.' message")
}

func TestStartErrorClusterNotFound(t *testing.T) {
	t.Parallel()
	runClusterNotFoundTest(t, "Start", func(p *kindprovisioner.Provisioner) error {
		return p.Start(context.Background(), "")
	})
}

func TestStartErrorProviderFailed(t *testing.T) {
	t.Parallel()
	provisioner, infraProvider, _ := newProvisionerForTest(t)
	infraProvider.On("StartNodes", mock.Anything, "cfg-name").Return(errStartClusterFailed)

	err := provisioner.Start(context.Background(), "")
	if err == nil {
		t.Fatalf("Start() expected error, got nil")
	}
}

func TestStartSuccess(t *testing.T) {
	t.Parallel()
	provisioner, infraProvider, _ := newProvisionerForTest(t)

	// Mock successful start
	infraProvider.On("StartNodes", mock.Anything, "cfg-name").Return(nil)

	err := provisioner.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}
}

func TestStartErrorDockerStartFailed(t *testing.T) {
	t.Parallel()
	runProviderOperationFailureTest(
		t,
		func(p *kindprovisioner.Provisioner) error { return p.Start(context.Background(), "") },
		"Start",
		func(infraProvider *provider.MockProvider) {
			infraProvider.On("StartNodes", mock.Anything, "cfg-name").Return(errStartClusterFailed)
		},
		"failed to start cluster",
	)
}

func TestStopErrorClusterNotFound(t *testing.T) {
	t.Parallel()
	runClusterNotFoundTest(t, "Stop", func(p *kindprovisioner.Provisioner) error {
		return p.Stop(context.Background(), "")
	})
}

func TestStopErrorProviderFailed(t *testing.T) {
	t.Parallel()
	provisioner, infraProvider, _ := newProvisionerForTest(t)
	infraProvider.On("StopNodes", mock.Anything, "cfg-name").Return(errStopClusterFailed)

	err := provisioner.Stop(context.Background(), "")
	if err == nil {
		t.Fatalf("Stop() expected error, got nil")
	}
}

func TestStopErrorDockerStopFailed(t *testing.T) {
	t.Parallel()
	runProviderOperationFailureTest(
		t,
		func(p *kindprovisioner.Provisioner) error { return p.Stop(context.Background(), "") },
		"Stop",
		func(infraProvider *provider.MockProvider) {
			infraProvider.On("StopNodes", mock.Anything, "cfg-name").Return(errStopClusterFailed)
		},
		"failed to stop cluster",
	)
}

func TestStopSuccess(t *testing.T) {
	t.Parallel()
	provisioner, infraProvider, _ := newProvisionerForTest(t)

	// Mock successful stop
	infraProvider.On("StopNodes", mock.Anything, "cfg-name").Return(nil)

	err := provisioner.Stop(context.Background(), "")
	if err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}
}

// --- internals ---

func newProvisionerForTest(
	t *testing.T,
) (
	*kindprovisioner.Provisioner,
	*provider.MockProvider,
	*mockCommandRunner,
) {
	t.Helper()
	kindProvider := kindprovisioner.NewMockProvider(t)
	infraProvider := provider.NewMockProvider()
	runner := &mockCommandRunner{}

	cfg := &v1alpha4.Cluster{
		Name: "cfg-name",
		TypeMeta: v1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
	}
	provisioner := kindprovisioner.NewProvisionerWithRunner(
		cfg,
		"~/.kube/config",
		kindProvider,
		infraProvider,
		runner,
	)

	return provisioner, infraProvider, runner
}

// helper to DRY up the repeated "cluster not found" error scenario for Start/Stop.
func runClusterNotFoundTest(
	t *testing.T,
	actionName string,
	action func(*kindprovisioner.Provisioner) error,
) {
	t.Helper()
	provisioner, infraProvider, _ := newProvisionerForTest(t)
	// Mock StartNodes/StopNodes to return ErrNoNodes
	infraProvider.On("StartNodes", mock.Anything, "cfg-name").Return(provider.ErrNoNodes).Maybe()
	infraProvider.On("StopNodes", mock.Anything, "cfg-name").Return(provider.ErrNoNodes).Maybe()

	err := action(provisioner)
	if err == nil {
		t.Fatalf("%s() expected error, got nil", actionName)
	}
}

// runProviderOperationFailureTest is a helper for testing provider operation failures.
func runProviderOperationFailureTest(
	t *testing.T,
	operation func(*kindprovisioner.Provisioner) error,
	operationName string,
	expectProviderCall func(*provider.MockProvider),
	expectedErrorMsg string,
) {
	t.Helper()
	provisioner, infraProvider, _ := newProvisionerForTest(t)

	expectProviderCall(infraProvider)

	err := operation(provisioner)
	if err == nil {
		t.Fatalf("%s() expected error, got nil", operationName)
	}

	if expectedErrorMsg != "" && !assert.Contains(t, err.Error(), expectedErrorMsg) {
		t.Fatalf("%s() error should contain %q, got: %v", operationName, expectedErrorMsg, err)
	}
}

func runProvisionerRunnerSuccessTest(
	t *testing.T,
	actionName string,
	action func(context.Context, *kindprovisioner.Provisioner, string) error,
) {
	t.Helper()

	testCases := []struct {
		name      string
		inputName string
	}{
		{
			name:      "without_name_uses_cfg",
			inputName: "",
		},
		{
			name:      "with_name",
			inputName: "my-cluster",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			provisioner, _, runner := newProvisionerForTest(t)
			runner.On("Run").Return(cmdrunner.CommandResult{}, nil)

			err := action(context.Background(), provisioner, testCase.inputName)
			require.NoErrorf(t, err, "%s()", actionName)
		})
	}
}

func assertFlagValue(t *testing.T, args []string, flag string, expected string) {
	t.Helper()

	for idx := range args {
		if args[idx] == flag {
			if idx+1 >= len(args) {
				t.Fatalf("flag %s missing value in args: %v", flag, args)
			}

			require.Equal(t, expected, args[idx+1], "unexpected value for %s", flag)

			return
		}
	}

	t.Fatalf("flag %s not found in args: %v", flag, args)
}

func assertNameFlagPropagation(
	t *testing.T,
	action func(*kindprovisioner.Provisioner) error,
	expectedName string,
) {
	t.Helper()

	provisioner, _, runner := newProvisionerForTest(t)
	runner.On("Run").Return(cmdrunner.CommandResult{}, nil)

	err := action(provisioner)

	require.NoError(t, err)
	assertFlagValue(t, runner.lastArgs, "--name", expectedName)
}
