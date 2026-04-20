package image_test

import (
	"archive/tar"
	"bytes"
	"context"
	sha256Lib "crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
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

const (
	kindExporterNodeName = "my-cluster-control-plane"
	kindExporterTarPath  = "/root/ksail-images-export.tar"
)

type specificImageExportCase struct {
	localImageList      string
	requestedImages     []string
	expectedExportImage []string
}

func exportRequestedImages(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	outputPath string,
	requestedImages []string,
) {
	t.Helper()

	exporter := image.NewExporter(mockClient)

	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     requestedImages,
		},
	)

	require.NoError(t, err)
}

func setupWhoamiExplicitExportTest(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
) {
	t.Helper()

	setupKindNodeListMock(ctx, mockClient)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)
}

func expectRepairPullSuccess(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	imageRef string,
) {
	t.Helper()

	// rm is called before pull to force fresh download (discard_unpacked_layers fix)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildCtrRmCommand(imageRef),
	)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildCtrPullCommand("linux/amd64", imageRef),
	)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildCtrContentFetchCommand("linux/amd64", imageRef),
	)
}

func expectRepairPullSuccessWithRmFailure(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	imageRef string,
) {
	t.Helper()

	// rm fails (e.g. image ref does not exist yet) but is best-effort
	setupKindExecFailWithCmdForExporter(
		ctx, t, mockClient,
		buildCtrRmCommand(imageRef),
		"ctr: image not found",
	)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildCtrPullCommand("linux/amd64", imageRef),
	)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildCtrContentFetchCommand("linux/amd64", imageRef),
	)
}

func expectRepairPullFailure(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	imageRef string,
	stderr string,
) {
	t.Helper()

	// rm is called before pull to force fresh download (discard_unpacked_layers fix)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildCtrRmCommand(imageRef),
	)
	setupKindExecFailWithCmdForExporter(
		ctx, t, mockClient,
		buildCtrPullCommand("linux/amd64", imageRef),
		stderr,
	)
}

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

	setupEmptyImageListMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock exec for export command
	exportCmd := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export",
		"--platform", "linux/amd64",
		"/root/ksail-images-export.tar", "docker.io/library/nginx:latest",
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

func TestExportWithSpecificImagesResolvesLocalDigestRef(t *testing.T) {
	t.Parallel()

	runResolvedSpecificImageExportTest(t, specificImageExportCase{
		localImageList:      "docker.io/traefik/whoami:v1.10@sha256:abc123\n",
		requestedImages:     []string{"traefik/whoami:v1.10"},
		expectedExportImage: []string{"docker.io/traefik/whoami:v1.10@sha256:abc123"},
	})
}

func TestExportWithDigestPinnedImageResolvesMatchingLocalRef(t *testing.T) {
	t.Parallel()

	runResolvedSpecificImageExportTest(t, specificImageExportCase{
		localImageList:      "docker.io/traefik/whoami:v1.10@sha256:abc123\n",
		requestedImages:     []string{"docker.io/traefik/whoami@sha256:abc123"},
		expectedExportImage: []string{"docker.io/traefik/whoami:v1.10@sha256:abc123"},
	})
}

func TestExportWithDigestPinnedImageDoesNotMatchDifferentRepository(t *testing.T) {
	t.Parallel()

	runResolvedSpecificImageExportTest(t, specificImageExportCase{
		localImageList:      "docker.io/library/busybox:latest@sha256:abc123\n",
		requestedImages:     []string{"docker.io/traefik/whoami@sha256:abc123"},
		expectedExportImage: []string{"docker.io/traefik/whoami@sha256:abc123"},
	})
}

