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
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v6/pkg/svc/provider/docker"
	"github.com/docker/docker/client"
)

// File permission constant.
const dirPerm = 0o750

// contentPullTimeout caps the best-effort "ctr images pull" that is issued when
// an individual image export fails with a "content digest not found" error.
// Without a deadline, a slow or unreachable registry can block the entire
// export indefinitely. The pull remains best-effort (errors, including
// timeouts, are ignored), and the export retry still proceeds afterward.
const contentPullTimeout = 5 * time.Minute

// Error definitions.
var (
	// ErrNoNodes is returned when no cluster nodes are found.
	ErrNoNodes = errors.New("no cluster nodes found")
	// ErrUnsupportedProvider is returned when the provider is not supported for image operations.
	ErrUnsupportedProvider = errors.New("unsupported provider for image operations")
	// ErrNoImagesFound is returned when no images are found in the cluster.
	ErrNoImagesFound = errors.New("no images found in cluster")
	// ErrDigestOnlyReference is returned when an image reference omits its repository name.
	ErrDigestOnlyReference = errors.New("digest-only references are not supported")
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
	// Talos and VCluster are not supported - Talos is an immutable OS without shell access,
	// VCluster (Vind) runs its own containerd inside Docker containers without standard
	// exec-based image import support.
	if distribution == v1alpha1.DistributionTalos ||
		distribution == v1alpha1.DistributionVCluster {
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

	var requestedImages []string
	if len(opts.Images) > 0 {
		requestedImages = images
	}

	// Export images using ctr inside the node container
	return e.exportImagesFromNode(
		ctx,
		nodeName,
		opts.OutputPath,
		images,
		tmpPath,
		requestedImages,
	)
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
		// Normalize user-supplied image references so bare Docker Hub refs
		// (e.g. "traefik/whoami:v1.10") are expanded to their fully qualified
		// form ("docker.io/traefik/whoami:v1.10") matching how containerd stores them.
		normalized := make([]string, 0, len(opts.Images))
		for _, img := range opts.Images {
			img = strings.TrimSpace(img)
			if img == "" {
				continue
			}

			if strings.HasPrefix(img, "sha256:") {
				return nil, fmt.Errorf(
					"invalid image reference %q: %w; provide a named image reference instead",
					img,
					ErrDigestOnlyReference,
				)
			}

			normalized = append(normalized, NormalizeImageRef(img))
		}

		if len(normalized) == 0 {
			return nil, fmt.Errorf(
				"no valid images provided (all specified names are empty or whitespace): %w",
				ErrNoImagesFound,
			)
		}

		return normalized, nil
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

// resolveSpecifiedImages normalizes user-provided image refs and, when possible,
// resolves them to the exact refs already present in the node's image store.
func (e *Exporter) resolveSpecifiedImages(
	ctx context.Context,
	nodeName string,
	requestedImages []string,
) []string {
	normalizedRequested := normalizeImageRefs(requestedImages)

	localImages, err := e.listImagesInNode(ctx, nodeName)
	if err != nil {
		return normalizedRequested
	}

	lookup := newLocalImageLookup(localImages)
	resolved := make([]string, 0, len(normalizedRequested))

	for _, imageRef := range normalizedRequested {
		resolved = append(resolved, lookup.resolve(imageRef))
	}

	return resolved
}

type localImageLookup struct {
	byExactRef      map[string]string
	byBaseRef       map[string]string
	byRepoDigestRef map[string]string
}

func newLocalImageLookup(localImages []string) localImageLookup {
	lookup := localImageLookup{
		byExactRef:      make(map[string]string, len(localImages)),
		byBaseRef:       make(map[string]string, len(localImages)),
		byRepoDigestRef: make(map[string]string, len(localImages)),
	}

	for _, imageRef := range localImages {
		lookup.add(imageRef)
	}

	return lookup
}

func (l localImageLookup) add(imageRef string) {
	l.byExactRef[imageRef] = imageRef

	baseRef := stripDigestFromImageRef(imageRef)
	if _, exists := l.byBaseRef[baseRef]; !exists {
		l.byBaseRef[baseRef] = imageRef
	}

	normalizedBaseRef := NormalizeImageRef(baseRef)
	if _, exists := l.byBaseRef[normalizedBaseRef]; !exists {
		l.byBaseRef[normalizedBaseRef] = imageRef
	}

	digest := imageDigest(imageRef)
	if digest == "" {
		return
	}

	repoDigestRef := imageRepository(imageRef) + "@" + digest
	if _, exists := l.byRepoDigestRef[repoDigestRef]; !exists {
		l.byRepoDigestRef[repoDigestRef] = imageRef
	}
}

func (l localImageLookup) resolve(imageRef string) string {
	if exactRef, exists := l.byExactRef[imageRef]; exists {
		return exactRef
	}

	if localRef, exists := l.byBaseRef[imageRef]; exists {
		return localRef
	}

	digest := imageDigest(imageRef)
	if digest == "" {
		return imageRef
	}

	repoDigestRef := imageRepository(imageRef) + "@" + digest
	if localRef, exists := l.byRepoDigestRef[repoDigestRef]; exists {
		return localRef
	}

	return imageRef
}

func stripDigestFromImageRef(imageRef string) string {
	baseRef, _, found := strings.Cut(imageRef, "@")
	if found {
		return baseRef
	}

	return imageRef
}

func imageDigest(imageRef string) string {
	_, digest, found := strings.Cut(imageRef, "@")
	if found {
		return digest
	}

	return ""
}

func imageRepository(imageRef string) string {
	baseRef := stripDigestFromImageRef(imageRef)
	lastSlash := strings.LastIndex(baseRef, "/")

	lastColon := strings.LastIndex(baseRef, ":")
	if lastColon > lastSlash {
		return baseRef[:lastColon]
	}

	return baseRef
}

func normalizeImageRefs(imageRefs []string) []string {
	normalized := make([]string, 0, len(imageRefs))

	for _, imageRef := range imageRefs {
		normalized = append(normalized, NormalizeImageRef(imageRef))
	}

	return normalized
}

func mergeRepairImages(primary []string, secondary []string) []string {
	covered := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))

	add := func(imageRefs []string) {
		for _, imageRef := range imageRefs {
			if _, exists := covered[imageRef]; exists {
				continue
			}

			merged = append(merged, imageRef)
			for _, candidate := range repairPullCandidates(imageRef) {
				covered[candidate] = struct{}{}
			}
		}
	}

	add(primary)
	add(secondary)

	return merged
}

