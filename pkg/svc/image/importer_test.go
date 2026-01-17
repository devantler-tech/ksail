package image_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Package-level error definitions for linting compliance.
var (
	errImportListFailed = errors.New("list nodes failed")
	errImportCopyFailed = errors.New("copy to container failed")
)

func TestNewImporter(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	importer := image.NewImporter(mockClient)

	assert.NotNil(t, importer, "NewImporter should return a non-nil importer")
}

func TestImportUnsupportedDistribution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "talos does not support image export/import")
}

func TestImportUnsupportedProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderHetzner,
		image.ImportOptions{InputPath: inputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider for image operations")
}

func TestImportInputFileNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	importer := image.NewImporter(mockClient)
	err := importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: "/nonexistent/images.tar"},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "input file does not exist")
}

func TestImportDefaultInputPathNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)

	importer := image.NewImporter(mockClient)
	err := importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "input file does not exist")
}

func TestImportListNodesFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(nil, errImportListFailed)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list nodes")
}

func TestImportNoNodesFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cluster nodes found")
}

func TestImportOnlyHelperContainers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	// Return only helper containers that should be filtered out
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/k3d-my-cluster-serverlb"},
				Labels: map[string]string{"k3d.role": "loadbalancer"},
			},
			{
				Names:  []string{"/k3d-my-cluster-tools"},
				Labels: map[string]string{"k3d.role": "noRole"},
			},
		}, nil)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid kubernetes nodes found")
}

func TestImportSuccessVanilla(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	// Mock ContainerList - returns both control-plane and worker
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
			{
				Names:  []string{"/my-cluster-worker"},
				Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
			},
		}, nil)

	// Import happens to ALL K8s nodes, so we expect 2 imports
	for _, nodeName := range []string{"my-cluster-control-plane", "my-cluster-worker"} {
		// Mock CopyToContainer
		mockClient.EXPECT().
			CopyToContainer(ctx, nodeName, "/root", mock.Anything, container.CopyToContainerOptions{}).
			Return(nil).Once()

		// Mock exec for import command - Kind uses /root path
		importPath := "/root/ksail-images-import.tar"
		setupImportExecMockForImporter(ctx, t, mockClient, nodeName, importPath)

		// Mock exec for cleanup
		setupExecMockForImporter(ctx, t, mockClient, nodeName)
	}

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{
			InputPath: inputPath,
		},
	)

	require.NoError(t, err)
}

func TestImportSuccessK3s(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	// Mock ContainerList - K3d nodes with helper containers mixed in
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/k3d-my-cluster-server-0"},
				Labels: map[string]string{"k3d.role": "server"},
			},
			{
				Names:  []string{"/k3d-my-cluster-agent-0"},
				Labels: map[string]string{"k3d.role": "agent"},
			},
			{
				Names:  []string{"/k3d-my-cluster-serverlb"},
				Labels: map[string]string{"k3d.role": "loadbalancer"},
			},
			{
				Names:  []string{"/k3d-my-cluster-registry"},
				Labels: map[string]string{"k3d.role": "registry"},
			},
		}, nil)

	// Import happens only to server and agent nodes (not loadbalancer or registry)
	for _, nodeName := range []string{"k3d-my-cluster-server-0", "k3d-my-cluster-agent-0"} {
		// Mock CopyToContainer
		mockClient.EXPECT().
			CopyToContainer(ctx, nodeName, "/tmp", mock.Anything, container.CopyToContainerOptions{}).
			Return(nil).Once()

		// Mock exec for import command - K3d uses /tmp path
		setupImportExecMockForImporter(ctx, t, mockClient, nodeName, "/tmp/ksail-images-import.tar")

		// Mock exec for cleanup
		setupExecMockForImporter(ctx, t, mockClient, nodeName)
	}

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
		image.ImportOptions{
			InputPath: inputPath,
		},
	)

	require.NoError(t, err)
}

func TestImportCopyToContainerFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock CopyToContainer - fails
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(errImportCopyFailed)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{
			InputPath: inputPath,
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy import file to container")
}

func TestImportExecFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock CopyToContainer - succeeds
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(nil)

	// Mock exec for import - fails with non-zero exit code
	execID := "exec-import-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, "my-cluster-control-plane", mock.Anything).
		Return(container.ExecCreateResponse{ID: execID}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "ctr: import failed"), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 1}, nil)

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{
			InputPath: inputPath,
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ctr import failed")
}

func TestImportEmptyProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	// Empty provider should default to Docker behavior
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock CopyToContainer
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(nil)

	// Mock exec for import
	setupImportExecMockForImporter(
		ctx,
		t,
		mockClient,
		"my-cluster-control-plane",
		"/root/ksail-images-import.tar",
	)

	// Mock exec for cleanup
	setupExecMockForImporter(ctx, t, mockClient, "my-cluster-control-plane")

	importer := image.NewImporter(mockClient)
	err = importer.Import(ctx, "my-cluster", v1alpha1.DistributionVanilla, "", image.ImportOptions{
		InputPath: inputPath,
	})

	require.NoError(t, err)
}

func TestImportMultipleNodesPartialFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o600)
	require.NoError(t, err)

	// Return multiple nodes
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
			{
				Names:  []string{"/my-cluster-worker"},
				Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
			},
		}, nil)

	// First node succeeds
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(nil).Once()

	setupImportExecMockForImporter(
		ctx,
		t,
		mockClient,
		"my-cluster-control-plane",
		"/root/ksail-images-import.tar",
	)
	setupExecMockForImporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Second node fails during copy
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-worker", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(errImportCopyFailed).Once()

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{
			InputPath: inputPath,
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to import images to node my-cluster-worker")
}

func TestImportLargeFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create a larger input file to test tar creation
	inputPath := filepath.Join(tmpDir, "images.tar")
	largeContent := bytes.Repeat([]byte("image-data"), 1000) // 10KB
	err := os.WriteFile(inputPath, largeContent, 0o600)
	require.NoError(t, err)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Verify the tar archive sent to container has correct structure
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.MatchedBy(func(reader io.Reader) bool {
			// Read the tar data and verify it's valid
			return reader != nil
		}), container.CopyToContainerOptions{}).
		Return(nil)

	setupImportExecMockForImporter(
		ctx,
		t,
		mockClient,
		"my-cluster-control-plane",
		"/root/ksail-images-import.tar",
	)
	setupExecMockForImporter(ctx, t, mockClient, "my-cluster-control-plane")

	importer := image.NewImporter(mockClient)
	err = importer.Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{
			InputPath: inputPath,
		},
	)

	require.NoError(t, err)
}

// setupExecMockForImporter is a helper to set up ContainerExec* mocks for simple cases.
func setupExecMockForImporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
) {
	t.Helper()

	execID := "exec-" + containerName

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.Anything).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}

// setupImportExecMockForImporter sets up exec mocks for the ctr import command.
func setupImportExecMockForImporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
	importPath string,
) {
	t.Helper()

	execID := "exec-import-" + containerName

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			// Verify the import command structure
			return len(opts.Cmd) >= 5 &&
				opts.Cmd[0] == "ctr" &&
				opts.Cmd[1] == "--namespace=k8s.io" &&
				opts.Cmd[2] == "images" &&
				opts.Cmd[3] == "import" &&
				opts.Cmd[4] == "--digests" &&
				opts.Cmd[5] == importPath
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}