func TestExportWithSpecificImagesFallsBackWhenLocalImageListFails(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupKindExecFailWithCmdForExporter(
		ctx, t, mockClient,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"ctr: list failed",
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10"),
	)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportRequestedImages(
		ctx,
		t,
		mockClient,
		outputPath,
		[]string{"traefik/whoami:v1.10"},
	)
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

	setupEmptyImageListMockForExporter(ctx, t, mockClient, "k3d-my-cluster-server-0")

	// Mock platform detection (uname -m)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, "k3d-my-cluster-server-0")

	// Mock exec for export - K3d uses /tmp path
	k3dExportCmd := []string{
		ctrCommand, "--namespace=k8s.io", "images", "export",
		"--platform", "linux/amd64",
		"/tmp/ksail-images-export.tar", "docker.io/library/nginx:latest",
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

	setupEmptyImageListMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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
			"docker.io/library/nginx:latest",
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

	setupEmptyImageListMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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
			"docker.io/library/nginx:latest",
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

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupEmptyImageListMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand("docker.io/library/nginx:latest")
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, "ctr: export failed")
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, "ctr: export failed")
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

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

func TestExportMissingContentRetriesPullForExplicitImages(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10@sha256:abc123")
	setupKindExecFailWithCmdForExporter(
		ctx, t, mockClient, exportCmd,
		"ctr: failed to get reader: content digest sha256:missing: not found",
	)
	expectRepairPullFailure(
		ctx, t, mockClient, "docker.io/traefik/whoami:v1.10@sha256:abc123", "ctr: pull failed",
	)
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10")
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupExecMockWithCmdForExporter(
		ctx,
		t,
		mockClient,
		kindExporterNodeName,
		buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10"),
	)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportRequestedImages(
		ctx,
		t,
		mockClient,
		outputPath,
		[]string{"traefik/whoami:v1.10"},
	)
}

func TestExportRepairPullSucceedsWhenRmFails(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10@sha256:abc123")
	setupKindExecFailWithCmdForExporter(
		ctx, t, mockClient, exportCmd,
		"ctr: failed to get reader: content digest sha256:missing: not found",
	)
	// rm returns an error ("not found") but repair pull still succeeds
	expectRepairPullSuccessWithRmFailure(
		ctx,
		t,
		mockClient,
		"docker.io/traefik/whoami:v1.10@sha256:abc123",
	)
	expectRepairPullSuccessWithRmFailure(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10")
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupExecMockWithCmdForExporter(
		ctx,
		t,
		mockClient,
		kindExporterNodeName,
		buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10"),
	)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportRequestedImages(
		ctx,
		t,
		mockClient,
		outputPath,
		[]string{"traefik/whoami:v1.10"},
	)
}

func TestExportMissingContentUsesFallbackRepairCandidateAfterDigestPullSucceeds(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupWhoamiExplicitExportTest(ctx, t, mockClient)

	exportCmd := buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10@sha256:abc123")
	setupKindExecFailWithCmdForExporter(
		ctx,
		t,
		mockClient,
		exportCmd,
		"ctr: failed to get reader: content digest sha256:missing: not found",
	)
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10@sha256:abc123")
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10")
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupExecMockWithCmdForExporter(
		ctx,
		t,
		mockClient,
		kindExporterNodeName,
		buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10"),
	)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportRequestedImages(
		ctx,
		t,
		mockClient,
		outputPath,
		[]string{"traefik/whoami:v1.10"},
	)
}

func TestExportMissingContentReresolvesExplicitImagesAfterRepairPull(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupWhoamiExplicitExportTest(ctx, t, mockClient)

	exportCmd := buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10@sha256:abc123")
	setupKindExecFailWithCmdForExporter(
		ctx,
		t,
		mockClient,
		exportCmd,
		"ctr: failed to get reader: content digest sha256:missing: not found",
	)
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10@sha256:abc123")
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10")
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n"+
			"docker.io/traefik/whoami:v1.10\n",
	)
	setupExecMockWithCmdForExporter(
		ctx,
		t,
		mockClient,
		kindExporterNodeName,
		buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10"),
	)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportRequestedImages(
		ctx,
		t,
		mockClient,
		outputPath,
		[]string{"traefik/whoami:v1.10"},
	)
}

