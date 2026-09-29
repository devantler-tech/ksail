package vclusterprovisioner_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestConfigureContainerdRegistryMirrors_EnablesHostsDirectory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := dockerclient.NewMockAPIClient(t)
	node := "vcluster.cp.test-cluster"
	var commands [][]string

	mockClient.On("ContainerList", ctx, testifymock.Anything).
		Return([]container.Summary{{Names: []string{"/" + node}}}, nil).Once()
	mockClient.On("ContainerExecCreate", ctx, node, testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			commands = append(commands, args.Get(2).(container.ExecOptions).Cmd)
		}).
		Return(container.ExecCreateResponse{ID: "exec-id"}, nil).Twice()
	connection, peer := net.Pipe()
	defer peer.Close()
	mockClient.On("ContainerExecAttach", ctx, "exec-id", testifymock.Anything).
		Return(types.HijackedResponse{
			Reader: bufio.NewReader(strings.NewReader("")),
			Conn:   connection,
		}, nil).Twice()
	mockClient.On("ContainerExecInspect", ctx, "exec-id").
		Return(container.ExecInspect{ExitCode: 0}, nil).Twice()

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		"test-cluster",
		[]registry.MirrorSpec{{Host: "docker.io", Remote: "https://registry-1.docker.io"}},
		mockClient,
		io.Discard,
	)

	require.NoError(t, err)
	require.Len(t, commands, 2, "node must configure containerd after writing hosts.toml")
	require.Contains(t, strings.Join(commands[1], " "), "config_path")
	require.Contains(t, strings.Join(commands[1], " "), "systemctl restart containerd")
	mockClient.AssertExpectations(t)
}

var errMockContainerList = errors.New("container list failed")

func TestSetupRegistries_EmptySpecs(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)

	err := vclusterprovisioner.SetupRegistries(
		context.Background(),
		"test-cluster",
		mockClient,
		[]registry.MirrorSpec{},
		io.Discard,
	)

	require.NoError(t, err, "SetupRegistries() with empty specs should not error")
	mockClient.AssertExpectations(t)
}

func TestConnectRegistriesToNetwork_EmptySpecs(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)

	err := vclusterprovisioner.ConnectRegistriesToNetwork(
		context.Background(),
		[]registry.MirrorSpec{},
		"test-cluster",
		mockClient,
		io.Discard,
	)

	require.NoError(t, err, "ConnectRegistriesToNetwork() with empty specs should not error")
	mockClient.AssertExpectations(t)
}

func TestCleanupRegistries_EmptySpecs(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)

	err := vclusterprovisioner.CleanupRegistries(
		context.Background(),
		[]registry.MirrorSpec{},
		"test-cluster",
		mockClient,
		false,
	)

	require.NoError(t, err, "CleanupRegistries() with empty specs should not error")
	mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_EmptySpecs(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"test-cluster",
		[]registry.MirrorSpec{},
		mockClient,
		io.Discard,
	)

	require.NoError(t, err,
		"ConfigureContainerdRegistryMirrors() with empty specs should not error")
	mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_NoNodes(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)

	mockClient.On("ContainerList", testifymock.Anything, testifymock.Anything).
		Return([]container.Summary{}, nil)

	specs := []registry.MirrorSpec{
		{
			Host:   "docker.io",
			Remote: "https://registry-1.docker.io",
		},
	}

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"test-cluster",
		specs,
		mockClient,
		io.Discard,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, vclusterprovisioner.ErrNoVClusterNodes)
	mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_ListError(t *testing.T) {
	t.Parallel()

	mockClient := dockerclient.NewMockAPIClient(t)

	mockClient.On("ContainerList", testifymock.Anything, testifymock.Anything).
		Return([]container.Summary(nil), errMockContainerList)

	specs := []registry.MirrorSpec{
		{
			Host:   "docker.io",
			Remote: "https://registry-1.docker.io",
		},
	}

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"test-cluster",
		specs,
		mockClient,
		io.Discard,
	)

	require.ErrorIs(t, err, errMockContainerList)
	require.ErrorContains(t, err, "container list failed", "error should contain list error")
	mockClient.AssertExpectations(t)
}
