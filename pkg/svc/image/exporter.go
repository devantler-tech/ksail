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

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
	"github.com/docker/docker/client"
)

// File permission constant.
const dirPerm = 0o750

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
	dockerClient client.APIClient
	executor     *ContainerExecutor
}

// NewExporter creates a new image exporter.
func NewExporter(dockerClient client.APIClient) *Exporter {
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
	// Talos is not supported - it's an immutable OS without shell access
	// and its Machine API doesn't expose image export functionality
	if distribution == v1alpha1.DistributionTalos {
		return ErrUnsupportedDistribution
	}

	// Set default output path
	if opts.OutputPath == "" {
		opts.OutputPath = "images.tar"
	}

	// Find a suitable node for export
	nodeName, err := e.findExportNode(ctx, clusterName, distribution, providerType)
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

	// Export images using ctr inside the node container
	return e.exportImagesFromNode(ctx, nodeName, opts.OutputPath, images, tmpPath)
}

// findExportNode finds a suitable node for export operations.
func (e *Exporter) findExportNode(
	ctx context.Context,
	clusterName string,
	distribution v1alpha1.Distribution,
	providerType v1alpha1.Provider,
) (string, error) {
	// Only Docker provider is supported via Docker SDK
	if providerType != v1alpha1.ProviderDocker && providerType != "" {
		return "", fmt.Errorf(
			"%w: %s (only Docker provider supported)",
			ErrUnsupportedProvider,
			providerType,
		)
	}

	// Get the label scheme for the distribution
	labelScheme := getLabelScheme(distribution)

	// Get nodes for the cluster
	dockerProvider := dockerprovider.NewProvider(e.dockerClient, labelScheme)

	nodes, err := dockerProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return "", fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return "", fmt.Errorf("%w: cluster %s", ErrNoNodes, clusterName)
	}

	// Find a suitable node for export (control-plane or server node)
	nodeName := selectNodeForExport(nodes)
	if nodeName == "" {
		return "", fmt.Errorf("%w: no suitable node found in cluster %s", ErrNoNodes, clusterName)
	}

	return nodeName, nil
}

// resolveImages gets the list of images to export.
func (e *Exporter) resolveImages(
	ctx context.Context,
	nodeName string,
	opts ExportOptions,
) ([]string, error) {
	if len(opts.Images) > 0 {
		return opts.Images, nil
	}

	images, err := e.listImagesInNode(ctx, nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	if len(images) == 0 {
		return nil, ErrNoImagesFound
	}

	return images, nil
}

// listImagesInNode lists all images in the containerd of a node.
func (e *Exporter) listImagesInNode(
	ctx context.Context,
	nodeName string,
) ([]string, error) {
	// Build ctr command to list images
	cmd := []string{"ctr", "--namespace=k8s.io", "images", "list", "-q"}

	output, err := e.executor.ExecInContainer(ctx, nodeName, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list images in node %s: %w", nodeName, err)
	}

	// Parse output - each line is an image reference
	lines := strings.Split(strings.TrimSpace(output), "\n")
	images := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "sha256:") {
			// Skip digest-only references, keep named images
			images = append(images, line)
		}
	}

	return images, nil
}

// exportImagesFromNode exports images from a node's containerd to the host filesystem.
func (e *Exporter) exportImagesFromNode(
	ctx context.Context,
	nodeName string,
	outputPath string,
	images []string,
	tmpBasePath string,
) error {
	// Create a temporary file path inside the container
	tmpPath := tmpBasePath + "/ksail-images-export.tar"

	// Detect the node's platform (OS/architecture)
	platform, err := e.detectNodePlatform(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("failed to detect node platform: %w", err)
	}

	// Try exporting all images at once first (most efficient if it works)
	err = e.tryExportImages(ctx, nodeName, tmpPath, platform, images)
	if err != nil {
		// Fall back to exporting images one-by-one, skipping failures
		// This handles cases where some images have incomplete manifests
		// (e.g., multi-arch images where not all platform layers were pulled)
		successfulImages, failedImages := e.exportImagesOneByOne(ctx, nodeName, tmpPath, platform, images)
		if len(successfulImages) == 0 {
			return fmt.Errorf("ctr export failed for all images during individual export attempts (initial bulk export error: %w)", err)
		}

		// Report failed images to stderr
		if len(failedImages) > 0 {
			fmt.Fprintf(os.Stderr, "warning: failed to export %d image(s): %s\n", len(failedImages), strings.Join(failedImages, ", "))
		}

		// Re-export only the successful images together
		err = e.tryExportImages(ctx, nodeName, tmpPath, platform, successfulImages)
		if err != nil {
			return fmt.Errorf("ctr export failed: %w", err)
		}
	}

	// Copy the tar file from container to host
	err = e.copyFromContainer(ctx, nodeName, tmpPath, outputPath)
	if err != nil {
		return fmt.Errorf("failed to copy export file from container: %w", err)
	}

	// Clean up temporary file in container
	_, _ = e.executor.ExecInContainer(ctx, nodeName, []string{"rm", "-f", tmpPath})

	return nil
}

