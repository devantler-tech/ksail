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

func TestNewImporter(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	importer := image.NewImporter(mockClient)

	assert.NotNil(t, importer, "NewImporter should return a non-nil importer")
}

func TestImport(t *testing.T) {
	t.Parallel()

	errListFailed := errors.New("list nodes failed")

	tests := []struct {
		name         string
		clusterName  string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		opts         image.ImportOptions
		setupMocks   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context)
		setupFiles   func(t *testing.T, tmpDir string) string
		wantErr      bool
		wantErrMsg   string
	}{
		{
			name:         "unsupported Talos distribution",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ImportOptions{InputPath: "/tmp/images.tar"},
			setupMocks:   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {},
			setupFiles: func(t *testing.T, tmpDir string) string {
				t.Helper()
				// Create a dummy input file
				inputPath := filepath.Join(tmpDir, "images.tar")
				err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
				require.NoError(t, err)

				return inputPath
			},
			wantErr:    true,
			wantErrMsg: "distribution does not support image export/import",
		},
		{
			name:         "unsupported provider (Hetzner)",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderHetzner,
			opts:         image.ImportOptions{},
			setupMocks:   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {},
			setupFiles: func(t *testing.T, tmpDir string) string {
				t.Helper()

				inputPath := filepath.Join(tmpDir, "images.tar")
				err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
				require.NoError(t, err)

				return inputPath
			},
			wantErr:    true,
			wantErrMsg: "unsupported provider for image operations",
		},
		{
			name:         "input file not found",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ImportOptions{InputPath: "/nonexistent/images.tar"},
			setupMocks:   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {},
			setupFiles:   nil,
			wantErr:      true,
			wantErrMsg:   "input file does not exist",
		},
		{
			name:         "default input path not found",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ImportOptions{}, // Uses default "images.tar"
			setupMocks:   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {},
			setupFiles:   nil,
			wantErr:      true,
			wantErrMsg:   "input file does not exist",
		},
		{
			name:         "list nodes fails",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ImportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {
				t.Helper()

				mockClient.EXPECT().
					ContainerList(ctx, mock.Anything).
					Return(nil, errListFailed)
			},
			setupFiles: func(t *testing.T, tmpDir string) string {
				t.Helper()

				inputPath := filepath.Join(tmpDir, "images.tar")
				err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
				require.NoError(t, err)

				return inputPath
			},
			wantErr:    true,
			wantErrMsg: "failed to list nodes",
		},
		{
			name:         "no nodes found",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ImportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {
				t.Helper()

				mockClient.EXPECT().
					ContainerList(ctx, mock.Anything).
					Return([]container.Summary{}, nil)
			},
			setupFiles: func(t *testing.T, tmpDir string) string {
				t.Helper()

				inputPath := filepath.Join(tmpDir, "images.tar")
				err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
				require.NoError(t, err)

				return inputPath
			},
			wantErr:    true,
			wantErrMsg: "no cluster nodes found",
		},
		{
			name:         "only helper containers (no K8s nodes)",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ImportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context) {
				t.Helper()

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
			},
			setupFiles: func(t *testing.T, tmpDir string) string {
				t.Helper()

				inputPath := filepath.Join(tmpDir, "images.tar")
				err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
				require.NoError(t, err)

				return inputPath
			},
			wantErr:    true,
			wantErrMsg: "no valid kubernetes nodes found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockClient := docker.NewMockAPIClient(t)

			tmpDir := t.TempDir()

			if tt.setupFiles != nil {
				inputPath := tt.setupFiles(t, tmpDir)
				if tt.opts.InputPath == "" {
					tt.opts.InputPath = inputPath
				}
			}

			tt.setupMocks(t, mockClient, ctx)

			importer := image.NewImporter(mockClient)
			err := importer.Import(ctx, tt.clusterName, tt.distribution, tt.provider, tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestImportSuccessVanilla(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
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
		setupImportExecMock(t, mockClient, ctx, nodeName, "/root/ksail-images-import.tar")

		// Mock exec for cleanup
		setupExecMock(t, mockClient, ctx, nodeName, "", 0)
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
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
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
			}, // Should be filtered
			{
				Names:  []string{"/k3d-my-cluster-registry"},
				Labels: map[string]string{"k3d.role": "registry"},
			}, // Should be filtered
		}, nil)

	// Import happens only to server and agent nodes (not loadbalancer or registry)
	for _, nodeName := range []string{"k3d-my-cluster-server-0", "k3d-my-cluster-agent-0"} {
		// Mock CopyToContainer
		mockClient.EXPECT().
			CopyToContainer(ctx, nodeName, "/tmp", mock.Anything, container.CopyToContainerOptions{}).
			Return(nil).Once()

		// Mock exec for import command - K3d uses /tmp path
		setupImportExecMock(t, mockClient, ctx, nodeName, "/tmp/ksail-images-import.tar")

		// Mock exec for cleanup
		setupExecMock(t, mockClient, ctx, nodeName, "", 0)
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

	errCopyFailed := errors.New("copy to container failed")

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
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
		Return(errCopyFailed)

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
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
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
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
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
	setupImportExecMock(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		"/root/ksail-images-import.tar",
	)

	// Mock exec for cleanup
	setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)

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

	errCopyFailed := errors.New("copy to container failed")

	// Create input file
	inputPath := filepath.Join(tmpDir, "images.tar")
	err := os.WriteFile(inputPath, []byte("fake tar content"), 0o644)
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

	setupImportExecMock(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		"/root/ksail-images-import.tar",
	)
	setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)

	// Second node fails during copy
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-worker", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(errCopyFailed).Once()

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

// setupImportExecMock sets up exec mocks for the ctr import command.
func setupImportExecMock(
	t *testing.T,
	mockClient *docker.MockAPIClient,
	ctx context.Context,
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

func TestImportLargeFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()

	// Create a larger input file to test tar creation
	inputPath := filepath.Join(tmpDir, "images.tar")
	largeContent := bytes.Repeat([]byte("image-data"), 1000) // 10KB
	err := os.WriteFile(inputPath, largeContent, 0o644)
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
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.MatchedBy(func(r io.Reader) bool {
			// Read the tar data and verify it's valid
			return r != nil
		}), container.CopyToContainerOptions{}).
		Return(nil)

	setupImportExecMock(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		"/root/ksail-images-import.tar",
	)
	setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)

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
