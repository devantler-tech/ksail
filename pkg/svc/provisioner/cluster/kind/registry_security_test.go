package kindprovisioner_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	docker "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Static error for testing.
var errSecurityExecCreateFailed = errors.New("exec create failed")

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

func newMockHijackedResponse() types.HijackedResponse {
	return types.HijackedResponse{
		Reader: bufio.NewReader(strings.NewReader("")),
		Conn:   &mockConn{},
	}
}

func TestEscapeShellArg(t *testing.T) { //nolint:funlen // table-driven test with many cases
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simpleString",
			input:    "docker.io",
			expected: "'docker.io'",
		},
		{
			name:     "stringWithSpaces",
			input:    "docker io",
			expected: "'docker io'",
		},
		{
			name:     "stringWithSingleQuote",
			input:    "docker'io",
			expected: "'docker'\\''io'",
		},
		{
			name:     "stringWithMultipleSingleQuotes",
			input:    "a'b'c",
			expected: "'a'\\''b'\\''c'",
		},
		{
			name:     "stringWithSemicolon",
			input:    "docker.io;rm -rf /",
			expected: "'docker.io;rm -rf /'",
		},
		{
			name:     "stringWithBacktick",
			input:    "docker.io`whoami`",
			expected: "'docker.io`whoami`'",
		},
		{
			name:     "stringWithDollarSign",
			input:    "docker.io$(whoami)",
			expected: "'docker.io$(whoami)'",
		},
		{
			name:     "emptyString",
			input:    "",
			expected: "''",
		},
		{
			name:     "complexInjectionAttempt",
			input:    "'; rm -rf / #",
			expected: "''\\''; rm -rf / #'",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := kindprovisioner.EscapeShellArg(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestConfigureContainerdRegistryMirrors_NilMirrorSpecs(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		nil,
		mockClient,
		io.Discard,
	)

	assert.NoError(t, err)
}

func TestConfigureContainerdRegistryMirrors_EmptyMirrorSpecs(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		[]registry.MirrorSpec{},
		mockClient,
		io.Discard,
	)

	assert.NoError(t, err)
}

func TestConfigureContainerdRegistryMirrors_NoKindNodes(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	mirrorSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	// Mock ContainerList to return no containers
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		mirrorSpecs,
		mockClient,
		io.Discard,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, kindprovisioner.ErrNoKindNodes)
}

func TestConfigureContainerdRegistryMirrors_ContainerListError(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	mirrorSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	// Mock ContainerList to return an error
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(nil, errContainerListFailed).
		Once()

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		mirrorSpecs,
		mockClient,
		io.Discard,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to list Kind nodes")
}

func TestConfigureContainerdRegistryMirrors_WithExtraMounts(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	// Kind config with extraMounts for docker.io (should skip injection)
	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
		Nodes: []v1alpha4.Node{
			{
				ExtraMounts: []v1alpha4.Mount{
					{
						HostPath:      "/tmp/kind-mirrors/docker.io",
						ContainerPath: "/etc/containerd/certs.d/docker.io",
					},
				},
			},
		},
	}

	mirrorSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	// Should not need to inject since docker.io has an extraMount
	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		mirrorSpecs,
		mockClient,
		io.Discard,
	)

	require.NoError(t, err)
	// Verify no ContainerList was called (no injection needed)
	mockClient.AssertNotCalled(t, "ContainerList", mock.Anything, mock.Anything)
}

func TestConfigureContainerdRegistryMirrors_SuccessfulInjection(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	mirrorSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	// Mock ContainerList to return one Kind node
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names: []string{"/test-cluster-control-plane"},
				Labels: map[string]string{
					"io.x-k8s.kind.cluster": "test-cluster",
				},
			},
		}, nil).
		Once()

	// Mock ContainerExecCreate
	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-cluster-control-plane", mock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id"}, nil).
		Once()

	// Mock ContainerExecAttach
	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id", mock.Anything).
		Return(newMockHijackedResponse(), nil).
		Once()

	// Mock ContainerExecInspect to return success
	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id").
		Return(container.ExecInspect{ExitCode: 0}, nil).
		Once()

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		mirrorSpecs,
		mockClient,
		io.Discard,
	)

	require.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_ExecCreateFailure(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	mirrorSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	// Mock ContainerList to return one Kind node
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names: []string{"/test-cluster-control-plane"},
				Labels: map[string]string{
					"io.x-k8s.kind.cluster": "test-cluster",
				},
			},
		}, nil).
		Once()

	// Mock ContainerExecCreate to fail
	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-cluster-control-plane", mock.Anything).
		Return(container.ExecCreateResponse{}, errSecurityExecCreateFailed).
		Once()

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		mirrorSpecs,
		mockClient,
		io.Discard,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to inject hosts.toml")
}

func TestConfigureContainerdRegistryMirrors_NonZeroExitCode(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	ctx := context.Background()

	kindConfig := &v1alpha4.Cluster{
		Name: "test-cluster",
	}

	mirrorSpecs := []registry.MirrorSpec{
		{Host: "docker.io", Remote: "https://registry-1.docker.io"},
	}

	// Mock ContainerList to return one Kind node
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names: []string{"/test-cluster-control-plane"},
				Labels: map[string]string{
					"io.x-k8s.kind.cluster": "test-cluster",
				},
			},
		}, nil).
		Once()

	// Mock ContainerExecCreate
	mockClient.EXPECT().
		ContainerExecCreate(ctx, "test-cluster-control-plane", mock.Anything).
		Return(container.ExecCreateResponse{ID: "exec-id"}, nil).
		Once()

	// Mock ContainerExecAttach
	mockClient.EXPECT().
		ContainerExecAttach(ctx, "exec-id", mock.Anything).
		Return(newMockHijackedResponse(), nil).
		Once()

	// Mock ContainerExecInspect to return non-zero exit code
	mockClient.EXPECT().
		ContainerExecInspect(ctx, "exec-id").
		Return(container.ExecInspect{ExitCode: 1}, nil).
		Once()

	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		ctx,
		kindConfig,
		mirrorSpecs,
		mockClient,
		io.Discard,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, kindprovisioner.ErrExecFailed)
}
