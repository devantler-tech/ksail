package kindprovisioner_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	cmdrunner "github.com/devantler-tech/ksail/v5/pkg/cli/runner"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
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
		func(ctx context.Context, provisioner *kindprovisioner.KindClusterProvisioner, name string) error {
			return provisioner.Create(ctx, name)
		},
	)
}

func TestCreateErrorCreateFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, _, runner := newProvisionerForTest(t)

	// Mock command runner to return error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errCreateClusterFailed)

	err := provisioner.Create(context.Background(), "my-cluster")

	require.ErrorIs(t, err, errCreateClusterFailed, "Create()")
}

func TestDeleteSuccess(t *testing.T) {
	t.Parallel()

	runProvisionerRunnerSuccessTest(
		t,
		"Delete",
		func(ctx context.Context, provisioner *kindprovisioner.KindClusterProvisioner, name string) error {
			return provisioner.Delete(ctx, name)
		},
	)
}

func TestDeleteIncludesKubeconfigFlag(t *testing.T) {
	t.Parallel()

	provisioner, _, _, runner := newProvisionerForTest(t)
	runner.On("Run").Return(cmdrunner.CommandResult{}, nil)

	err := provisioner.Delete(context.Background(), "")

	require.NoError(t, err, "Delete()")
	require.Contains(t, runner.lastArgs, "--kubeconfig", "Delete() should pass kubeconfig flag")
}

func TestCreateUsesProvidedName(t *testing.T) {
	t.Parallel()

	assertNameFlagPropagation(t, func(p *kindprovisioner.KindClusterProvisioner) error {
		return p.Create(context.Background(), "custom-cluster")
	}, "custom-cluster")
}

func TestCreateUsesConfigNameWhenEmpty(t *testing.T) {
	t.Parallel()

	assertNameFlagPropagation(t, func(p *kindprovisioner.KindClusterProvisioner) error {
		return p.Create(context.Background(), "")
	}, "cfg-name")
}

func TestDeleteUsesProvidedName(t *testing.T) {
	t.Parallel()

	assertNameFlagPropagation(t, func(p *kindprovisioner.KindClusterProvisioner) error {
		return p.Delete(context.Background(), "delete-me")
	}, "delete-me")
}

func TestDeleteErrorDeleteFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, _, runner := newProvisionerForTest(t)

	// Mock command runner to return error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errDeleteClusterFailed)

	err := provisioner.Delete(context.Background(), "bad")

	require.ErrorIs(t, err, errDeleteClusterFailed, "Delete()")
}

func TestExistsSuccessFalse(t *testing.T) {
	t.Parallel()
	provisioner, _, _, runner := newProvisionerForTest(t)

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
	provisioner, _, _, runner := newProvisionerForTest(t)

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
	provisioner, _, _, runner := newProvisionerForTest(t)

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
	provisioner, _, _, runner := newProvisionerForTest(t)
	// Mock command runner to return cluster names
	runner.On("Run").
		Return(cmdrunner.CommandResult{Stdout: "a\nb\n", Stderr: ""}, nil)

	got, err := provisioner.List(context.Background())

	require.NoError(t, err, "List()")
	assert.Equal(t, []string{"a", "b"}, got, "List()")
}

func TestListErrorListFailed(t *testing.T) {
	t.Parallel()
	provisioner, _, _, runner := newProvisionerForTest(t)
	// Mock command runner to return error
	runner.On("Run").
		Return(cmdrunner.CommandResult{}, errListClustersFailed)

	_, err := provisioner.List(context.Background())

	require.ErrorIs(t, err, errListClustersFailed, "List()")
	assert.ErrorContains(t, err, "failed to list kind clusters", "List()")
}

func TestListFiltersNoKindClustersMessage(t *testing.T) {
	t.Parallel()
	provisioner, _, _, runner := newProvisionerForTest(t)

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
	runClusterNotFoundTest(t, "Start", func(p *kindprovisioner.KindClusterProvisioner) error {
		return p.Start(context.Background(), "")
	})
}

func TestStartErrorNoNodesFound(t *testing.T) {
	t.Parallel()
	provisioner, provider, _, _ := newProvisionerForTest(t)
	provider.On("ListNodes", "cfg-name").Return(nil, errStartClusterFailed)

	err := provisioner.Start(context.Background(), "")
	if err == nil {
		t.Fatalf("Start() expected error, got nil")
	}
}

func TestStartSuccess(t *testing.T) {
	t.Parallel()
	provisioner, provider, client, _ := newProvisionerForTest(t)
	provider.On("ListNodes", "cfg-name").Return([]string{"kind-control-plane", "kind-worker"}, nil)

	// Expect ContainerStart called twice with any args
	client.On("ContainerStart", mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)

	err := provisioner.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}
}

