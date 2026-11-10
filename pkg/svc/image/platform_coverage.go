package image

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ctrdimages "github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	// archiveIndexName is the OCI image layout index at the root of an image archive.
	archiveIndexName = "index.json"
	// maxArchiveIndexBytes bounds the index documents read to check platform coverage.
	maxArchiveIndexBytes = 4 << 20
	// containerdImageNameAnnotation names an image in a containerd-exported archive.
	containerdImageNameAnnotation = "io.containerd.image.name"
)

// ErrArchiveMissingPlatform is returned when an archive cannot serve a node's platform.
var ErrArchiveMissingPlatform = errors.New("image archive has no content for the node platform")

// imagePlatformCoverage is what an archive holds for one named multi-platform image: the
// platforms its index advertises, and the subset whose manifests the archive carries.
type imagePlatformCoverage struct {
	name       string
	advertised []ocispec.Platform
	carried    []ocispec.Platform
}

// servable reports whether the archive carries a manifest a node of platform can run.
func (c imagePlatformCoverage) servable(platform ocispec.Platform) bool {
	return slices.ContainsFunc(c.carried, platforms.Only(platform).Match)
}

// partial reports whether the index advertises a platform the archive carries no manifest for.
func (c imagePlatformCoverage) partial() bool {
	return len(c.carried) < len(c.advertised)
}

// readArchivePlatformCoverage lists, for each named multi-platform image in an OCI image archive,
// the platforms its index advertises and the ones the archive carries. An export keeps only its
// node's platform but keeps the full index, so an archive can advertise platforms it cannot
// serve. Single-platform images and archives without an OCI index are not listed.
func readArchivePlatformCoverage(path string) ([]imagePlatformCoverage, error) {
	blobs, index, err := readArchiveIndex(path)
	if err != nil || index == nil {
		return nil, err
	}

	named := namedIndexDescriptors(index, blobs)

	wanted := make(map[string]bool, len(named))
	for _, descriptor := range named {
		wanted[descriptor.Digest.Encoded()] = true
	}

	documents, err := readArchiveBlobs(path, wanted)
	if err != nil {
		return nil, err
	}

	coverage := make([]imagePlatformCoverage, 0, len(named))

	for _, descriptor := range named {
		entry, err := indexPlatformCoverage(
			archiveImageName(descriptor), documents[descriptor.Digest.Encoded()], blobs,
		)
		if err != nil {
			return nil, err
		}

		if len(entry.advertised) > 0 {
			coverage = append(coverage, entry)
		}
	}

	return coverage, nil
}

// namedIndexDescriptors returns the archive's named image indexes whose blob it holds.
func namedIndexDescriptors(index *ocispec.Index, blobs map[string]bool) []ocispec.Descriptor {
	var named []ocispec.Descriptor

	for _, descriptor := range index.Manifests {
		if archiveImageName(descriptor) != "" && ctrdimages.IsIndexType(descriptor.MediaType) &&
			blobs[descriptor.Digest.Encoded()] {
			named = append(named, descriptor)
		}
	}

	return named
}

// indexPlatformCoverage lists the platform manifests one image index advertises, and the ones
// whose manifest blob the archive holds.
func indexPlatformCoverage(
	name string,
	document []byte,
	blobs map[string]bool,
) (imagePlatformCoverage, error) {
	var child ocispec.Index

	err := json.Unmarshal(document, &child)
	if err != nil {
		return imagePlatformCoverage{}, fmt.Errorf("parse image index of %s: %w", name, err)
	}

	entry := imagePlatformCoverage{name: name}

	for _, manifest := range child.Manifests {
		if manifest.Platform == nil || !ctrdimages.IsManifestType(manifest.MediaType) ||
			isAttestationPlatform(*manifest.Platform) {
			continue
		}

		entry.advertised = append(entry.advertised, *manifest.Platform)
		if blobs[manifest.Digest.Encoded()] {
			entry.carried = append(entry.carried, *manifest.Platform)
		}
	}

	return entry, nil
}

// unservedImages names the images an archive cannot serve on platform.
func unservedImages(coverage []imagePlatformCoverage, platform ocispec.Platform) []string {
	var names []string

	for _, entry := range coverage {
		if !entry.servable(platform) {
			names = append(names, entry.name)
		}
	}

	return names
}

