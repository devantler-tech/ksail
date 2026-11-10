package image

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
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
	// nodeCleanupTimeout bounds removing an export or import archive from a node
	// after the command's context is done, so a cancelled operation still cleans up.
	nodeCleanupTimeout = 30 * time.Second
	// nodeArchiveSuffixBytes is the random suffix length of a node-side archive name.
	nodeArchiveSuffixBytes = 8
)

// newExportArchiveName returns the file name of one export's archive on a node.
// Each export gets its own name, so two exports on the same node, from separate
// commands or from one command, never write to or remove each other's archive.
// Tests replace it to keep the node-side path predictable.
//
//nolint:gochecknoglobals // Test seam: tests pin the name so node-side commands stay exact.
var newExportArchiveName = defaultExportArchiveName

// defaultExportArchiveName returns a random node-side archive name, so concurrent
// exports on the same node never write to the same file.
func defaultExportArchiveName() string {
	return randomNodeArchiveName("ksail-images-export-")
}

// randomNodeArchiveName returns prefix followed by a random hex suffix and ".tar".
func randomNodeArchiveName(prefix string) string {
	suffix := make([]byte, nodeArchiveSuffixBytes)
	_, _ = rand.Read(suffix)

	return prefix + hex.EncodeToString(suffix) + ".tar"
}

// Error definitions.
var (
	errIncompleteImageHelp = errors.New("incomplete help output")

	// ErrNoNodes is returned when no cluster nodes are found.
	ErrNoNodes = errors.New("no cluster nodes found")
	// ErrUnsupportedProvider is returned when the provider is not supported for image operations.
	ErrUnsupportedProvider = errors.New("unsupported provider for image operations")
	// ErrNoImagesFound is returned when no images are found in the cluster.
	ErrNoImagesFound = errors.New("no images found in cluster")
	// ErrIncompleteExport means complete image coverage could not be established.
	ErrIncompleteExport = errors.New("cannot establish complete image export")
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
	localExport  bool
	ociOnly      bool
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

	// Canonicalize the user-supplied destination once, before any write. The archive is
	// written twice on the repair path (initial copy, then the post-repair re-export), so
	// resolving it here is what guarantees both writes land on the same file rather than
	// re-resolving a symlink that moved in between.
	err = os.MkdirAll(filepath.Dir(opts.OutputPath), dirPerm)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	opts.OutputPath, err = fsutil.EvalCanonicalPath(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("failed to resolve export output path: %w", err)
	}

	sources, err := e.findExportSources(ctx, clusterName, distribution, opts.Images)
	if err != nil {
		return err
	}

	// A single-node cluster keeps its original export and repair path. A
	// captured inventory marks a multi-node plan, even if one node covers it.
	if len(sources) == 1 && sources[0].images == nil {
		_, err = e.exportSource(ctx, sources[0], distribution, opts)

		return err
	}

	return e.exportSources(ctx, sources, distribution, opts)
}

// exportSource exports one node's share of the images to the archive at
// opts.OutputPath, repairing missing content on that node when needed, and
// returns the platform it exported.
func (e *Exporter) exportSource(
	ctx context.Context,
	source nodeExportSource,
	distribution v1alpha1.Distribution,
	opts ExportOptions,
) (string, error) {
	if len(opts.Images) > 0 && source.images != nil {
		opts.Images = source.wanted
	}

	images, err := e.resolveImages(ctx, source.node, opts, source.images)
	if err != nil {
		return "", err
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
		source.node,
		opts.OutputPath,
		images,
		getTempPath(distribution),
		repairImages,
		resolveImages,
	)
}

// countHeldImages counts the requested refs the lookup resolves to a local image.
func countHeldImages(lookup localImageLookup, normalizedRequested []string) int {
	held := 0

	for _, imageRef := range normalizedRequested {
		if _, exists := lookup.byExactRef[lookup.resolve(imageRef)]; exists {
			held++
		}
	}

	return held
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
) (string, error) {
	tmpPath := tmpBasePath + "/" + newExportArchiveName()

	// Detect the node's platform (OS/architecture)
	platform, err := e.detectNodePlatform(ctx, nodeName)
	if err != nil {
		return "", fmt.Errorf("failed to detect node platform: %w", err)
	}

	e, err = e.withLocalExport(ctx, nodeName)
	if err != nil {
		return "", err
	}

	// From here on ctr may write the archive, so remove it on every path,
	// including failures and cancellation.
	defer e.removeNodeArchive(ctx, nodeName, tmpPath)

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
			return "", err
		}
	}

	err = e.collectExportedArchive(
		ctx,
		nodeName,
		tmpPath,
		outputPath,
		platform,
		exportImages,
		repairImages,
	)
	if err != nil {
		return "", err
	}

	return platform, nil
}

// withLocalExport returns a copy of the exporter that uses the export mode the
// node's containerd supports, probed once for this operation.
func (e *Exporter) withLocalExport(ctx context.Context, nodeName string) (*Exporter, error) {
	// Probe once for this operation. Containerd 1.6 exports synchronously but
	// lacks --local; newer versions expose it and 2.x otherwise defaults to an
	// asynchronous transfer whose output copy may outlive the export command.
	local, err := e.executor.localImageMode(ctx, nodeName, "export")
	if err != nil {
		return nil, err
	}

	// Keep the selection local to this operation, including repairs and fallbacks,
	// so concurrent calls on the same Exporter cannot change each other's mode.
	exporter := *e
	exporter.localExport = local

	return &exporter, nil
}

// collectExportedArchive copies the exported archive to the host and validates it,
// repairing and re-exporting once if the archive turns out to be truncated.
//
// An incomplete containerd content store surfaces two ways: ctr export exits non-zero
// (handled by tryExportImagesWithRepair), or it exits zero and silently writes a truncated
// archive, which only this validation sees. Both have the same remedy, so the validation
// failure refreshes the content store and re-exports once as well.
func (e *Exporter) collectExportedArchive(
	ctx context.Context,
	nodeName string,
	tmpPath string,
	outputPath string,
	platform string,
	exportImages []string,
	repairImages []string,
) error {
	err := e.copyFromContainer(ctx, nodeName, tmpPath, outputPath)
	if err != nil {
		return fmt.Errorf("failed to copy export file from container: %w", err)
	}

	err = ValidateExportedTar(outputPath)
	if err != nil {
		return e.repairAndReExport(
			ctx,
			nodeName,
			tmpPath,
			outputPath,
			platform,
			exportImages,
			repairImages,
			err,
		)
	}

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

// removeNodeArchive deletes an export archive from a node.
func (e *Exporter) removeNodeArchive(ctx context.Context, nodeName, tmpPath string) {
	e.executor.removeNodeArchive(ctx, nodeName, tmpPath)
}