func TestExportFallbackRepairsSingleImageAfterBulkRetryFails(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupFallbackRepairRetryMocks(ctx, t, mockClient)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"traefik/whoami:v1.10"},
		},
	)

	require.NoError(t, err)
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

	setupEmptyImageListMockForExporter(ctx, t, mockClient, "my-cluster-control-plane")

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
	assert.Contains(t, stderrOutput, "docker.io/library/redis:alpine")
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

func newExporterTestContext(t *testing.T) (context.Context, *docker.MockAPIClient, string) {
	t.Helper()

	return context.Background(),
		docker.NewMockAPIClient(t),
		filepath.Join(t.TempDir(), "images.tar")
}

func setupKindNodeListMock(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/" + kindExporterNodeName},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)
}

func buildKindCtrExportCommand(images ...string) []string {
	cmd := make([]string, 0, 7+len(images))
	cmd = append(cmd,
		ctrCommand,
		"--namespace=k8s.io",
		"images",
		"export",
		"--platform",
		"linux/amd64",
		kindExporterTarPath,
	)

	return append(cmd, images...)
}

func buildCtrRmCommand(imageRef string) []string {
	return []string{
		ctrCommand,
		"--namespace=k8s.io",
		"images",
		"rm",
		imageRef,
	}
}

func buildCtrPullCommand(platform string, imageRef string) []string {
	return []string{
		ctrCommand,
		"--namespace=k8s.io",
		"images",
		"pull",
		"--platform",
		platform,
		imageRef,
	}
}

func buildCtrContentFetchCommand(platform string, imageRef string) []string {
	return []string{
		ctrCommand,
		"--namespace=k8s.io",
		"content",
		"fetch",
		"--platform",
		platform,
		imageRef,
	}
}

func expectCopiedExportTar(ctx context.Context, t *testing.T, mockClient *docker.MockAPIClient) {
	t.Helper()

	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, kindExporterNodeName, kindExporterTarPath).
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)
}

func runResolvedSpecificImageExportTest(t *testing.T, testCase specificImageExportCase) {
	t.Helper()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		testCase.localImageList,
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupExecMockWithCmdForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		buildKindCtrExportCommand(testCase.expectedExportImage...),
	)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exporter := image.NewExporter(mockClient)
	err := exporter.Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     testCase.requestedImages,
		},
	)

	require.NoError(t, err)
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

	setupEmptyImageListMockForExporter(ctx, t, mockClient, expectedNodeName)

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

func setupEmptyImageListMockForExporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
) {
	t.Helper()

	setupExecMockWithStdoutForExporter(ctx, t, mockClient, containerName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"}, "")
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

func setupKindExecFailWithCmdForExporter(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	expectedCmd []string,
	stderr string,
) {
	t.Helper()

	execID := "exec-" + kindExporterNodeName + "-cmd-fail"

	mockClient.EXPECT().
		ContainerExecCreate(ctx, kindExporterNodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
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
		Return(mockDockerStreamResponse("", stderr), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 1}, nil).Once()
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

	setupBulkExportFailMock(ctx, t, mockClient, nodeName)
	setupIndividualImageExportMocks(ctx, t, mockClient, nodeName)
	setupExecMockForExporter(ctx, t, mockClient, nodeName)
	setupReexportSuccessfulImageMock(ctx, t, mockClient, nodeName)
}

func setupFallbackRepairRetryMocks(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
) {
	t.Helper()

	setupKindNodeListMock(ctx, mockClient)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10@sha256:abc123")
	missingContentErr := "ctr: failed to get reader: content digest sha256:missing: not found"

	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, missingContentErr)
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10@sha256:abc123")
	expectRepairPullFailure(
		ctx, t, mockClient, "docker.io/traefik/whoami:v1.10", "ctr: pull failed",
	)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
	)
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, missingContentErr)
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, missingContentErr)
	expectRepairPullFailure(
		ctx, t, mockClient, "docker.io/traefik/whoami:v1.10@sha256:abc123", "ctr: pull failed",
	)
	expectRepairPullSuccess(ctx, t, mockClient, "docker.io/traefik/whoami:v1.10")

	tagExportCmd := buildKindCtrExportCommand("docker.io/traefik/whoami:v1.10")
	setupExecMockWithCmdForExporter(ctx, t, mockClient, kindExporterNodeName, tagExportCmd)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupExecMockWithCmdForExporter(ctx, t, mockClient, kindExporterNodeName, tagExportCmd)
	expectCopiedExportTar(ctx, t, mockClient)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)
}

