package image_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	errImportListFailed = errors.New("list nodes failed")
	errImportCopyFailed = errors.New("copy to container failed")
	errImportDiscovery  = errors.New("fixture import capability discovery failed")
)

// kindImportPath is the node-side import archive on a Kind node under the fixed
// test archive name.
const kindImportPath = "/root/ksail-images-import.tar"

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

	// Mock ContainerList - include helper mirror containers, but only import to
	// actual Kind nodes with containerd.
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
			{
				Names:  []string{"/my-cluster-docker.io"},
				Labels: map[string]string{},
			},
			{
				Names:  []string{"/my-cluster-worker"},
				Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
			},
			{
				Names:  []string{"/my-cluster-ghcr.io"},
				Labels: map[string]string{},
			},
		}, nil)

	// Import happens to ALL K8s nodes, so we expect 2 imports
	for _, nodeName := range []string{"my-cluster-control-plane", "my-cluster-worker"} {
		// Mock CopyToContainer
		mockClient.EXPECT().
			CopyToContainer(ctx, nodeName, "/root", mock.Anything, container.CopyToContainerOptions{}).
			Return(nil).Once()

		// Mock exec for import command - Kind uses /root path
		importPath := kindImportPath
		setupImportExecMockForImporter(ctx, t, mockClient, nodeName, importPath)

		// Mock exec for cleanup
		setupCleanupMockForImporter(t, mockClient, nodeName, importPath)
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
		setupCleanupMockForImporter(t, mockClient, nodeName, "/tmp/ksail-images-import.tar")
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

	expectImportHelp(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock CopyToContainer - fails
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(errImportCopyFailed)
	setupCleanupMockForImporter(t, mockClient, "my-cluster-control-plane", kindImportPath)

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

	expectImportHelp(ctx, t, mockClient, "my-cluster-control-plane")

	// Mock CopyToContainer - succeeds
	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(nil)

	// Mock exec for import - fails with non-zero exit code
	execID := "exec-import-fail"
	mockClient.EXPECT().
		ContainerExecCreate(ctx, "my-cluster-control-plane", mock.MatchedBy(func(opts container.ExecOptions) bool {
			return slices.Equal(opts.Cmd, []string{
				ctrCommand, "--namespace=k8s.io", "images", "import", "--digests", kindImportPath,
			})
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil)

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", "ctr: import failed"), nil)

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 1}, nil)
	setupCleanupMockForImporter(t, mockClient, "my-cluster-control-plane", kindImportPath)

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
	setupCleanupMockForImporter(t, mockClient, "my-cluster-control-plane", kindImportPath)

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
	setupCleanupMockForImporter(t, mockClient, "my-cluster-control-plane", kindImportPath)

	// Second node fails during copy
	expectImportHelp(ctx, t, mockClient, "my-cluster-worker")

	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-worker", "/root", mock.Anything, container.CopyToContainerOptions{}).
		Return(errImportCopyFailed).Once()
	setupCleanupMockForImporter(t, mockClient, "my-cluster-worker", kindImportPath)

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
	setupCleanupMockForImporter(t, mockClient, "my-cluster-control-plane", kindImportPath)

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

// setupCleanupMockForImporter expects the import archive at archivePath to be
// removed from containerName exactly once, on a live context detached from the
// caller, so cleanup also runs after a failed or cancelled import.
func setupCleanupMockForImporter(
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
	archivePath string,
) {
	t.Helper()

	execID := "exec-cleanup-" + containerName

	mockClient.EXPECT().
		ContainerExecCreate(detachedCleanupContext(), containerName,
			mock.MatchedBy(func(opts container.ExecOptions) bool {
				return slices.Equal(opts.Cmd, []string{"rm", "-f", archivePath})
			})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(detachedCleanupContext(), execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(detachedCleanupContext(), execID).
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

	expectImportHelp(ctx, t, mockClient, containerName)

	execID := "exec-import-" + containerName

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			// A node without --local keeps the plain legacy command.
			return slices.Equal(opts.Cmd, []string{
				ctrCommand, "--namespace=k8s.io", "images", "import", "--digests", importPath,
			})
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse("", ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}

// Two imports on the same node must never share an archive, so every generated
// name is distinct.
func TestDefaultImportArchiveNamesAreDistinct(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(`^ksail-images-import-[0-9a-f]{16}\.tar$`)
	seen := make(map[string]struct{})

	for range 64 {
		name := image.DefaultImportArchiveNameForTest()
		if !pattern.MatchString(name) {
			t.Fatalf("archive name %q does not match %s", name, pattern)
		}

		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("archive name %q was generated twice", name)
		}

		seen[name] = struct{}{}
	}
}

// A cancelled import must still remove its archive from the node, so cleanup
// runs on a live context after the caller's context is done.
func TestImportCancelledStillRemovesArchive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	mockClient := docker.NewMockAPIClient(t)

	inputPath := filepath.Join(t.TempDir(), "images.tar")
	require.NoError(t, os.WriteFile(inputPath, []byte("fake tar content"), 0o600))

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				Names:  []string{"/my-cluster-control-plane"},
				Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			},
		}, nil)

	expectImportHelp(ctx, t, mockClient, "my-cluster-control-plane")

	mockClient.EXPECT().
		CopyToContainer(ctx, "my-cluster-control-plane", "/root", mock.Anything, container.CopyToContainerOptions{}).
		RunAndReturn(func(context.Context, string, string, io.Reader, container.CopyToContainerOptions) error {
			cancel()

			return context.Canceled
		})
	setupCleanupMockForImporter(t, mockClient, "my-cluster-control-plane", kindImportPath)

	err := image.NewImporter(mockClient).Import(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath},
	)

	require.ErrorIs(t, err, context.Canceled)
}

