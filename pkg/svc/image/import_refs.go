package image

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// ErrImportStoredUnknownReference is returned when an import leaves a digest reference on a node
// that names none of the archive's images. A later export cannot find or pull such a reference.
var ErrImportStoredUnknownReference = errors.New(
	"image import stored a reference that names no image in the archive",
)

// archiveImageDigests is what an OCI image archive declares: the digest of its own index.json and
// the digests of the images that index lists. Only those images may gain a digest reference when
// the archive is imported.
type archiveImageDigests struct {
	index  string
	images map[string]bool
}

// readArchiveImageDigests reads the archive's index.json. It returns nil for an input that is not
// an OCI image layout archive, so the import keeps its existing behaviour for other inputs.
func readArchiveImageDigests(path string) (*archiveImageDigests, error) {
	var indexData []byte

	err := walkArchive(path, func(header *tar.Header, reader io.Reader) error {
		if strings.TrimPrefix(header.Name, "./") != archiveIndexName {
			return nil
		}

		data, err := readBounded(reader)
		indexData = data

		return err
	})
	if err != nil || indexData == nil {
		return nil, err
	}

	var index ocispec.Index

	err = json.Unmarshal(indexData, &index)
	if err != nil {
		return nil, fmt.Errorf("parse image archive index: %w", err)
	}

	sum := sha256.Sum256(indexData)
	digests := &archiveImageDigests{
		index:  "sha256:" + hex.EncodeToString(sum[:]),
		images: make(map[string]bool, len(index.Manifests)),
	}

	for _, descriptor := range index.Manifests {
		digests.images[descriptor.Digest.String()] = true
	}

	return digests, nil
}

// unknownReferences returns the references in after that were not in before and carry a digest the
// archive does not list as an image after "@". Named references without a digest, and digest
// references of the archive's images, are what an import is expected to add. Bare "sha256:"
// entries are image IDs the container runtime's image service records for the images it sees, so
// they are not judged here.
func (d *archiveImageDigests) unknownReferences(before []string, after []string) []string {
	existing := make(map[string]bool, len(before))
	for _, ref := range before {
		existing[ref] = true
	}

	var unknown []string

	for _, ref := range after {
		if existing[ref] {
			continue
		}

		digest := imageDigest(ref)
		if digest != "" && !d.images[digest] {
			unknown = append(unknown, ref)
		}
	}

	return unknown
}

// describe names the archive's own index when a reference stores it, so a failure says which
// content the reference points at.
func (d *archiveImageDigests) describe(refs []string) string {
	for _, ref := range refs {
		if imageDigest(ref) == d.index {
			return "the archive's own index " + d.index
		}
	}

	return "content outside the archive index " + d.index
}

// listNodeImageReferences lists every image reference in a node's containerd, including bare
// digest references.
func (e *ContainerExecutor) listNodeImageReferences(
	ctx context.Context,
	nodeName string,
) ([]string, error) {
	output, err := e.ExecInContainer(
		ctx, nodeName, []string{ctrCommand, ctrNamespaceArg, ctrImages, "list", "-q"},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list images in node %s: %w", nodeName, err)
	}

	var refs []string

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			refs = append(refs, line)
		}
	}

	return refs, nil
}

// requireKnownReferences fails when the import added a digest reference that names no image in
// the archive, naming the reference and the archive index it was imported from.
func (e *ContainerExecutor) requireKnownReferences(
	ctx context.Context,
	nodeName string,
	digests *archiveImageDigests,
	before []string,
) error {
	after, err := e.listNodeImageReferences(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("check imported image references: %w", err)
	}

	unknown := digests.unknownReferences(before, after)
	if len(unknown) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%w: node %s now holds %s, which points at %s",
		ErrImportStoredUnknownReference,
		nodeName,
		strings.Join(unknown, ", "),
		digests.describe(unknown),
	)
}