func matchesSingleImageExportCommand(opts container.ExecOptions, imageRef string) bool {
	return len(opts.Cmd) == 8 && opts.Cmd[len(opts.Cmd)-1] == imageRef
}

// setupBulkExportFailMock sets up the mock for the initial bulk export that fails.
func setupBulkExportFailMock(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	nodeName string,
) {
	t.Helper()

	execID := "exec-bulk-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return len(opts.Cmd) > 5 && opts.Cmd[0] == ctrCommand && opts.Cmd[3] == "export"
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "bulk export failed"), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 1}, nil).Once()
}

// setupIndividualImageExportMocks sets up mocks for individual image export attempts.
func setupIndividualImageExportMocks(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	nodeName string,
) {
	t.Helper()

	// First image succeeds
	execID2 := "exec-image1-success"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return matchesSingleImageExportCommand(opts, "docker.io/library/nginx:latest")
		})).
		Return(container.ExecCreateResponse{ID: execID2}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID2, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID2).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()

	// Second image fails
	execID3 := "exec-image2-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return matchesSingleImageExportCommand(opts, "docker.io/library/redis:alpine")
		})).
		Return(container.ExecCreateResponse{ID: execID3}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID3, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "export failed"), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID3).
		Return(container.ExecInspect{ExitCode: 1}, nil).Once()
}

// setupReexportSuccessfulImageMock sets up the mock for re-exporting only successful images.
func setupReexportSuccessfulImageMock(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	nodeName string,
) {
	t.Helper()

	execID := "exec-reexport"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, nodeName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return matchesSingleImageExportCommand(opts, "docker.io/library/nginx:latest")
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}

// --- ValidateExportedTar tests ---

// createOCITar builds a tar archive with OCI-layout blob entries.
// Each entry is placed at the given path with the given content.
func createOCITar(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	for name, content := range entries {
		header := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}

		err := tarWriter.WriteHeader(header)
		require.NoError(t, err)

		_, err = tarWriter.Write(content)
		require.NoError(t, err)
	}

	err := tarWriter.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

// blobPath returns the OCI tar path for a SHA256 blob with the given hex digest.
func blobPath(hexDigest string) string {
	return "blobs/sha256/" + hexDigest
}

// sha256Hex computes the SHA256 hex digest of content.
func sha256Hex(content []byte) string {
	h := sha256Lib.Sum256(content)

	return hex.EncodeToString(h[:])
}

