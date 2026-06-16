package dockerutil_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/dockerutil"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

	err := dockerutil.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ dockerpkg.Client) error { return nil },
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

	err := dockerutil.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ dockerpkg.Client) error {
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

	err := dockerutil.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ dockerpkg.Client) error { return nil },
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

	err := dockerutil.WithDockerClientInstance(
		cmd,
		mockClient,
		func(_ dockerpkg.Client) error {
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
	mockClient.EXPECT().Ping(mock.Anything).Return(dockertypes.Ping{APIVersion: "1.42"}, nil)

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	var receivedVersion string

	err := dockerutil.WithDockerClientInstance(cmd, mockClient, func(c dockerpkg.Client) error {
		ping, pingErr := c.Ping(t.Context())
		receivedVersion = ping.APIVersion

		if pingErr != nil {
			return fmt.Errorf("ping: %w", pingErr)
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "1.42", receivedVersion)
}
