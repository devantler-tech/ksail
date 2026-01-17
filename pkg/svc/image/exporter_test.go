package image_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewExporter(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	exporter := image.NewExporter(mockClient)

	assert.NotNil(t, exporter, "NewExporter should return a non-nil exporter")
}

func TestExport(t *testing.T) {
	t.Parallel()

	errListFailed := errors.New("list nodes failed")

	tests := []struct {
		name         string
		clusterName  string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		opts         image.ExportOptions
		setupMocks   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context, tmpDir string)
		setupFiles   func(t *testing.T, tmpDir string)
		wantErr      bool
		wantErrMsg   string
		checkResult  func(t *testing.T, tmpDir string)
	}{
		{
			name:         "unsupported Talos distribution",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionTalos,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ExportOptions{},
			setupMocks:   func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context, tmpDir string) {},
			wantErr:      true,
			wantErrMsg:   "distribution does not support image export/import",
		},
		{
			name:         "unsupported provider (Hetzner)",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderHetzner,
			opts:         image.ExportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context, tmpDir string) {
				t.Helper()
				// No mocks needed - should fail before listing nodes
			},
			wantErr:    true,
			wantErrMsg: "unsupported provider for image operations",
		},
		{
			name:         "list nodes fails",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ExportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context, tmpDir string) {
				t.Helper()

				mockClient.EXPECT().
					ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
						return opts.Filters.Get("label") != nil
					})).
					Return(nil, errListFailed)
			},
			wantErr:    true,
			wantErrMsg: "failed to list nodes",
		},
		{
			name:         "no nodes found",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ExportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context, tmpDir string) {
				t.Helper()

				mockClient.EXPECT().
					ContainerList(ctx, mock.Anything).
					Return([]container.Summary{}, nil)
			},
			wantErr:    true,
			wantErrMsg: "no cluster nodes found",
		},
		{
			name:         "no images found in cluster",
			clusterName:  "my-cluster",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderDocker,
			opts:         image.ExportOptions{},
			setupMocks: func(t *testing.T, mockClient *docker.MockAPIClient, ctx context.Context, tmpDir string) {
				t.Helper()

				// Mock ContainerList for listing nodes
				mockClient.EXPECT().
					ContainerList(ctx, mock.Anything).
					Return([]container.Summary{
						{
							Names:  []string{"/my-cluster-control-plane"},
							Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
						},
					}, nil)

				// Mock exec for listing images - returns empty
				setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)
			},
			wantErr:    true,
			wantErrMsg: "no images found in cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockClient := docker.NewMockAPIClient(t)

			tmpDir := t.TempDir()
			if tt.opts.OutputPath == "" {
				tt.opts.OutputPath = filepath.Join(tmpDir, "images.tar")
			}

			if tt.setupFiles != nil {
				tt.setupFiles(t, tmpDir)
			}

			tt.setupMocks(t, mockClient, ctx, tmpDir)

			exporter := image.NewExporter(mockClient)
			err := exporter.Export(ctx, tt.clusterName, tt.distribution, tt.provider, tt.opts)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)

				return
			}

			require.NoError(t, err)

			if tt.checkResult != nil {
				tt.checkResult(t, tmpDir)
			}
		})
	}
}

func TestExportWithSpecificImages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	// Mock ContainerList for listing nodes
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock exec for export command
	setupExecMockWithCmd(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		[]string{
			"ctr",
			"--namespace=k8s.io",
			"images",
			"export",
			"/root/ksail-images-export.tar",
			"nginx:latest",
		},
		"",
		0,
	)

	// Mock CopyFromContainer - create a valid tar archive
	tarContent := createTarWithFile(t, "ksail-images-export.tar", []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.NoError(t, err)

	// Verify the file was created
	_, err = os.Stat(outputPath)
	require.NoError(t, err)
}