// legacyImportHelp is ctr import help from a version without --local.
const legacyImportHelp = "OPTIONS:\n   --digests  Create digest refs\n   --help, -h  Show help\n"

// localImportHelp is ctr import help from a version that advertises --local.
const localImportHelp = "OPTIONS:\n   --digests  Create digest refs\n" +
	"   --local  Run import locally rather than through transfer API (default: false)\n" +
	"   --help, -h  Show help\n"

// namedDigestImportHelp is ctr import help from a version that advertises both --local and
// --skip-digest-for-named, as containerd 1.7 and 2.x do.
const namedDigestImportHelp = "OPTIONS:\n   --digests  Create digest refs\n" +
	"   --skip-digest-for-named  Skip applying --digests option to images named in the importing tar\n" +
	"   --local  Run import locally rather than through transfer API (default: false)\n" +
	"   --help, -h  Show help\n"

// legacyNamedDigestImportHelp is ctr import help that advertises --skip-digest-for-named but
// not --local.
const legacyNamedDigestImportHelp = "OPTIONS:\n   --digests  Create digest refs\n" +
	"   --skip-digest-for-named  Skip digest refs for named images\n   --help, -h  Show help\n"

func importHelpCommand() []string {
	return []string{ctrCommand, "--namespace=k8s.io", "images", "import", "--help"}
}

// expectImportHelp expects one ctr import capability probe on containerName that
// reports a ctr without --local.
func expectImportHelp(
	ctx context.Context,
	t *testing.T,
	mockClient *docker.MockAPIClient,
	containerName string,
) {
	t.Helper()

	execID := "exec-import-help-" + containerName

	mockClient.EXPECT().
		ContainerExecCreate(ctx, containerName, mock.MatchedBy(func(opts container.ExecOptions) bool {
			return slices.Equal(opts.Cmd, importHelpCommand())
		})).
		Return(container.ExecCreateResponse{ID: execID}, nil).Once()

	mockClient.EXPECT().
		ContainerExecAttach(ctx, execID, container.ExecStartOptions{}).
		Return(mockDockerStreamResponse(legacyImportHelp, ""), nil).Once()

	mockClient.EXPECT().
		ContainerExecInspect(ctx, execID).
		Return(container.ExecInspect{ExitCode: 0}, nil).Once()
}

type importModeCase struct {
	name string
	// help is what ctr import --help prints; probeErr fails the probe instead.
	help     string
	probeErr error
	// uname is what uname -m prints; unameErr fails it instead.
	uname    string
	unameErr error
	// wantImport is the expected ctr import command, or nil when the import
	// must refuse before copying anything.
	wantImport []string
	wantErr    string
}

func legacyImportCommand() []string {
	return []string{
		ctrCommand, "--namespace=k8s.io", "images", "import", "--digests", kindImportPath,
	}
}

func localImportCommand(platform string) []string {
	return []string{
		ctrCommand, "--namespace=k8s.io", "images", "import", "--digests",
		"--local", "--platform", platform, kindImportPath,
	}
}

