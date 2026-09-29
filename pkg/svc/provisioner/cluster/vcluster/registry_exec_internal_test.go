package vclusterprovisioner

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEnableRegistryHostsOnNodeRejectsRunningExec(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	client.On("ContainerExecCreate", ctx, "node", testifymock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id"}, nil).Once()
	connection, peer := net.Pipe()
	defer peer.Close()
	client.On("ContainerExecAttach", ctx, "exec-id", testifymock.Anything).
		Return(types.HijackedResponse{Reader: bufio.NewReader(strings.NewReader("")), Conn: connection}, nil).Once()
	client.On("ContainerExecInspect", ctx, "exec-id").
		Return(container.ExecInspect{Running: true, ExitCode: 0}, nil).Once()

	err := enableRegistryHostsOnNode(ctx, client, "node")
	require.ErrorContains(t, err, "still running")
}

func TestEnableRegistryHostsOnNodeRejectsBrokenStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	client.On("ContainerExecCreate", ctx, "node", testifymock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id"}, nil).Once()
	reader, writer := io.Pipe()
	require.NoError(t, writer.CloseWithError(errors.New("broken exec stream")))
	defer reader.Close()
	connection, peer := net.Pipe()
	defer peer.Close()
	client.On("ContainerExecAttach", ctx, "exec-id", testifymock.Anything).
		Return(types.HijackedResponse{Reader: bufio.NewReader(reader), Conn: connection}, nil).Once()
	client.On("ContainerExecInspect", ctx, "exec-id").
		Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()

	err := enableRegistryHostsOnNode(ctx, client, "node")
	require.ErrorContains(t, err, "broken exec stream")
}