func TestStartErrorDockerStartFailed(t *testing.T) {
	t.Parallel()
	runDockerOperationFailureTest(
		t,
		func(p *kindprovisioner.KindClusterProvisioner) error { return p.Start(context.Background(), "") },
		"Start",
		func(client *docker.MockContainerAPIClient) {
			client.On("ContainerStart", mock.Anything, "kind-control-plane", mock.Anything).
				Return(errStartClusterFailed)
		},
		"docker start failed for kind-control-plane",
	)
}

func TestStopErrorClusterNotFound(t *testing.T) {
	t.Parallel()
	runClusterNotFoundTest(t, "Stop", func(p *kindprovisioner.KindClusterProvisioner) error {
		return p.Stop(context.Background(), "")
	})
}

func TestStopErrorNoNodesFound(t *testing.T) {
	t.Parallel()
	provisioner, provider, _, _ := newProvisionerForTest(t)
	provider.On("ListNodes", "cfg-name").Return(nil, errStopClusterFailed)

	err := provisioner.Stop(context.Background(), "")
	if err == nil {
		t.Fatalf("Stop() expected error, got nil")
	}
}

func TestStopErrorDockerStopFailed(t *testing.T) {
	t.Parallel()
	runDockerOperationFailureTest(
		t,
		func(p *kindprovisioner.KindClusterProvisioner) error { return p.Stop(context.Background(), "") },
		"Stop",
		func(client *docker.MockContainerAPIClient) {
			client.On("ContainerStop", mock.Anything, "kind-control-plane", mock.Anything).
				Return(errStopClusterFailed)
		},
		"docker stop failed for kind-control-plane",
	)
}

func TestStopSuccess(t *testing.T) {
	t.Parallel()
	provisioner, provider, client, _ := newProvisionerForTest(t)
	provider.On("ListNodes", "cfg-name").
		Return([]string{"kind-control-plane", "kind-worker", "kind-worker2"}, nil)

	client.On("ContainerStop", mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(3)

	err := provisioner.Stop(context.Background(), "")
	if err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}
}

// --- internals ---

func newProvisionerForTest(
	t *testing.T,
) (
	*kindprovisioner.KindClusterProvisioner,
	*kindprovisioner.MockKindProvider,
	*docker.MockContainerAPIClient,
	*mockCommandRunner,
) {
	t.Helper()
	provider := kindprovisioner.NewMockKindProvider(t)
	client := docker.NewMockContainerAPIClient(t)
	runner := &mockCommandRunner{}

	cfg := &v1alpha4.Cluster{
		Name: "cfg-name",
		TypeMeta: v1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
	}
	provisioner := kindprovisioner.NewKindClusterProvisionerWithRunner(
		cfg,
		"~/.kube/config",
		provider,
		client,
		runner,
	)

	return provisioner, provider, client, runner
}

// helper to DRY up the repeated "cluster not found" error scenario for Start/Stop.
func runClusterNotFoundTest(
	t *testing.T,
	actionName string,
	action func(*kindprovisioner.KindClusterProvisioner) error,
) {
	t.Helper()
	provisioner, provider, _, _ := newProvisionerForTest(t)
	provider.On("ListNodes", "cfg-name").Return([]string{}, nil)

	err := action(provisioner)
	if err == nil {
		t.Fatalf("%s() expected error, got nil", actionName)
	}

	if !errors.Is(err, kindprovisioner.ErrClusterNotFound) {
		t.Fatalf("%s() error = %v, want ErrClusterNotFound", actionName, err)
	}
}

// runDockerOperationFailureTest is a helper for testing Docker operation failures.
func runDockerOperationFailureTest(
	t *testing.T,
	operation func(*kindprovisioner.KindClusterProvisioner) error,
	operationName string,
	expectDockerCall func(*docker.MockContainerAPIClient),
	expectedErrorMsg string,
) {
	t.Helper()
	provisioner, provider, client, _ := newProvisionerForTest(t)
	provider.On("ListNodes", "cfg-name").Return([]string{"kind-control-plane"}, nil)

	expectDockerCall(client)

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
	action func(context.Context, *kindprovisioner.KindClusterProvisioner, string) error,
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
			provisioner, _, _, runner := newProvisionerForTest(t)
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
	action func(*kindprovisioner.KindClusterProvisioner) error,
	expectedName string,
) {
	t.Helper()

	provisioner, _, _, runner := newProvisionerForTest(t)
	runner.On("Run").Return(cmdrunner.CommandResult{}, nil)

	err := action(provisioner)

	require.NoError(t, err)
	assertFlagValue(t, runner.lastArgs, "--name", expectedName)
}
