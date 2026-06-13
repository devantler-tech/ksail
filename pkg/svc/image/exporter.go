package image

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
)

// File permission constant.
const dirPerm = 0o750

const (
	// ctrCommand is the containerd CLI binary used to manage images on cluster nodes.
	ctrCommand = "ctr"
	// ctrNamespaceArg selects containerd's k8s.io namespace for all ctr invocations.
	ctrNamespaceArg = "--namespace=k8s.io"
	// ctrImages is the ctr subcommand group for image operations.
	ctrImages = "images"
)

// Error definitions.
var (
	// ErrNoNodes is returned when no cluster nodes are found.
	ErrNoNodes = errors.New("no cluster nodes found")
	// ErrUnsupportedProvider is returned when the provider is not supported for image operations.
	ErrUnsupportedProvider = errors.New("unsupported provider for image operations")
	// ErrNoImagesFound is returned when no images are found in the cluster.
	ErrNoImagesFound = errors.New("no images found in cluster")
)

// ExportOptions configures the image export operation.
type ExportOptions struct {
	// OutputPath is the file path for the exported tar archive.
	// Defaults to "images.tar" in the current directory.
	OutputPath string
	// Images is an optional list of specific images to export.
	// If empty, all images in the cluster will be exported.
	Images []string
}

// Exporter handles exporting container images from cluster containerd.
type Exporter struct {
	dockerClient dockerclient.Client
	executor     *ContainerExecutor
}

// NewExporter creates a new image exporter.
func NewExporter(dockerClient dockerclient.Client) *Exporter {
	return &Exporter{
		dockerClient: dockerClient,
		executor:     NewContainerExecutor(dockerClient),
	}
}

// Export exports container images from the cluster's containerd runtime.
func (e *Exporter) Export(
	ctx context.Context,
	clusterName string,
	distribution v1alpha1.Distribution,
	providerType v1alpha1.Provider,
	opts ExportOptions,
) error {
	err := validateImageOpParams(distribution, providerType)
	if err != nil {
		return err
	}

	// Set default output path
	if opts.OutputPath == "" {
		opts.OutputPath = "images.tar"
	}

	// Find a suitable node for export
	nodeName, err := e.findExportNode(ctx, clusterName, distribution)
	if err != nil {
		return err
	}

	// Get the temp path for this distribution
	tmpPath := getTempPath(distribution)

	// Get list of images to export
	images, err := e.resolveImages(ctx, nodeName, opts)
	if err != nil {
		return err
	}

	var (
		repairImages  []string
		resolveImages []string
	)

	if len(opts.Images) > 0 {
		resolveImages = normalizeImageRefs(opts.Images)
		repairImages = mergeRepairImages(images, resolveImages)
	}

	// Export images using ctr inside the node container
	return e.exportImagesFromNode(
		ctx,
		nodeName,
		opts.OutputPath,
		images,
		tmpPath,
		repairImages,
		resolveImages,
	)
}

// findExportNode finds a suitable node for export operations. The
// unsupported-distribution and Docker-only provider guards are validated by
// Export via validateImageOpParams before this is reached.
func (e *Exporter) findExportNode(
	ctx context.Context,
	clusterName string,
	distribution v1alpha1.Distribution,
) (string, error) {
	nodes, err := listClusterNodes(ctx, e.dockerClient, clusterName, distribution)
	if err != nil {
		return "", err
	}

	// Find a suitable node for export (control-plane or server node)
	nodeName := selectNodeForExport(nodes)
	if nodeName == "" {
		return "", fmt.Errorf("%w: no suitable node found in cluster %s", ErrNoNodes, clusterName)
	}

	return nodeName, nil
}

// exportImagesFromNode exports images from a node's containerd to the host filesystem.
func (e *Exporter) exportImagesFromNode(
	ctx context.Context,
	nodeName string,
	outputPath string,
	images []string,
	tmpBasePath string,
	repairImages []string,
	resolveImages []string,
) error {
	// Create a temporary file path inside the container
	tmpPath := tmpBasePath + "/ksail-images-export.tar"

	// Detect the node's platform (OS/architecture)
	platform, err := e.detectNodePlatform(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("failed to detect node platform: %w", err)
	}

	exportImages, exportErr := e.tryExportImagesWithRepair(
		ctx,
		nodeName,
		tmpPath,
		platform,
		images,
		repairImages,
		resolveImages,
	)
	if exportErr != nil {
		err = e.fallbackExportImages(
			ctx,
			nodeName,
			tmpPath,
			platform,
			exportImages,
			exportErr,
		)
		if err != nil {
			return err
		}
	}

	// Copy the tar file from container to host
	err = e.copyFromContainer(ctx, nodeName, tmpPath, outputPath)
	if err != nil {
		return fmt.Errorf("failed to copy export file from container: %w", err)
	}

	// Validate blob integrity in the exported OCI tar archive.
	// Catches truncated or corrupted blobs that ctr export may silently produce
	// when the containerd content store has incomplete data.
	err = ValidateExportedTar(outputPath)
	if err != nil {
		return err
	}

	// Clean up temporary file in container
	_, _ = e.executor.ExecInContainer(ctx, nodeName, []string{"rm", "-f", tmpPath})

	return nil
}

// detectNodePlatform detects the OS/architecture of the node container.
// Returns a platform string in the format "os/arch" (e.g., "linux/amd64", "linux/arm64").
func (e *Exporter) detectNodePlatform(ctx context.Context, nodeName string) (string, error) {
	// Use uname to detect architecture
	output, err := e.executor.ExecInContainer(ctx, nodeName, []string{"uname", "-m"})
	if err != nil {
		return "", fmt.Errorf("failed to detect architecture: %w", err)
	}

	arch := strings.TrimSpace(output)

	// Convert uname output to Go/OCI architecture names
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	case "armv7l":
		arch = "arm"
	}

	// Always linux for Kubernetes nodes
	return "linux/" + arch, nil
}

// copyFromContainer copies a file from a container to the host.
func (e *Exporter) copyFromContainer(
	ctx context.Context,
	containerName string,
	srcPath string,
	dstPath string,
) error {
	reader, _, err := e.dockerClient.CopyFromContainer(ctx, containerName, srcPath)
	if err != nil {
		return fmt.Errorf("failed to copy from container: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// The response is a tar archive containing the file
	tarReader := tar.NewReader(reader)

	// Find the file in the tar archive
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: %s", ErrFileNotFoundInArchive, srcPath)
		}

		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// The file we want
		if header.Typeflag == tar.TypeReg {
			return e.writeFileFromTar(dstPath, tarReader)
		}
	}
}

// writeFileFromTar extracts content from tar reader to a destination file.
func (e *Exporter) writeFileFromTar(dstPath string, tarReader *tar.Reader) error {
	// Ensure parent directory exists
	dir := filepath.Dir(dstPath)

	err := os.MkdirAll(dir, dirPerm)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create the destination file
	dstFile, err := os.Create(dstPath) //nolint:gosec // Path from internal code
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}

	defer func() { _ = dstFile.Close() }()

	// Copy the file content with size limit for decompression bomb protection
	_, err = io.Copy(dstFile, tarReader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
