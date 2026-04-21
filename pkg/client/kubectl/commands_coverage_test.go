package kubectl_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDebugCommand(t *testing.T) {
	t.Parallel()

	client := kubectl.NewClient(createTestIOStreams())
	cmd := client.CreateDebugCommand("")

	require.NotNil(t, cmd)
	assert.Equal(t, "debug", cmd.Use)
	assert.Contains(t, cmd.Short, "debugging")
	assert.NotEmpty(t, cmd.Long)
}

func TestCreateDebugCommandHasFlags(t *testing.T) {
	t.Parallel()

	client := kubectl.NewClient(createTestIOStreams())
	cmd := client.CreateDebugCommand("/tmp/kubeconfig")

	require.NotNil(t, cmd)
	// Debug command has standard kubeconfig flag from configFlags
	flags := cmd.Flags()
	require.NotNil(t, flags)
	assert.NotNil(t, flags.Lookup("kubeconfig"), "expected --kubeconfig flag")
}

func TestCreateWaitCommand(t *testing.T) {
	t.Parallel()

	client := kubectl.NewClient(createTestIOStreams())
	cmd := client.CreateWaitCommand("")

	require.NotNil(t, cmd)
	assert.Equal(t, "wait", cmd.Use)
	assert.Contains(t, cmd.Short, "Wait")
	assert.NotEmpty(t, cmd.Long)
}

func TestCreateWaitCommandHasFlags(t *testing.T) {
	t.Parallel()

	client := kubectl.NewClient(createTestIOStreams())
	cmd := client.CreateWaitCommand("/tmp/kubeconfig")

	require.NotNil(t, cmd)
	flags := cmd.Flags()
	require.NotNil(t, flags)
	assert.NotNil(t, flags.Lookup("for"), "expected --for flag")
	assert.NotNil(t, flags.Lookup("timeout"), "expected --timeout flag")
}

func TestCreateDebugCommand_WithKubeconfig(t *testing.T) {
	t.Parallel()

	client := kubectl.NewClient(createTestIOStreams())

	// With empty kubeconfig
	cmd1 := client.CreateDebugCommand("")
	require.NotNil(t, cmd1)

	// With a kubeconfig path
	cmd2 := client.CreateDebugCommand("/path/to/kubeconfig")
	require.NotNil(t, cmd2)
}

func TestCreateWaitCommand_WithKubeconfig(t *testing.T) {
	t.Parallel()

	client := kubectl.NewClient(createTestIOStreams())

	// With empty kubeconfig
	cmd1 := client.CreateWaitCommand("")
	require.NotNil(t, cmd1)

	// With a kubeconfig path
	cmd2 := client.CreateWaitCommand("/path/to/kubeconfig")
	require.NotNil(t, cmd2)
}