func TestValidateExportedTarValidBlobs(t *testing.T) {
	t.Parallel()

	content1 := []byte("hello world blob content")
	content2 := []byte("another blob with different data")
	digest1 := sha256Hex(content1)
	digest2 := sha256Hex(content2)

	tarData := createOCITar(t, map[string][]byte{
		"oci-layout":      []byte(`{"imageLayoutVersion":"1.0.0"}`),
		blobPath(digest1): content1,
		blobPath(digest2): content2,
		"index.json":      []byte("{}"),
	})

	tarPath := filepath.Join(t.TempDir(), "valid.tar")
	err := os.WriteFile(tarPath, tarData, 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	assert.NoError(t, err)
}

func TestValidateExportedTarTruncatedBlob(t *testing.T) {
	t.Parallel()

	fullContent := []byte("this is the full blob content that should be here")
	correctDigest := sha256Hex(fullContent)

	// Write truncated content but use the digest of the full content as the filename
	truncated := fullContent[:len(fullContent)-10]

	tarData := createOCITar(t, map[string][]byte{
		blobPath(correctDigest): truncated,
	})

	tarPath := filepath.Join(t.TempDir(), "truncated.tar")
	err := os.WriteFile(tarPath, tarData, 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	require.Error(t, err)
	require.ErrorIs(t, err, image.ErrBlobIntegrityFailed)
	assert.Contains(t, err.Error(), correctDigest)
}

func TestValidateExportedTarNotATar(t *testing.T) {
	t.Parallel()

	tarPath := filepath.Join(t.TempDir(), "not-a-tar.tar")
	err := os.WriteFile(tarPath, []byte("this is not a tar file"), 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	assert.NoError(t, err, "non-tar files should be skipped gracefully")
}

func TestValidateExportedTarEmptyTar(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	err := tarWriter.Close()
	require.NoError(t, err)

	tarPath := filepath.Join(t.TempDir(), "empty.tar")
	err = os.WriteFile(tarPath, buf.Bytes(), 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	assert.NoError(t, err, "empty tar should pass validation")
}

func TestValidateExportedTarNoBlobs(t *testing.T) {
	t.Parallel()

	tarData := createOCITar(t, map[string][]byte{
		"oci-layout": []byte(`{"imageLayoutVersion":"1.0.0"}`),
		"index.json": []byte("{}"),
	})

	tarPath := filepath.Join(t.TempDir(), "no-blobs.tar")
	err := os.WriteFile(tarPath, tarData, 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	assert.NoError(t, err, "tar with no blobs should pass validation")
}

func TestValidateExportedTarFileNotFound(t *testing.T) {
	t.Parallel()

	err := image.ValidateExportedTar(filepath.Join(t.TempDir(), "subdir", "nonexistent.tar"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open exported tar for validation")
}

func TestValidateExportedTarShortRead(t *testing.T) {
	t.Parallel()

	fullContent := []byte("this is the complete blob data, all of it")
	correctDigest := sha256Hex(fullContent)

	// Build a tar where the header declares len(fullContent) bytes but only
	// half are written, simulating ctr export truncation in the content store.
	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	header := &tar.Header{
		Name:     blobPath(correctDigest),
		Mode:     0o644,
		Size:     int64(len(fullContent)),
		Typeflag: tar.TypeReg,
	}

	err := tarWriter.WriteHeader(header)
	require.NoError(t, err)

	_, err = tarWriter.Write(fullContent[:len(fullContent)/2])
	require.NoError(t, err)

	// Intentionally do not close the writer — the archive is incomplete.
	tarPath := filepath.Join(t.TempDir(), "short-read.tar")
	err = os.WriteFile(tarPath, buf.Bytes(), 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	require.Error(t, err)
	require.ErrorIs(t, err, image.ErrBlobIntegrityFailed)
}

func TestValidateExportedTarMidStreamCorruption(t *testing.T) {
	t.Parallel()

	content := []byte("blob content to validate")
	digest := sha256Hex(content)

	tarData := createOCITar(t, map[string][]byte{
		"oci-layout":     []byte(`{"imageLayoutVersion":"1.0.0"}`),
		blobPath(digest): content,
	})

	// Each tar entry occupies 512 bytes (header) + ceil(len/512)*512 bytes (data).
	// Small content fits in one 512-byte data block, so each entry is 1024 bytes.
	// Truncating at 1024+100 cuts the second entry's header in half, producing a
	// mid-stream parse error after at least one entry has been successfully read.
	truncateAt := 1024 + 100
	if truncateAt > len(tarData) {
		truncateAt = len(tarData) / 2
	}

	tarPath := filepath.Join(t.TempDir(), "mid-stream.tar")
	err := os.WriteFile(tarPath, tarData[:truncateAt], 0o600)
	require.NoError(t, err)

	err = image.ValidateExportedTar(tarPath)
	require.Error(t, err)
	require.ErrorIs(t, err, image.ErrBlobIntegrityFailed)
}
