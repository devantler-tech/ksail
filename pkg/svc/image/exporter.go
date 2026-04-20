package image

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
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
		return e.resolveSpecifiedImages(ctx, nodeName, opts.Images), nil
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

// resolveSpecifiedImages normalizes user-provided image refs and, when possible,
// resolves them to the exact refs already present in the node's image store.
// This keeps shorthand refs working while preferring the precise local tag@digest
// references returned by `ctr images list -q`.
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
	if existing, exists := l.byBaseRef[baseRef]; !exists ||
		preferBaseRef(existing, imageRef) {
		l.byBaseRef[baseRef] = imageRef
	}

	normalizedBaseRef := NormalizeImageRef(baseRef)
	if existing, exists := l.byBaseRef[normalizedBaseRef]; !exists ||
		preferBaseRef(existing, imageRef) {
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

func preferBaseRef(existingRef string, candidateRef string) bool {
	return imageDigest(existingRef) != "" && imageDigest(candidateRef) == ""
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

func (e *Exporter) tryExportImagesWithRepair(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
	repairImages []string,
	resolveImages []string,
) ([]string, error) {
	exportErr := e.tryExportImages(ctx, nodeName, tmpPath, platform, images)
	if exportErr == nil || len(repairImages) == 0 || !isMissingContentError(exportErr) {
		return images, exportErr
	}

	successfulRepairRefs, refreshErr := e.refreshImageContent(ctx, nodeName, platform, repairImages)
	if refreshErr != nil {
		return images, exportErr
	}

	refreshedImages := images
	if len(resolveImages) > 0 {
		refreshedImages = e.resolveSpecifiedImages(ctx, nodeName, resolveImages)
	}

	refreshedImages = preferSuccessfulRepairRefs(refreshedImages, successfulRepairRefs)

	return refreshedImages, e.tryExportImages(
		ctx,
		nodeName,
		tmpPath,
		platform,
		refreshedImages,
	)
}

func (e *Exporter) fallbackExportImages(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	images []string,
	bulkErr error,
) error {
	successfulImages, failedImages := e.exportImagesOneByOne(
		ctx,
		nodeName,
		tmpPath,
		platform,
		images,
	)
	if len(successfulImages) == 0 {
		return fmt.Errorf(
			"ctr export failed for all images during individual export attempts (initial bulk export error: %w)",
			bulkErr,
		)
	}

	logFailedImageExports(failedImages)

	err := e.tryExportImages(ctx, nodeName, tmpPath, platform, successfulImages)
	if err != nil {
		return fmt.Errorf("ctr export failed: %w", err)
	}

	return nil
}

func logFailedImageExports(failedImages []string) {
	if len(failedImages) == 0 {
		return
	}

	fmt.Fprintf(
		os.Stderr,
		"warning: failed to export %d image(s): %s\n",
		len(failedImages),
		strings.Join(failedImages, ", "),
	)
}

func (e *Exporter) refreshImageContent(
	ctx context.Context,
	nodeName string,
	platform string,
	imageRefs []string,
) (map[string]string, error) {
	successfulRefs := make(map[string]string, len(imageRefs))

	var errs []error

	for _, imageRef := range imageRefs {
		successfulRef, err := e.refreshSingleImageContent(ctx, nodeName, platform, imageRef)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		successfulRefs[imageRef] = successfulRef
	}

	if len(errs) > 0 {
		return successfulRefs, errors.Join(errs...)
	}

	return successfulRefs, nil
}

func (e *Exporter) refreshSingleImageContent(
	ctx context.Context,
	nodeName string,
	platform string,
	imageRef string,
) (string, error) {
	successfulRef := ""

	var errs []error

	for _, candidate := range repairPullCandidates(imageRef) {
		// Remove the image reference before pulling. When containerd is configured
		// with discard_unpacked_layers=true (Kind's default), a re-pull of an image
		// whose snapshot already exists is a no-op — containerd skips re-downloading
		// the content blobs because the snapshot is still live. Removing the image ref
		// first forces a genuinely fresh pull that downloads all content blobs.
		// Running containers are unaffected: their snapshot leases keep the overlayfs
		// mounts alive regardless of whether the image ref exists.
		_, _ = e.executor.ExecInContainer(
			ctx,
			nodeName,
			buildCtrImagesRmCommand(candidate),
		)

		_, err := e.executor.ExecInContainer(
			ctx,
			nodeName,
			buildCtrPullCommand(platform, candidate),
		)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", candidate, err))

			continue
		}

		// Content fetch re-downloads blobs into the content store without unpacking,
		// keeping them present for the subsequent ctr export call even when
		// discard_unpacked_layers=true would otherwise discard them after unpacking.
		_, _ = e.executor.ExecInContainer(
			ctx,
			nodeName,
			buildCtrContentFetchCommand(platform, candidate),
		)

		if successfulRef == "" || preferBaseRef(successfulRef, candidate) {
			successfulRef = candidate
		}
	}

	if successfulRef != "" {
		return successfulRef, nil
	}

	return "", errors.Join(errs...)
}

func buildCtrImagesRmCommand(imageRef string) []string {
	return []string{
		"ctr",
		"--namespace=k8s.io",
		"images",
		"rm",
		imageRef,
	}
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

func buildCtrContentFetchCommand(platform string, imageRef string) []string {
	return []string{
		"ctr",
		"--namespace=k8s.io",
		"content",
		"fetch",
		"--platform",
		platform,
		imageRef,
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

func (e *Exporter) tryExportSingleImageWithRepair(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	platform string,
	image string,
) (string, error) {
	err := e.tryExportImages(ctx, nodeName, tmpPath, platform, []string{image})
	if err == nil || !isMissingContentError(err) {
		return image, err
	}

	successfulRef, refreshErr := e.refreshSingleImageContent(ctx, nodeName, platform, image)
	if refreshErr != nil {
		return image, err
	}

	return successfulRef, e.tryExportImages(
		ctx,
		nodeName,
		tmpPath,
		platform,
		[]string{successfulRef},
	)
}

func preferSuccessfulRepairRefs(
	images []string,
	successfulRepairRefs map[string]string,
) []string {
	if len(successfulRepairRefs) == 0 {
		return images
	}

	preferred := make([]string, 0, len(images))
	for _, image := range images {
		if successfulRef, exists := successfulRepairRefs[image]; exists {
			preferred = append(preferred, successfulRef)

			continue
		}

		preferred = append(preferred, image)
	}

	return preferred
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
		successfulRef, err := e.tryExportSingleImageWithRepair(
			ctx,
			nodeName,
			tmpPath,
			platform,
			image,
		)
		if err == nil {
			successful = append(successful, successfulRef)
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

// blobSHA256Prefix is the path prefix for SHA256 blobs in OCI image layout tar archives.
const blobSHA256Prefix = "blobs/sha256/"

// sha256HexLength is the expected length of a SHA256 hex-encoded digest.
const sha256HexLength = 64

// ValidateExportedTar validates the integrity of SHA256 blobs in an OCI image tar archive.
// For each blob file at blobs/sha256/<hex>, it computes the SHA256 hash of the content
// and verifies it matches the expected digest from the filename.
//
// Returns nil if:
//   - The first header cannot be parsed (not a tar archive, skip validation)
//   - The tar contains no SHA256 blobs
//   - All blobs have correct digests
//
// Returns ErrBlobIntegrityFailed if:
//   - Any blob's content does not match its expected digest (truncated or corrupted export)
//   - The tar stream is corrupted mid-archive after at least one header was successfully read
func ValidateExportedTar(tarPath string) error {
	tarFile, err := os.Open(tarPath) //nolint:gosec // Path is from internal code
	if err != nil {
		return fmt.Errorf("failed to open exported tar for validation: %w", err)
	}

	defer func() { _ = tarFile.Close() }()

	tarReader := tar.NewReader(tarFile)
	entriesSeen := false

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			if !entriesSeen {
				// First header failed to parse — not a tar archive, skip validation.
				return nil
			}

			return fmt.Errorf(
				"%w: tar archive is truncated or corrupted: %w",
				ErrBlobIntegrityFailed,
				err,
			)
		}

		entriesSeen = true

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if !strings.HasPrefix(header.Name, blobSHA256Prefix) {
			continue
		}

		expectedDigest := strings.TrimPrefix(header.Name, blobSHA256Prefix)
		if len(expectedDigest) != sha256HexLength {
			continue
		}

		err = validateBlobDigest(header, tarReader)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateBlobDigest reads a single OCI blob from tarReader and verifies that its SHA256
// digest matches the expected value encoded in the blob's tar entry name.
func validateBlobDigest(header *tar.Header, tarReader *tar.Reader) error {
	expectedDigest := strings.TrimPrefix(header.Name, blobSHA256Prefix)
	hasher := sha256.New()

	bytesRead, copyErr := io.Copy(hasher, io.LimitReader(tarReader, header.Size))
	if copyErr != nil {
		return fmt.Errorf(
			"%w: failed to read blob %s: %w",
			ErrBlobIntegrityFailed,
			header.Name,
			copyErr,
		)
	}

	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != expectedDigest {
		return fmt.Errorf(
			"%w: blob %s: computed SHA256 %s (read %d of %d bytes)",
			ErrBlobIntegrityFailed,
			header.Name,
			actualDigest,
			bytesRead,
			header.Size,
		)
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