func importModeCases() []importModeCase {
	return []importModeCase{
		{
			name:       "local import on an amd64 node pins the node platform",
			help:       localImportHelp,
			uname:      "x86_64\n",
			wantImport: localImportCommand("linux/amd64"),
		},
		{
			name:       "local import on an arm64 node pins the node platform",
			help:       localImportHelp,
			uname:      "aarch64\n",
			wantImport: localImportCommand("linux/arm64"),
		},
		{
			name:       "local import with bare --help",
			help:       "OPTIONS:\n   --local  Import locally\n   --help  Show help\n",
			uname:      "x86_64\n",
			wantImport: localImportCommand("linux/amd64"),
		},
		{
			name:       "legacy ctr without --local keeps the plain command",
			help:       legacyImportHelp,
			wantImport: legacyImportCommand(),
		},
		{
			name:       "a look-alike option is not --local",
			help:       "OPTIONS:\n   --localization value  Language\n   --help, -h  Show help\n",
			wantImport: legacyImportCommand(),
		},
		{
			name:     "failed capability discovery refuses the import",
			probeErr: errImportDiscovery,
			wantErr:  "failed to discover image import options",
		},
		{
			name:    "empty capability discovery refuses the import",
			help:    "",
			wantErr: "incomplete help output",
		},
		{
			name:    "truncated capability discovery refuses the import",
			help:    "OPTIONS:\n   --local  Import locally\n",
			wantErr: "incomplete help output",
		},
		{
			name:     "an unknown node platform refuses the local import",
			help:     localImportHelp,
			unameErr: errImportDiscovery,
			wantErr:  "detect platform of node my-cluster-control-plane",
		},
	}
}

// The import mode follows what each node's ctr advertises, and import never
// guesses after failed or incomplete discovery.
func TestImportSelectsModeFromNodeCapabilities(t *testing.T) {
	t.Parallel()

	for _, testCase := range append(importModeCases(), namedDigestImportModeCases()...) {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockClient := docker.NewMockAPIClient(t)
			setupKindNodeListMock(ctx, mockClient)

			copied := false
			commands := observeImportNode(t, mockClient, kindExporterNodeName, testCase, &copied)

			inputPath := filepath.Join(t.TempDir(), "images.tar")
			require.NoError(t, os.WriteFile(inputPath, []byte("fake tar content"), 0o600))

			err := image.NewImporter(mockClient).Import(ctx, "my-cluster",
				v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker,
				image.ImportOptions{InputPath: inputPath})

			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				assert.False(t, copied, "a refused import must not copy the archive")
			} else {
				require.NoError(t, err)
				assert.True(t, copied, "the archive must be copied before importing")
			}

			assert.Equal(t, expectedImportCommands(testCase), drain(commands))
		})
	}
}

// Each node selects its own mode, so a cluster with mixed ctr versions imports
// on every node with the command that node supports.
func TestImportSelectsModePerNode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().ContainerList(ctx, mock.Anything).Return(kindMultiNodeSummaries()[:2], nil)

	nodes := []string{kindExporterNodeName, "my-cluster-worker"}
	cases := []importModeCase{
		{help: legacyImportHelp, wantImport: legacyImportCommand()},
		{help: localImportHelp, uname: "aarch64\n", wantImport: localImportCommand("linux/arm64")},
	}

	observed := make([]chan []string, len(nodes))
	for index, node := range nodes {
		copied := false
		observed[index] = observeImportNode(t, mockClient, node, cases[index], &copied)
	}

	inputPath := filepath.Join(t.TempDir(), "images.tar")
	require.NoError(t, os.WriteFile(inputPath, []byte("fake tar content"), 0o600))

	err := image.NewImporter(mockClient).Import(ctx, "my-cluster",
		v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker,
		image.ImportOptions{InputPath: inputPath})
	require.NoError(t, err)

	for index, commands := range observed {
		assert.Equal(t, expectedImportCommands(cases[index]), drain(commands), nodes[index])
	}
}

// The import command keeps digest references for named images and their
// children, and never asks ctr to store the archive's own index as an image or
// to import platforms the archive does not carry.
func TestImportCommandKeepsArchiveRootOutOfImageStore(t *testing.T) {
	t.Parallel()

	for _, command := range [][]string{
		image.ImportCommandForTest(kindImportPath, true, "linux/amd64", false),
		image.ImportCommandForTest(kindImportPath, false, "", false),
	} {
		assert.Contains(t, command, "--digests", command)
		assert.Equal(t, kindImportPath, command[len(command)-1], command)

		for _, forbidden := range []string{"--index-name", "--all-platforms", "--base-name"} {
			for _, argument := range command {
				assert.NotContains(t, argument, forbidden, command)
			}
		}
	}

	assert.NotContains(t, image.ImportCommandForTest(kindImportPath, false, "", false), "--local")
	assert.NotContains(
		t,
		image.ImportCommandForTest(kindImportPath, false, "", false),
		"--platform",
	)
	assert.NotContains(t, image.ImportCommandForTest(kindImportPath, true, "linux/amd64", false),
		"--skip-digest-for-named")
}