// tryExportImages attempts to export a set of images using ctr.
func (e *Exporter) tryExportImages(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
) error {
	// Build ctr export command with platform flag
	// The --platform flag is critical for multi-arch images: containerd may have
	// image manifests listing multiple platforms (e.g., linux/amd64, linux/arm64),
	// but only the layers for the running platform are actually downloaded.
	// Without --platform, ctr tries to export all platforms and fails with
	// "content digest not found" for missing platform layers.
	cmd := make([]string, 0, 7+len(images)) //nolint:mnd // fixed args + images
	cmd = append(
		cmd,
		"ctr",
		"--namespace=k8s.io",
		"images",
		"export",
		"--platform",
		platform,
		tmpPath,
	)
	cmd = append(cmd, images...)

	_, err := e.executor.ExecInContainer(ctx, nodeName, cmd)

	return err
}

// exportImagesOneByOne tests each image individually and returns the list of
// images that can be successfully exported, along with the list of failed images.
func (e *Exporter) exportImagesOneByOne(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
) ([]string, []string) {
	successful := make([]string, 0, len(images))
	failed := make([]string, 0, len(images))

	for _, image := range images {
		err := e.tryExportImages(ctx, nodeName, tmpPath, platform, []string{image})
		if err == nil {
			successful = append(successful, image)
		} else {
			failed = append(failed, image)
		}
	}

	// Clean up test export file
	_, _ = e.executor.ExecInContainer(ctx, nodeName, []string{"rm", "-f", tmpPath})

	return successful, failed
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

// getLabelScheme returns the Docker provider label scheme for a distribution.
func getLabelScheme(distribution v1alpha1.Distribution) dockerprovider.LabelScheme {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return dockerprovider.LabelSchemeKind
	case v1alpha1.DistributionK3s:
		return dockerprovider.LabelSchemeK3d
	case v1alpha1.DistributionTalos:
		// Talos is not supported for image operations, but we return Kind as fallback
		// The caller should have already checked for Talos and returned an error
		return dockerprovider.LabelSchemeKind
	}

	return dockerprovider.LabelSchemeKind
}

// Node role priorities for export selection.
const (
	rolePriorityControlPlane    = 0
	rolePriorityServer          = 1
	rolePriorityWorker          = 2
	rolePriorityAgent           = 3
	rolePriorityUnknown         = 4
	rolePriorityUnselectedStart = 999
)

// selectNodeForExport selects a suitable node for export operations.
// It prefers control-plane/server nodes over workers, and filters out
// helper containers (tools, loadbalancer, etc.) that don't have containerd.
func selectNodeForExport(nodes []provider.NodeInfo) string {
	// Define preferred role order - control-plane/server nodes first
	rolePreference := map[string]int{
		"control-plane": rolePriorityControlPlane, // Kind control-plane
		"server":        rolePriorityServer,       // K3d server (control-plane)
		"worker":        rolePriorityWorker,       // Kind/K3d worker nodes
		"agent":         rolePriorityAgent,        // K3d agent nodes
		"":              rolePriorityUnknown,      // Unknown role - fallback
	}

	// Roles to exclude (helper containers without containerd)
	excludedRoles := map[string]bool{
		"loadbalancer": true, // K3d load balancer proxy
		"noRole":       true, // K3d tools container
	}

	var bestNode provider.NodeInfo

	bestPriority := rolePriorityUnselectedStart

	for _, node := range nodes {
		// Skip excluded roles
		if excludedRoles[node.Role] {
			continue
		}

		priority, ok := rolePreference[node.Role]
		if !ok {
			priority = rolePriorityUnknown // Unknown role gets low priority
		}

		if priority < bestPriority {
			bestPriority = priority
			bestNode = node
		}
	}

	return bestNode.Name
}

// Temp path constants for different distributions.
const (
	tmpPathRoot = "/root" // Kind containers
	tmpPathTmp  = "/tmp"  // K3d containers
)

// getTempPath returns the appropriate temp directory path for a distribution.
// Kind containers have tmpfs on /tmp which Docker cp can't access properly,
// so we use /root instead. K3d containers don't have /root but /tmp works fine.
func getTempPath(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return tmpPathRoot // Kind has tmpfs on /tmp
	case v1alpha1.DistributionK3s:
		return tmpPathTmp // K3d doesn't have /root
	case v1alpha1.DistributionTalos:
		// Talos is not supported for image operations, but we return /tmp as fallback
		// The caller should have already checked for Talos and returned an error
		return tmpPathTmp
	}

	return tmpPathTmp
}