func TestExportK3sDistribution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	// Mock ContainerList for listing K3d nodes
	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			// K3d uses different labels
			return opts.Filters.Get("label") != nil
		})).
		Return([]container.Summary{
			{
				Names:  []string{"/k3d-my-cluster-server-0"},
				Labels: map[string]string{"k3d.role": "server"},
			},
		}, nil)

	// Mock exec for export - K3d uses /tmp path
	setupExecMockWithCmd(
		t,
		mockClient,
		ctx,
		"k3d-my-cluster-server-0",
		[]string{
			"ctr",
			"--namespace=k8s.io",
			"images",
			"export",
			"/tmp/ksail-images-export.tar",
			"nginx:latest",
		},
		"",
		0,
	)

	// Mock CopyFromContainer
	tarContent := createTarWithFile(t, "ksail-images-export.tar", []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "k3d-my-cluster-server-0", "/tmp/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMock(t, mockClient, ctx, "k3d-my-cluster-server-0", "", 0)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionK3s,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.NoError(t, err)
}

// setupExecMock is a helper to set up ContainerExec* mocks for simple cases.
func setupExecMock(
	t *testing.T,
	mockClient *docker.MockAPIClient,
	ctx context.Context,
	containerName string,
	stdout string,
	exitCode int,
) {
	t.Helper()

	execID := "exec-" + containerName

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.Anything).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse(stdout, ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: exitCode}, nil).Once()
}

// setupExecMockWithCmd sets up exec mocks with specific command matching.
func setupExecMockWithCmd(
	t *testing.T,
	mockClient *docker.MockAPIClient,
	ctx context.Context,
	containerName string,
	expectedCmd []string,
	stdout string,
	exitCode int,
) {
	t.Helper()

	execID := "exec-" + containerName + "-cmd"

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			if len(opts.Cmd) != len(expectedCmd) {
				return false
			}

			for i := range opts.Cmd {
				if opts.Cmd[i] != expectedCmd[i] {
					return false
				}
			}

			return true
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse(stdout, ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: exitCode}, nil).Once()
}

// createTarWithFile creates a tar archive containing a single file.
func createTarWithFile(t *testing.T, filename string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	header := &tar.Header{
		Name:     filename,
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}

	err := tw.WriteHeader(header)
	require.NoError(t, err)

	_, err = tw.Write(content)
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

func TestExportEmptyProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	// Empty provider should default to Docker behavior
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock exec for export
	setupExecMockWithCmd(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		[]string{
			"ctr",
			"--namespace=k8s.io",
			"images",
			"export",
			"/root/ksail-images-export.tar",
			"nginx:latest",
		},
		"",
		0,
	)

	// Mock CopyFromContainer
	tarContent := createTarWithFile(t, "ksail-images-export.tar", []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(ctx, "my-cluster", v1alpha1.DistributionVanilla, "", image.ExportOptions{
		OutputPath: outputPath,
		Images:     []string{"nginx:latest"},
	})

	require.NoError(t, err)
}

func TestExportCopyFromContainerFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	errCopyFailed := errors.New("copy from container failed")

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock exec for export
	setupExecMockWithCmd(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		[]string{
			"ctr",
			"--namespace=k8s.io",
			"images",
			"export",
			"/root/ksail-images-export.tar",
			"nginx:latest",
		},
		"",
		0,
	)

	// Mock CopyFromContainer - fails
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(nil, container.PathStat{}, errCopyFailed)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy export file from container")
}

func TestExportExecFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock exec for export - fails with non-zero exit code
	execID := "exec-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, "my-cluster-control-plane", mock.Anything).
		Return(container.ExecCreateResponse{ID: execID}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "ctr: export failed"), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 1}, nil)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ctr export failed")
}

