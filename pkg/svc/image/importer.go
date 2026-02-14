package image

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ImportOptions configures the image import operation.
type ImportOptions struct {
	// InputPath is the file path of the tar archive to import.
	// Defaults to "images.tar" in the current directory.
	InputPath string
}

// Importer handles importing container images to cluster containerd.
type Importer struct {
	dockerClient client.APIClient
	executor     *ContainerExecutor
}

// NewImporter creates a new image importer.
func NewImporter(dockerClient client.APIClient) *Importer {
	return &Importer{
		dockerClient: dockerClient,
		executor:     NewContainerExecutor(dockerClient),
	}
}

// Import imports container images to the cluster's containerd runtime.
func (i *Importer) Import(
	ctx context.Context,
	clusterName string,
	distribution v1alpha1.Distribution,
	providerType v1alpha1.Provider,
	opts ImportOptions,
) error {
	// Validate distribution and provider
	err := i.validateImportParams(distribution, providerType)
	if err != nil {
		return err
	}

	// Set default input path and validate file exists
	inputPath := opts.InputPath
	if inputPath == "" {
		inputPath = "images.tar"
	}

	_, err = os.Stat(inputPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrInputFileNotFound, inputPath)
	}

	// Get K8s nodes for the cluster
	k8sNodes, err := i.getK8sNodes(ctx, clusterName, distribution)
	if err != nil {
		return err
	}

	// Get the appropriate temp path for this distribution
	tmpBasePath := getTempPath(distribution)

	// Import to all K8s nodes
	for _, node := range k8sNodes {
		err = i.importImagesToNode(ctx, node.Name, inputPath, tmpBasePath)
		if err != nil {
			return fmt.Errorf("failed to import images to node %s: %w", node.Name, err)
		}
	}

	return nil
}

// validateImportParams validates the distribution and provider for import operations.
func (i *Importer) validateImportParams(
	distribution v1alpha1.Distribution,
	providerType v1alpha1.Provider,
) error {
	// Talos and VCluster are not supported - Talos is an immutable OS without shell access,
	// VCluster (Vind) runs its own containerd inside Docker containers without standard
	// exec-based image import support.
	if distribution == v1alpha1.DistributionTalos ||
		distribution == v1alpha1.DistributionVCluster {
		return ErrUnsupportedDistribution
	}

	// Only Docker provider is supported via Docker SDK
	if providerType != v1alpha1.ProviderDocker && providerType != "" {
		return fmt.Errorf(
			"%w: %s (only Docker provider supported)",
			ErrUnsupportedProvider,
			providerType,
		)
	}

	return nil
}

// getK8sNodes gets the list of Kubernetes nodes for the cluster.
func (i *Importer) getK8sNodes(
	ctx context.Context,
	clusterName string,
	distribution v1alpha1.Distribution,
) ([]provider.NodeInfo, error) {
	// Get the label scheme for the distribution
	labelScheme := getLabelScheme(distribution)

	// Get nodes for the cluster
	dockerProvider := dockerprovider.NewProvider(i.dockerClient, labelScheme)

	nodes, err := dockerProvider.ListNodes(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: cluster %s", ErrNoNodes, clusterName)
	}

	// Filter out helper containers and get actual K8s nodes
	k8sNodes := filterK8sNodes(nodes)
	if len(k8sNodes) == 0 {
		return nil, fmt.Errorf(
			"%w: no suitable nodes found in cluster %s",
			ErrNoK8sNodesFound,
			clusterName,
		)
	}

	return k8sNodes, nil
}

// filterK8sNodes filters out helper containers (tools, loadbalancer, registry) and returns
// only actual Kubernetes nodes that have containerd.
func filterK8sNodes(nodes []provider.NodeInfo) []provider.NodeInfo {
	var result []provider.NodeInfo

	for _, node := range nodes {
		if !isHelperContainer(node.Role) {
			result = append(result, node)
		}
	}

	return result
}

// importImagesToNode imports images from a tar archive to a node's containerd.
func (i *Importer) importImagesToNode(
	ctx context.Context,
	nodeName string,
	inputPath string,
	tmpBasePath string,
) error {
	// Copy the tar file from host to container
	tmpPath := tmpBasePath + "/ksail-images-import.tar"

	err := i.copyToContainer(ctx, nodeName, inputPath, tmpPath)
	if err != nil {
		return fmt.Errorf("failed to copy import file to container: %w", err)
	}

	// Build ctr import command
	// Note: We don't use --all-platforms because multi-platform images may have
	// manifests for platforms whose layers aren't present, causing import to fail.
	cmd := []string{
		"ctr", "--namespace=k8s.io", "images", "import",
		"--digests",
		tmpPath,
	}

	// Execute import command
	_, err = i.executor.ExecInContainer(ctx, nodeName, cmd)
	if err != nil {
		return fmt.Errorf("ctr import failed: %w", err)
	}

	// Clean up temporary file in container
	_, _ = i.executor.ExecInContainer(ctx, nodeName, []string{"rm", "-f", tmpPath})

	return nil
}

// copyToContainer copies a file from the host to a container.
func (i *Importer) copyToContainer(
	ctx context.Context,
	containerName string,
	srcPath string,
	dstPath string,
) error {
	// Open source file
	srcFile, err := os.Open(srcPath) //nolint:gosec // Path is from internal code
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}

	defer func() { _ = srcFile.Close() }()

	// Get file info
	fileInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create a tar archive containing the file
	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	header := &tar.Header{
		Name: filepath.Base(dstPath),
		Mode: 0o644, //nolint:mnd // Standard file permission
		Size: fileInfo.Size(),
	}

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	_, err = io.Copy(tarWriter, srcFile)
	if err != nil {
		return fmt.Errorf("failed to write file to tar: %w", err)
	}

	err = tarWriter.Close()
	if err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Copy to container
	err = i.dockerClient.CopyToContainer(
		ctx,
		containerName,
		filepath.Dir(dstPath),
		&buf,
		container.CopyToContainerOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to copy to container: %w", err)
	}

	return nil
}