// requireArchivePlatform refuses a node whose platform the archive cannot serve for every
// multi-platform image, naming the images, before anything is copied to the node.
func requireArchivePlatform(coverage []imagePlatformCoverage, nodeName, nodePlatform string) error {
	platform, err := platforms.Parse(nodePlatform)
	if err != nil {
		return fmt.Errorf("parse node platform: %w", err)
	}

	names := unservedImages(coverage, platform)
	if len(names) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: node %s runs %s, but the archive carries no %s content for %s; "+
			"export these images from a %s node, or import the archive on nodes of the "+
			"platform it was exported from",
		ErrArchiveMissingPlatform, nodeName, nodePlatform, nodePlatform,
		strings.Join(names, ", "), nodePlatform,
	)
}

// isAttestationPlatform reports the unknown/unknown entries BuildKit adds for attestations.
func isAttestationPlatform(platform ocispec.Platform) bool {
	return platform.OS == "unknown" && platform.Architecture == "unknown"
}

func archiveImageName(descriptor ocispec.Descriptor) string {
	if name := descriptor.Annotations[containerdImageNameAnnotation]; name != "" {
		return name
	}

	return descriptor.Annotations[ocispec.AnnotationRefName]
}

// readArchiveIndex lists the archive's blobs and parses its index.json. It returns a nil index
// for an input that is not an OCI image layout archive.
func readArchiveIndex(path string) (map[string]bool, *ocispec.Index, error) {
	blobs := make(map[string]bool)

	var indexData []byte

	err := walkArchive(path, func(header *tar.Header, reader io.Reader) error {
		name := strings.TrimPrefix(header.Name, "./")
		if digest, found := strings.CutPrefix(name, blobSHA256Prefix); found {
			blobs[digest] = true

			return nil
		}

		if name != archiveIndexName {
			return nil
		}

		data, err := readBounded(reader)
		indexData = data

		return err
	})
	if err != nil || indexData == nil {
		return nil, nil, err
	}

	var index ocispec.Index

	err = json.Unmarshal(indexData, &index)
	if err != nil {
		return nil, nil, fmt.Errorf("parse image archive index: %w", err)
	}

	return blobs, &index, nil
}

// readArchiveBlobs returns the content of the wanted blobs, keyed by digest.
func readArchiveBlobs(path string, wanted map[string]bool) (map[string][]byte, error) {
	documents := make(map[string][]byte, len(wanted))
	if len(wanted) == 0 {
		return documents, nil
	}

	err := walkArchive(path, func(header *tar.Header, reader io.Reader) error {
		digest, found := strings.CutPrefix(strings.TrimPrefix(header.Name, "./"), blobSHA256Prefix)
		if !found || !wanted[digest] {
			return nil
		}

		data, err := readBounded(reader)
		documents[digest] = data

		return err
	})

	return documents, err
}

// walkArchive calls visit for every regular file in the tar archive at path. An input whose first
// header is not a tar header is treated as an empty archive.
func walkArchive(path string, visit func(*tar.Header, io.Reader) error) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open image archive: %w", err)
	}

	defer func() { _ = file.Close() }()

	reader := tar.NewReader(file)

	for entries := 0; ; entries++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) || (err != nil && entries == 0) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read image archive: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		err = visit(header, reader)
		if err != nil {
			return err
		}
	}
}

func readBounded(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxArchiveIndexBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read image archive index: %w", err)
	}

	if len(data) > maxArchiveIndexBytes {
		return nil, fmt.Errorf(
			"image archive index exceeds %d bytes: %w",
			maxArchiveIndexBytes,
			ErrIncompleteExport,
		)
	}

	return data, nil
}

// reportPartialPlatforms warns, after an export, about multi-platform images whose archive keeps
// the full index but carries content only for the exporting nodes' platform, so the user learns
// before importing elsewhere which images other platforms cannot use. The archive itself is
// already complete for its own platform, so a failed read only skips the notice.
func reportPartialPlatforms(ctx context.Context, path string) {
	coverage, err := readArchivePlatformCoverage(path)
	if err != nil {
		slog.DebugContext(ctx, "Skipped image archive platform notice", "error", err)

		return
	}

	names, carried := partialPlatformImages(coverage)
	if len(names) == 0 {
		return
	}

	slog.WarnContext(ctx,
		"Image archive carries only some platforms of multi-platform images; "+
			"import it on nodes of the carried platform",
		"carried", strings.Join(carried, ","),
		"images", strings.Join(names, ","),
	)
}

// partialPlatformImages names the images whose index advertises a platform the archive does not
// carry, and lists the distinct platforms the archive does carry for them.
func partialPlatformImages(coverage []imagePlatformCoverage) ([]string, []string) {
	var names, carried []string

	for _, entry := range coverage {
		if !entry.partial() {
			continue
		}

		names = append(names, entry.name)

		for _, platform := range entry.carried {
			carried = append(carried, platforms.Format(platform))
		}
	}

	slices.Sort(carried)

	return names, slices.Compact(carried)
}
