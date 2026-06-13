package image

import (
	"context"
	"fmt"
	"strings"
)

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
	cmd := []string{ctrCommand, ctrNamespaceArg, ctrImages, "list", "-q"}

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