// exportImagesFromNode exports images from a node's containerd to the host filesystem.
func (e *Exporter) exportImagesFromNode(
	ctx context.Context,
	nodeName string,
	outputPath string,
	images []string,
	tmpBasePath string,
	requestedImages []string,
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
		requestedImages,
	)
	if exportErr != nil {
		// Fall back to exporting images one-by-one, skipping failures
		// This handles cases where some images have incomplete manifests
		// (e.g., multi-arch images where not all platform layers were pulled)
		successfulImages, failedImages := e.exportImagesOneByOne(
			ctx,
			nodeName,
			tmpPath,
			platform,
			exportImages,
		)
		if len(successfulImages) == 0 {
			return fmt.Errorf(
				"ctr export failed for all images during individual export attempts (initial bulk export error: %w)",
				exportErr,
			)
		}

		// Report failed images to stderr
		if len(failedImages) > 0 {
			fmt.Fprintf(
				os.Stderr,
				"warning: failed to export %d image(s): %s\n",
				len(failedImages),
				strings.Join(failedImages, ", "),
			)
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

func (e *Exporter) tryExportImagesWithRepair(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
	requestedImages []string,
) ([]string, error) {
	exportErr := e.tryExportImages(ctx, nodeName, tmpPath, platform, images)
	if exportErr == nil || len(requestedImages) == 0 || !isMissingContentError(exportErr) {
		return images, exportErr
	}

	repairImages := mergeRepairImages(
		e.resolveSpecifiedImages(ctx, nodeName, requestedImages),
		requestedImages,
	)
	e.refreshImageContent(ctx, nodeName, platform, repairImages)

	refreshedImages := e.resolveSpecifiedImages(ctx, nodeName, requestedImages)

	return refreshedImages, e.tryExportImages(
		ctx,
		nodeName,
		tmpPath,
		platform,
		refreshedImages,
	)
}

func (e *Exporter) refreshImageContent(
	ctx context.Context,
	nodeName string,
	platform string,
	imageRefs []string,
) {
	for _, imageRef := range imageRefs {
		e.ensureImageContent(ctx, nodeName, platform, imageRef)
	}
}

func repairPullCandidates(imageRef string) []string {
	candidates := []string{imageRef}
	if imageDigest(imageRef) == "" {
		return candidates
	}

	baseRef := stripDigestFromImageRef(imageRef)
	if baseRef == imageRepository(imageRef) {
		return candidates
	}

	candidates = append(candidates, baseRef)

	normalizedBaseRef := NormalizeImageRef(baseRef)
	if normalizedBaseRef != baseRef {
		candidates = append(candidates, normalizedBaseRef)
	}

	return candidates
}

func isMissingContentError(err error) bool {
	if err == nil {
		return false
	}

	errText := err.Error()

	return strings.Contains(errText, "failed to get reader: content digest") &&
		strings.Contains(errText, "not found")
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

// ensureImageContent pulls a single image inside the node to ensure all
// content blobs (including the manifest index) are present in containerd's
// content store. CRI-pulled multi-arch images often lack the manifest index
// content blob, causing "content digest not found" errors during ctr export.
func (e *Exporter) ensureImageContent(
	ctx context.Context,
	nodeName string,
	platform string,
	image string,
) {
	// Cap the pull with a deadline so a slow or unreachable registry cannot
	// stall the export indefinitely. The operation is best-effort: if the
	// pull times out the image may still export (e.g., content was
	// pre-loaded in an air-gapped environment), or it will simply be added
	// to the failed list by the caller.
	pullCtx, cancel := context.WithTimeout(ctx, contentPullTimeout)
	defer cancel()

	for _, candidate := range repairPullCandidates(image) {
		// Best-effort: ignore errors since the image may still be exportable
		// even if the pull fails (e.g., in air-gapped environments where
		// the content was pre-loaded).
		_, _ = e.executor.ExecInContainer(
			pullCtx,
			nodeName,
			buildCtrPullCommand(platform, candidate),
		)
	}
}

// exportImagesOneByOne exports each image individually, returning the list of
// images that can be successfully exported along with the list that failed.
// When an image fails to export with a "content digest not found" error,
// it attempts to pull the image content (to fix missing manifest index blobs
// from CRI pulls) and retries the export once.
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

			continue
		}

		// Export failed — if the error indicates missing content blobs
		// (e.g., manifest index not stored by CRI), pull the image to
		// repopulate the content store, then retry export once.
		if isMissingContentError(err) {
			e.ensureImageContent(ctx, nodeName, platform, image)

			err = e.tryExportImages(ctx, nodeName, tmpPath, platform, []string{image})
		}

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

func buildCtrPullCommand(platform string, imageRef string) []string {
	return []string{
		"ctr",
		"--namespace=k8s.io",
		"images",
		"pull",
		"--platform",
		platform,
		imageRef,
	}
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
	if distribution == v1alpha1.DistributionK3s {
		return dockerprovider.LabelSchemeK3d
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

	var bestNode provider.NodeInfo

	bestPriority := rolePriorityUnselectedStart

	for _, node := range nodes {
		// Skip helper containers without containerd
		if isHelperContainer(node.Role) {
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
	if distribution == v1alpha1.DistributionVanilla {
		return tmpPathRoot // Kind has tmpfs on /tmp
	}

	return tmpPathTmp
}
