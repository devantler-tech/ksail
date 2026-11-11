package image

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/docker/docker/api/types/container"
)

// ImportOptions configures the image import operation.
type ImportOptions struct {
	// InputPath is the file path of the tar archive to import.
	// Defaults to "images.tar" in the current directory.
	InputPath string
}

// newImportArchiveName returns the file name of one import's archive on a node.
// Each import gets its own name, so two imports on the same node never write to
// or remove each other's archive. Tests replace it to keep the node-side path
// predictable.
//
//nolint:gochecknoglobals // Test seam: tests pin the name so node-side commands stay exact.
var newImportArchiveName = defaultImportArchiveName

// defaultImportArchiveName returns a random node-side import archive name.
func defaultImportArchiveName() string {
	return randomNodeArchiveName("ksail-images-import-")
}

// Importer handles importing container images to cluster containerd.
type Importer struct {
	dockerClient dockerclient.Client
	executor     *ContainerExecutor
}

// NewImporter creates a new image importer.
func NewImporter(dockerClient dockerclient.Client) *Importer {
	return &Importer{
		dockerClient: dockerClient,
		executor:     NewContainerExecutor(dockerClient),
	}
}

// NewImporterFromDefaultClient connects to the default Docker client and wraps it in an Importer, so
// a caller that just needs "the importer" (not a specific injected client) does not have to repeat the
// connect/wrap/cleanup boilerplate — the setup shared by every CLI command that imports images. The
// returned cleanup func is always non-nil and safe to defer, even when err is non-nil.
func NewImporterFromDefaultClient() (*Importer, func(), error) {
	dockerClient, err := dockerclient.GetDockerClient()
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return NewImporter(dockerClient), func() { _ = dockerClient.Close() }, nil
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
	err := validateImageOpParams(distribution, providerType)
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

	// Refuse before copying anything when the archive cannot serve a node's platform.
	err = i.requireNodePlatforms(ctx, k8sNodes, inputPath)
	if err != nil {
		return err
	}

	// Get the appropriate temp path for this distribution
	tmpBasePath := getTempPath(distribution)

	// Import to all K8s nodes in parallel
	var waitGroup sync.WaitGroup

	errChan := make(chan error, len(k8sNodes))

	for _, node := range k8sNodes {
		waitGroup.Go(func() {
			importErr := i.importImagesToNode(ctx, node.Name, inputPath, tmpBasePath)
			if importErr != nil {
				errChan <- fmt.Errorf("failed to import images to node %s: %w", node.Name, importErr)
			}
		})
	}

	waitGroup.Wait()
	close(errChan)

	// Collect all errors
	errs := make([]error, 0, len(k8sNodes))
	for importErr := range errChan {
		errs = append(errs, importErr)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// getK8sNodes gets the list of Kubernetes nodes for the cluster, filtering out
// helper containers (load balancer, tools, registry) that lack containerd.
func (i *Importer) getK8sNodes(
	ctx context.Context,
	clusterName string,
	distribution v1alpha1.Distribution,
) ([]provider.NodeInfo, error) {
	nodes, err := listClusterNodes(ctx, i.dockerClient, clusterName, distribution)
	if err != nil {
		return nil, err
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

// requireNodePlatforms refuses the import before any copy when the archive carries no content
// for a node's platform for one of its multi-platform images. ctr would otherwise fail later, on
// that node only, with a missing-content error. Nodes are checked only when the archive holds a
// multi-platform image.
func (i *Importer) requireNodePlatforms(
	ctx context.Context,
	nodes []provider.NodeInfo,
	inputPath string,
) error {
	coverage, err := readArchivePlatformCoverage(inputPath)
	if err != nil {
		return fmt.Errorf("check image archive platforms: %w", err)
	}

	if len(coverage) == 0 {
		return nil
	}

	for _, node := range nodes {
		platform, err := i.executor.detectPlatform(ctx, node.Name)
		if err != nil {
			return fmt.Errorf("detect platform of node %s: %w", node.Name, err)
		}

		err = requireArchivePlatform(coverage, node.Name, platform)
		if err != nil {
			return err
		}
	}

	return nil
}

// importImagesToNode imports images from a tar archive to a node's containerd.
func (i *Importer) importImagesToNode(
	ctx context.Context,
	nodeName string,
	inputPath string,
	tmpBasePath string,
) error {
	// Copy the tar file from host to container. Each import gets its own archive
	// name, and the archive is removed however the import ends, even after a
	// partial copy or a cancellation.
	tmpPath := tmpBasePath + "/" + newImportArchiveName()

	defer i.executor.removeNodeArchive(ctx, nodeName, tmpPath)

	err := i.copyToContainer(ctx, nodeName, inputPath, tmpPath)
	if err != nil {
		return fmt.Errorf("failed to copy import file to container: %w", err)
	}

	// Build ctr import command
	// Note: We don't use --all-platforms because multi-platform images may have
	// manifests for platforms whose layers aren't present, causing import to fail.
	cmd := []string{
		ctrCommand, ctrNamespaceArg, ctrImages, "import",
		"--digests",
		tmpPath,
	}

	// Execute import command
	_, err = i.executor.ExecInContainer(ctx, nodeName, cmd)
	if err != nil {
		return fmt.Errorf("ctr import failed: %w", err)
	}

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