func TestExportListImagesFiltersDigests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// First exec is for listing images - includes both named images and digest-only refs
	imageList := "nginx:latest\nsha256:abc123\nredis:alpine\nsha256:def456\n"
	setupExecMockWithCmd(t, mockClient, ctx, "my-cluster-control-plane",
		[]string{"ctr", "--namespace=k8s.io", "images", "list", "-q"},
		imageList, 0)

	// Second exec is for exporting - only named images
	setupExecMockWithCmd(
		t,
		mockClient,
		ctx,
		"my-cluster-control-plane",
		[]string{
			"ctr",
			"--namespace=k8s.io",
			"images",
			"export",
			"/root/ksail-images-export.tar",
			"nginx:latest",
			"redis:alpine",
		},
		"",
		0,
	)

	// Mock CopyFromContainer
	tarContent := createTarWithFile(t, "ksail-images-export.tar", []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMock(t, mockClient, ctx, "my-cluster-control-plane", "", 0)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
		},
	)

	require.NoError(t, err)
}

func TestExportNodeSelectionPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		distribution     v1alpha1.Distribution
		nodes            []container.Summary
		expectedNodeName string
	}{
		{
			name:         "prefers control-plane over worker",
			distribution: v1alpha1.DistributionVanilla,
			nodes: []container.Summary{
				{
					Names:  []string{"/my-cluster-worker"},
					Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
				},
				{
					Names:  []string{"/my-cluster-control-plane"},
					Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
				},
			},
			expectedNodeName: "my-cluster-control-plane",
		},
		{
			name:         "K3d prefers server over agent",
			distribution: v1alpha1.DistributionK3s,
			nodes: []container.Summary{
				{
					Names:  []string{"/k3d-my-cluster-agent-0"},
					Labels: map[string]string{"k3d.role": "agent"},
				},
				{
					Names:  []string{"/k3d-my-cluster-server-0"},
					Labels: map[string]string{"k3d.role": "server"},
				},
			},
			expectedNodeName: "k3d-my-cluster-server-0",
		},
		{
			name:         "excludes loadbalancer",
			distribution: v1alpha1.DistributionK3s,
			nodes: []container.Summary{
				{
					Names:  []string{"/k3d-my-cluster-serverlb"},
					Labels: map[string]string{"k3d.role": "loadbalancer"},
				},
				{
					Names:  []string{"/k3d-my-cluster-server-0"},
					Labels: map[string]string{"k3d.role": "server"},
				},
			},
			expectedNodeName: "k3d-my-cluster-server-0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockClient := docker.NewMockAPIClient(t)
			tmpDir := t.TempDir()
			outputPath := filepath.Join(tmpDir, "images.tar")

			mockClient.EXPECT().
				ContainerList(ctx, mock.Anything).
				Return(tt.nodes, nil)

			// The expected node should be selected - use setupExecMock which accepts any command
			setupExecMock(t, mockClient, ctx, tt.expectedNodeName, "", 0)

			// Mock CopyFromContainer
			tarContent := createTarWithFile(t, "ksail-images-export.tar", []byte("fake image data"))
			mockClient.EXPECT().
				CopyFromContainer(ctx, tt.expectedNodeName, mock.AnythingOfType("string")).
				Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

			// Mock exec for cleanup
			setupExecMock(t, mockClient, ctx, tt.expectedNodeName, "", 0)

			exporter := image.NewExporter(mockClient)
			err := exporter.Export(
				ctx,
				"my-cluster",
				tt.distribution,
				v1alpha1.ProviderDocker,
				image.ExportOptions{
					OutputPath: outputPath,
					Images:     []string{"nginx:latest"},
				},
			)

			require.NoError(t, err)
		})
	}
}

// Helper to match container list filters.
func matchContainerListFilters(labelKey, labelValue string) func(container.ListOptions) bool {
	return func(opts container.ListOptions) bool {
		f := opts.Filters

		// Get the filter values for the label key
		for _, v := range f.Get("label") {
			if strings.Contains(v, labelKey) && strings.Contains(v, labelValue) {
				return true
			}
		}

		return len(f.Get("label")) > 0
	}
}

// Reuse filter helper across tests.
var _ = filters.Args{}
