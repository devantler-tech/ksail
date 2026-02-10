package docker_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers/docker"
	dockerpkg "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Error variables for test cases.
var (
	errOperationFailed = errors.New("operation failed")
	errCloseFailed     = errors.New("close failed")
)

func TestWithDockerClientInstance_Success(t *testing.T) {
	t.Parallel()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().Close().Return(nil)

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := docker.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ client.APIClient) error { return nil },
	)

	require.NoError(t, err)
}

func TestWithDockerClientInstance_OperationError(t *testing.T) {
	t.Parallel()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().Close().Return(nil)

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := docker.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ client.APIClient) error {
			return fmt.Errorf("test: %w", errOperationFailed)
		},
	)

	require.Error(t, err)
}

func TestWithDockerClientInstance_CloseError(t *testing.T) {
	t.Parallel()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().Close().Return(fmt.Errorf("test: %w", errCloseFailed))

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := docker.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ client.APIClient) error { return nil },
	)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "cleanup warning")
	snaps.MatchSnapshot(t, buf.String())
}

func TestWithDockerClientInstance_BothErrors(t *testing.T) {
	t.Parallel()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().Close().Return(fmt.Errorf("test: %w", errCloseFailed))

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	err := docker.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ client.APIClient) error {
			return fmt.Errorf("first: %w", errOperationFailed)
		},
	)

	require.Error(t, err)
	snaps.MatchSnapshot(t, "error: "+err.Error()+"\noutput: "+buf.String())
}

func TestWithDockerClientInstance_ClientPassedToOperation(t *testing.T) {
	t.Parallel()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().Close().Return(nil)
	mockClient.EXPECT().ClientVersion().Return("1.42")

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	var receivedVersion string

	err := docker.WithDockerClientInstance(cmd, mockClient, func(c client.APIClient) error {
		receivedVersion = c.ClientVersion()

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "1.42", receivedVersion)
}
