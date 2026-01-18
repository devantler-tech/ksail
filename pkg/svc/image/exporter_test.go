package image_test

import (
	"archive/tar"
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
	errExportListFailed = errors.New("list nodes failed")
	errExportCopyFailed = errors.New("copy from container failed")
)

func TestNewExporter(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	exporter := image.NewExporter(mockClient)

	assert.NotNil(t, exporter, "NewExporter should return a non-nil exporter")
}

func TestExportUnsupportedDistribution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "talos does not support image export/import")
}

func TestExportUnsupportedProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderHetzner,
		image.ExportOptions{OutputPath: outputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider for image operations")
}

func TestExportListNodesFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	mockClient.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.Filters.Get("label") != nil
		})).
		Return(nil, errExportListFailed)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list nodes")
}

func TestExportNoNodesFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cluster nodes found")
}

func TestExportNoImagesFound(t *testing.T) {
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

	// Mock exec for listing images - returns empty
	setupExecMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no images found in cluster")
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

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock exec for export command
	exportCmd := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export",
		"--platform", "linux/amd64",
		"/root/ksail-images-export.tar", "nginx:latest",
	}
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, "my-cluster-control-plane", exportCmd,
	)

	// Mock CopyFromContainer - create a valid tar archive
	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "k3d-my-cluster-server-0")

	// Mock exec for export - K3d uses /tmp path
	k3dExportCmd := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export",
		"--platform", "linux/amd64",
		"/tmp/ksail-images-export.tar", "nginx:latest",
	}
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, "k3d-my-cluster-server-0", k3dExportCmd,
	)

	// Mock CopyFromContainer
	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "k3d-my-cluster-server-0", "/tmp/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMockForExporter(ctx, t, mockClient, "k3d-my-cluster-server-0")

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

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock exec for export
	setupExecMockWithCmdForExporter(
		ctx,
		t,
		mockClient,
		"my-cluster-control-plane",
		[]string{
			ctrCommand,
			"--namespace=k8s.io",
			"images",
			"export",
			"--platform",
			"linux/amd64",
			"/root/ksail-images-export.tar",
			"nginx:latest",
		},
	)

	// Mock CopyFromContainer
	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock exec for export
	setupExecMockWithCmdForExporter(
		ctx,
		t,
		mockClient,
		"my-cluster-control-plane",
		[]string{
			ctrCommand,
			"--namespace=k8s.io",
			"images",
			"export",
			"--platform",
			"linux/amd64",
			"/root/ksail-images-export.tar",
			"nginx:latest",
		},
	)

	// Mock CopyFromContainer - fails
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(nil, container.PathStat{}, errExportCopyFailed)

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

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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

func TestExportFallbackReportsFailedImages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "images.tar")

	// Capture stderr output
	oldStderr := os.Stderr
	stderrReader, stderrWriter, _ := os.Pipe()
	os.Stderr = stderrWriter

	defer func() {
		os.Stderr = oldStderr
	}()

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")
	setupFallbackExportMocks(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock CopyFromContainer
	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Final cleanup
	setupExecMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	exporter := image.NewExporter(mockClient)
	exportErr := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest", "redis:alpine"},
		},
	)

	// Close write end and read stderr
	closeErr := stderrWriter.Close()
	require.NoError(t, closeErr)

	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, stderrReader)

	require.NoError(t, exportErr)

	// Verify stderr contains warning about failed image
	stderrOutput := stderrBuf.String()
	assert.Contains(t, stderrOutput, "warning: failed to export 1 image(s)")
	assert.Contains(t, stderrOutput, "redis:alpine")
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
	setupExecMockWithStdoutForExporter(ctx, t, mockClient, "my-cluster-control-plane",
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"}, imageList)

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Second exec is for exporting - only named images
	exportCmd := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export",
		"--platform", "linux/amd64",
		"/root/ksail-images-export.tar", "nginx:latest", "redis:alpine",
	}
	setupExecMockWithCmdForExporter(ctx, t, mockClient, "my-cluster-control-plane", exportCmd)

	// Mock CopyFromContainer
	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, "my-cluster-control-plane", "/root/ksail-images-export.tar").
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	// Mock exec for cleanup
	setupExecMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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

func TestExportPrefersControlPlaneOverWorker(t *testing.T) {
	t.Parallel()

	nodes := []container.Summary{
		{
			Names:  []string{"/my-cluster-worker"},
			Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
		},
		{
			Names:  []string{"/my-cluster-control-plane"},
			Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
		},
	}
	runNodeSelectionTest(t, v1alpha1.DistributionVanilla, nodes, "my-cluster-control-plane")
}