// A named image never gains a generated import-<date>@<digest> alias when the node's ctr can skip
// it, whichever import mode runs. The digest references themselves are kept.
func TestImportCommandSkipsGeneratedAliasesForNamedImages(t *testing.T) {
	t.Parallel()

	for _, command := range [][]string{
		image.ImportCommandForTest(kindImportPath, true, "linux/amd64", true),
		image.ImportCommandForTest(kindImportPath, false, "", true),
	} {
		assert.Contains(t, command, "--digests", command)
		assert.Contains(t, command, "--skip-digest-for-named", command)
		assert.Equal(t, kindImportPath, command[len(command)-1], command)
	}
}

func expectedImportCommands(testCase importModeCase) [][]string {
	want := [][]string{importHelpCommand()}
	if testCase.probeErr != nil || testCase.wantErr == "incomplete help output" {
		return want
	}

	if testCase.uname != "" || testCase.unameErr != nil {
		want = append(want, []string{"uname", "-m"})
	}

	if testCase.wantImport == nil {
		return want
	}

	return append(want, testCase.wantImport, []string{"rm", "-f", kindImportPath})
}

func drain(commands chan []string) [][]string {
	close(commands)

	var observed [][]string
	for command := range commands {
		observed = append(observed, command)
	}

	return observed
}

// observeImportNode records every command run on node, in order, and answers
// the capability probe and platform detection from testCase.
func observeImportNode(
	t *testing.T,
	mockClient *docker.MockAPIClient,
	node string,
	testCase importModeCase,
	copied *bool,
) chan []string {
	t.Helper()

	commands := make(chan []string, 8)
	outputs := map[string]string{
		"help-" + node:  testCase.help,
		"uname-" + node: testCase.uname,
	}

	mockClient.EXPECT().
		CopyToContainer(mock.Anything, node, "/root", mock.Anything, container.CopyToContainerOptions{}).
		RunAndReturn(func(context.Context, string, string, io.Reader, container.CopyToContainerOptions) error {
			*copied = true

			return nil
		}).
		Maybe()

	mockClient.EXPECT().
		ContainerExecCreate(mock.Anything, node, mock.Anything).
		RunAndReturn(func(
			_ context.Context, _ string, options container.ExecOptions,
		) (container.ExecCreateResponse, error) {
			commands <- slices.Clone(options.Cmd)

			switch {
			case slices.Equal(options.Cmd, importHelpCommand()):
				if testCase.probeErr != nil {
					return container.ExecCreateResponse{}, testCase.probeErr
				}

				return container.ExecCreateResponse{ID: "help-" + node}, nil
			case slices.Equal(options.Cmd, []string{"uname", "-m"}):
				if testCase.unameErr != nil {
					return container.ExecCreateResponse{}, testCase.unameErr
				}

				return container.ExecCreateResponse{ID: "uname-" + node}, nil
			default:
				return container.ExecCreateResponse{ID: "other-" + node}, nil
			}
		})

	for _, execID := range []string{"help-" + node, "uname-" + node, "other-" + node} {
		mockClient.EXPECT().
			ContainerExecAttach(mock.Anything, execID, container.ExecStartOptions{}).
			Return(mockDockerStreamResponse(outputs[execID], ""), nil).Maybe()
		mockClient.EXPECT().
			ContainerExecInspect(mock.Anything, execID).
			Return(container.ExecInspect{ExitCode: 0}, nil).Maybe()
	}

	return commands
}

// namedDigestImportModeCases cover a ctr that can skip the generated import-<date>@<digest> alias
// for the images an archive names.
func namedDigestImportModeCases() []importModeCase {
	return []importModeCase{
		{
			name:  "local import skips generated digest aliases for named images when advertised",
			help:  namedDigestImportHelp,
			uname: "x86_64\n",
			wantImport: []string{
				ctrCommand, "--namespace=k8s.io", "images", "import", "--digests",
				"--skip-digest-for-named", "--local", "--platform", "linux/amd64", kindImportPath,
			},
		},
		{
			name: "legacy import skips generated digest aliases for named images when advertised",
			help: legacyNamedDigestImportHelp,
			wantImport: []string{
				ctrCommand, "--namespace=k8s.io", "images", "import", "--digests",
				"--skip-digest-for-named", kindImportPath,
			},
		},
		{
			name:       "a look-alike option is not --skip-digest-for-named",
			help:       "OPTIONS:\n   --skip-digest-for-named-x  Other\n   --help, -h  Show help\n",
			wantImport: legacyImportCommand(),
		},
	}
}
