package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/containerd/containerd/v2/core/content"
	ctrdimages "github.com/containerd/containerd/v2/core/images"
	ctrdarchive "github.com/containerd/containerd/v2/core/images/archive"
	"github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type imageArchiveMerger struct {
	store     content.Store
	targets   map[string]ocispec.Descriptor
	names     []string
	platforms []ocispec.Platform
}

// newImageArchiveMerger opens the staging content store shared by the node archives.
func newImageArchiveMerger(directory string) (*imageArchiveMerger, error) {
	store, err := local.NewStore(directory)
	if err != nil {
		return nil, fmt.Errorf("initialize private image content store: %w", err)
	}

	return &imageArchiveMerger{store: store, targets: make(map[string]ocispec.Descriptor)}, nil
}

// Containerd owns format conversion and content-addressed blob ingestion.
// Validate each expected name against its actual source node's platform before
// allowing incomplete foreign-platform variants to be omitted on final export.
func (m *imageArchiveMerger) add(
	ctx context.Context,
	reader io.Reader,
	refs []string,
	sourcePlatform string,
) error {
	platform, err := platforms.Parse(sourcePlatform)
	if err != nil {
		return fmt.Errorf("parse source image platform: %w", err)
	}

	index, err := ctrdarchive.ImportIndex(ctx, m.store, reader)
	if err != nil {
		return fmt.Errorf("import node image archive: %w", err)
	}

	data, err := content.ReadBlob(ctx, m.store, index)
	if err != nil {
		return fmt.Errorf("read node archive index: %w", err)
	}

	byName, err := archiveDescriptorsByName(data)
	if err != nil {
		return err
	}

	err = requireSamePlatform(m.platforms, platform)
	if err != nil {
		return err
	}

	// Validate every reference before registering any, so a node archive that
	// fails part-way leaves the merger unchanged for a retry on another node.
	targets, err := m.validTargets(ctx, byName, refs, platform)
	if err != nil {
		return err
	}

	for _, target := range targets {
		if _, found := m.targets[target.name]; !found {
			m.targets[target.name] = target.descriptor
			m.names = append(m.names, target.name)
		}
	}

	if len(m.platforms) == 0 {
		m.platforms = append(m.platforms, platform)
	}

	return nil
}

// namedTarget is one archive entry an export will publish.
type namedTarget struct {
	name       string
	descriptor ocispec.Descriptor
}

// validTargets resolves each reference to the archive entry that serves it and
// checks its content, without changing the merger.
func (m *imageArchiveMerger) validTargets(
	ctx context.Context,
	byName map[string]ocispec.Descriptor,
	refs []string,
	platform ocispec.Platform,
) ([]namedTarget, error) {
	targets := make([]namedTarget, 0, len(refs))

	for _, ref := range refs {
		name, descriptor, found := exportedDescriptor(byName, ref)
		if !found {
			return nil, fmt.Errorf("%w: source archive omits %s", ErrIncompleteExport, ref)
		}

		err := m.checkTarget(ctx, name, descriptor, platform)
		if err != nil {
			return nil, err
		}

		targets = append(targets, namedTarget{name: name, descriptor: descriptor})
	}

	return targets, nil
}

// requireSamePlatform refuses a node whose platform differs from the nodes
// already merged. Each node exports only its own variant, so a combined archive
// is complete for exactly one platform; Docker nodes share their host's
// architecture, and a mixed set would advertise a platform it cannot serve.
func requireSamePlatform(merged []ocispec.Platform, platform ocispec.Platform) error {
	if len(merged) == 0 || platforms.OnlyStrict(merged[0]).Match(platform) {
		return nil
	}

	return fmt.Errorf(
		"%w: nodes run different platforms (%s and %s)",
		ErrIncompleteExport,
		platforms.Format(merged[0]),
		platforms.Format(platform),
	)
}

// exportedDescriptor finds the archive entry for ref. A content repair may
// re-pull a digest-qualified name by its tag, so the archive can hold one of
// the ref's repair candidates instead; the entry is published under the name
// the archive actually holds.
func exportedDescriptor(
	byName map[string]ocispec.Descriptor, ref string,
) (string, ocispec.Descriptor, bool) {
	for _, candidate := range repairPullCandidates(NormalizeImageRef(ref)) {
		name := NormalizeImageRef(candidate)
		if descriptor, found := byName[name]; found {
			return name, descriptor, true
		}
	}

	return "", ocispec.Descriptor{}, false
}

// archiveDescriptorsByName indexes named OCI entries by normalized reference.
// It refuses an index that assigns the same name to different digests.
func archiveDescriptorsByName(data []byte) (map[string]ocispec.Descriptor, error) {
	var manifest ocispec.Index

	err := json.Unmarshal(data, &manifest)
	if err != nil {
		return nil, fmt.Errorf("decode node archive index: %w", err)
	}

	byName := make(map[string]ocispec.Descriptor, len(manifest.Manifests))
	for _, descriptor := range manifest.Manifests {
		name := descriptor.Annotations[ctrdimages.AnnotationImageName]
		if name == "" {
			name = descriptor.Annotations[ocispec.AnnotationRefName]
		}

		if name == "" {
			continue
		}

		key := NormalizeImageRef(name)
		if existing, found := byName[key]; found && existing.Digest != descriptor.Digest {
			return nil, fmt.Errorf(
				"%w: conflicting content for image %s",
				ErrIncompleteExport,
				name,
			)
		}

		byName[key] = descriptor
	}

	return byName, nil
}

// checkTarget checks that the selected platform's content is complete for a
// reference. A name already registered must resolve to the same digest.
func (m *imageArchiveMerger) checkTarget(
	ctx context.Context, ref string, descriptor ocispec.Descriptor, platform ocispec.Platform,
) error {
	available, _, _, missing, err := ctrdimages.Check(
		ctx,
		m.store,
		descriptor,
		platforms.Only(platform),
	)
	if err != nil {
		return fmt.Errorf("check image %s: %w", ref, err)
	}

	if !available || len(missing) > 0 {
		return fmt.Errorf("%w: missing content for %s", ErrIncompleteExport, ref)
	}

	if existing, found := m.targets[ref]; found && existing.Digest != descriptor.Digest {
		return fmt.Errorf("%w: conflicting content for %s", ErrIncompleteExport, ref)
	}

	return nil
}

// write emits a single OCI archive with one named entry per validated reference.
func (m *imageArchiveMerger) write(ctx context.Context, writer io.Writer) error {
	defaults := []ctrdarchive.ExportOpt{
		ctrdarchive.WithSkipDockerManifest(),
		ctrdarchive.WithAllPlatforms(),
		ctrdarchive.WithSkipMissing(m.store),
		ctrdarchive.WithPlatform(platforms.Ordered(m.platforms...)),
	}
	options := make([]ctrdarchive.ExportOpt, 0, len(defaults)+len(m.names))

	options = append(options, defaults...)
	for _, name := range m.names {
		options = append(options, ctrdarchive.WithManifest(m.targets[name], name))
	}

	err := ctrdarchive.Export(ctx, m.store, writer, options...)
	if err != nil {
		return fmt.Errorf("merge complete image archives: %w", err)
	}

	return nil
}