func TestExportK3dPrefersServerOverAgent(t *testing.T) {
	t.Parallel()

	nodes := []container.Summary{
		{
			Names:  []string{"/k3d-my-cluster-agent-0"},
			Labels: map[string]string{"k3d.role": "agent"},
		},
		{
			Names:  []string{"/k3d-my-cluster-server-0"},
			Labels: map[string]string{"k3d.role": "server"},
		},
	}
	runNodeSelectionTest(t, v1alpha1.DistributionK3s, nodes, "k3d-my-cluster-server-0")
}

func TestExportExcludesLoadbalancer(t *testing.T) {
	t.Parallel()

	nodes := []container.Summary{
		{
			Names:  []string{"/k3d-my-cluster-serverlb"},
			Labels: map[string]string{"k3d.role": "loadbalancer"},
		},
		{
			Names:  []string{"/k3d-my-cluster-server-0"},
			Labels: map[string]string{"k3d.role": "server"},
		},
	}
	runNodeSelectionTest(t, v1alpha1.DistributionK3s, nodes, "k3d-my-cluster-server-0")
}

func runNodeSelectionTest(
	t *testing.T,
	distribution v1alpha1.Distribution,
	nodes []container.Summary,
	expectedNodeName string,
) {
	t.Helper()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	outputPath := filepath.Join(t.TempDir(), "images.tar")

	mockClient.EXPECT().ContainerList(ctx, mock.Anything).Return(nodes, nil)

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, expectedNodeName)

	// Mock export command
	setupExecMockForExporter(ctx, t, mockClient, expectedNodeName)

	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, expectedNodeName, mock.AnythingOfType("string")).
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	setupExecMockForExporter(ctx, t, mockClient, expectedNodeName)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(ctx, "my-cluster", distribution, v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath, Images: []string{"nginx:latest"}})

	require.NoError(t, err)
}

// setupPlatformDetectMockForExporter sets up mock for uname -m platform detection.
func setupPlatformDetectMockForExporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
) {
	t.Helper()

	setupExecMockWithStdoutForExporter(ctx, t, mockClient, containerName,
		[]string{"uname", "-m"}, "x86_64\n")
}

// setupExecMockForExporter is a helper to set up ContainerExec* mocks for simple cases.
func setupExecMockForExporter(
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

// setupExecMockWithCmdForExporter sets up exec mocks with specific command matching.
func setupExecMockWithCmdForExporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
	expectedCmd []string,
) {
	t.Helper()

	execID := "exec-" + containerName + "-cmd"

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			if len(opts.Cmd) != len(expectedCmd) {
				return false
			}

			for index := range opts.Cmd {
				if opts.Cmd[index] != expectedCmd[index] {
					return false
				}
			}

			return true
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}

// setupExecMockWithStdoutForExporter sets up exec mocks with stdout output.
func setupExecMockWithStdoutForExporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
	expectedCmd []string,
	stdout string,
) {
	t.Helper()

	execID := "exec-" + containerName + "-stdout"

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			if len(opts.Cmd) != len(expectedCmd) {
				return false
			}

			for index := range opts.Cmd {
				if opts.Cmd[index] != expectedCmd[index] {
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
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}

// createExportTar creates a tar archive containing the export file.
func createExportTar(t *testing.T, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	header := &tar.Header{
		Name:     "ksail-images-export.tar",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}

	err := tarWriter.WriteHeader(header)
	require.NoError(t, err)

	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	err = tarWriter.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

// setupFallbackExportMocks sets up mocks for testing the fallback export mechanism.
func setupFallbackExportMocks(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	nodeName string,
) {
	t.Helper()

	// First exec: bulk export fails (triggers fallback)
	execID1 := "exec-bulk-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return len(opts.Cmd) > 5 && opts.Cmd[0] == ctrCommand && opts.Cmd[3] == "export"
		})).
		Return(container.ExecCreateResponse{ID: execID1}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID1, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "bulk export failed"), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID1).
		Return(container.ExecInspect{ExitCode: 1}, nil).Once()

	// Second exec: first image succeeds
	execID2 := "exec-image1-success"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return len(opts.Cmd) == 8 && opts.Cmd[len(opts.Cmd)-1] == "nginx:latest"
		})).
		Return(container.ExecCreateResponse{ID: execID2}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID2, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID2).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()

	// Third exec: second image fails
	execID3 := "exec-image2-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return len(opts.Cmd) == 8 && opts.Cmd[len(opts.Cmd)-1] == "redis:alpine"
		})).
		Return(container.ExecCreateResponse{ID: execID3}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID3, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "export failed"), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID3).
		Return(container.ExecInspect{ExitCode: 1}, nil).Once()

	// Fourth exec: cleanup after individual tests
	setupExecMockForExporter(ctx, t, mockClient, nodeName)

	// Fifth exec: re-export only successful image
	execID5 := "exec-reexport"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return len(opts.Cmd) == 8 && opts.Cmd[len(opts.Cmd)-1] == "nginx:latest"
		})).
		Return(container.ExecCreateResponse{ID: execID5}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID5, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID5).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}
