package image_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	errExecCreateFailed  = errors.New("exec create failed")
	errExecAttachFailed  = errors.New("exec attach failed")
	errExecInspectFailed = errors.New("exec inspect failed")
)

// mockConn is a minimal net.Conn implementation for testing.
type mockConn struct{}

func (m *mockConn) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (m *mockConn) Write(b []byte) (int, error)        { return len(b), nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

// mockDockerStreamResponse creates a Docker HijackedResponse for testing.
// The data is wrapped in Docker's multiplexed stream format.
func mockDockerStreamResponse(stdout, stderr string) dockertypes.HijackedResponse {
	// Docker exec uses multiplexed streams with 8-byte header:
	// [0] = stream type (1=stdout, 2=stderr)
	// [1-3] = reserved
	// [4-7] = size of payload (big-endian uint32)
	var data []byte

	if stdout != "" {
		header := make([]byte, 8)
		header[0] = 1 // stdout
		payload := []byte(stdout)
		header[4] = byte(len(payload) >> 24)
		header[5] = byte(len(payload) >> 16)
		header[6] = byte(len(payload) >> 8)
		header[7] = byte(len(payload))
		data = append(data, header...)
		data = append(data, payload...)
	}

	if stderr != "" {
		header := make([]byte, 8)
		header[0] = 2 // stderr
		payload := []byte(stderr)
		header[4] = byte(len(payload) >> 24)
		header[5] = byte(len(payload) >> 16)
		header[6] = byte(len(payload) >> 8)
		header[7] = byte(len(payload))
		data = append(data, header...)
		data = append(data, payload...)
	}

	return dockertypes.HijackedResponse{
		Reader: bufio.NewReader(strings.NewReader(string(data))),
		Conn:   &mockConn{},
	}
}

func TestNewContainerExecutor(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	executor := image.NewContainerExecutor(mockClient)

	assert.NotNil(t, executor, "NewContainerExecutor should return a non-nil executor")
}

func TestExecInContainerSuccessfulCommand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-container", mock.MatchedBy(func(opts container.ExecOptions) bool {
			return opts.AttachStdout && opts.AttachStderr &&
				len(opts.Cmd) == 2 && opts.Cmd[0] == "echo" && opts.Cmd[1] == "hello"
		})).
		Return(container.ExecCreateResponse{ID: "exec-id-123"}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id-123", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("hello\n", ""), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id-123").
		Return(container.ExecInspect{ExitCode: 0}, nil)

	executor := image.NewContainerExecutor(mockClient)
	got, err := executor.ExecInContainer(ctx, "test-container", []string{"echo", "hello"})

	require.NoError(t, err)
	assert.Equal(t, "hello\n", got)
}

func TestExecInContainerEmptyOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-container", mock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id-456"}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id-456", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id-456").
		Return(container.ExecInspect{ExitCode: 0}, nil)

	executor := image.NewContainerExecutor(mockClient)
	got, err := executor.ExecInContainer(ctx, "test-container", []string{"true"})

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestExecInContainerCreateFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-container", mock.Anything).
		Return(container.ExecCreateResponse{}, errExecCreateFailed)

	executor := image.NewContainerExecutor(mockClient)
	_, err := executor.ExecInContainer(ctx, "test-container", []string{"echo", "fail"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create exec")
}

func TestExecInContainerAttachFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-container", mock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id-789"}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id-789", container.ExecStartOptions{}).
		Return(dockertypes.HijackedResponse{}, errExecAttachFailed)

	executor := image.NewContainerExecutor(mockClient)
	_, err := executor.ExecInContainer(ctx, "test-container", []string{"echo", "fail"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to attach to exec")
}

func TestExecInContainerInspectFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-container", mock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id-abc"}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id-abc", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id-abc").
		Return(container.ExecInspect{}, errExecInspectFailed)

	executor := image.NewContainerExecutor(mockClient)
	_, err := executor.ExecInContainer(ctx, "test-container", []string{"echo", "fail"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to inspect exec")
}

func TestExecInContainerNonZeroExitCode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-container", mock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id-def"}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id-def", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "command failed\n"), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id-def").
		Return(container.ExecInspect{ExitCode: 1}, nil)

	executor := image.NewContainerExecutor(mockClient)
	_, err := executor.ExecInContainer(ctx, "test-container", []string{"false"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "container exec failed")
}

func TestExecInContainerMultipleArguments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	mockClient.EXPECT().
		ContainerExecCreate(ctx, "my-node", mock.MatchedBy(func(opts container.ExecOptions) bool {
			return len(opts.Cmd) == 5 &&
				opts.Cmd[0] == "ctr" &&
				opts.Cmd[1] == "--namespace=k8s.io" &&
				opts.Cmd[2] == "images" &&
				opts.Cmd[3] == "list" &&
				opts.Cmd[4] == "-q"
		})).
		Return(container.ExecCreateResponse{ID: "exec-id-ghi"}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id-ghi", container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("nginx:latest\nredis:alpine\n", ""), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id-ghi").
		Return(container.ExecInspect{ExitCode: 0}, nil)

	executor := image.NewContainerExecutor(mockClient)
	cmd := []string{"ctr", "--namespace=k8s.io", "images", "list", "-q"}
	got, err := executor.ExecInContainer(ctx, "my-node", cmd)

	require.NoError(t, err)
	assert.Equal(t, "nginx:latest\nredis:alpine\n", got)
}
