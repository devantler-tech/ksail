package image

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ExtractImagesFromManifest extracts all container image references from rendered Kubernetes manifests.
// It parses YAML documents and extracts images from containers, initContainers, and ephemeralContainers.
//
// The extraction uses regex patterns to handle various YAML formats and indentation styles.
// Images are deduplicated before being returned.
func ExtractImagesFromManifest(manifest string) ([]string, error) {
	if manifest == "" {
		return nil, nil
	}

	seen := make(map[string]struct{})

	var images []string

	reader := strings.NewReader(manifest)

	images, err := extractImagesFromReader(reader, seen)
	if err != nil {
		return nil, fmt.Errorf("extract images: %w", err)
	}

	return images, nil
}

// extractImagesFromReader extracts images from a YAML reader.
func extractImagesFromReader(reader io.Reader, seen map[string]struct{}) ([]string, error) {
	var images []string

	// Pattern to match image: fields in YAML
	// Handles various formats:
	//   image: nginx:latest
	//   image: "nginx:latest"
	//   image: 'nginx:latest'
	//   - image: docker.io/library/nginx:1.25
	//   - image: ghcr.io/fluxcd/source-controller:v1.5.0
	//   image: nginx:1.25  # with comment
	//
	// The pattern matches:
	// - Optional leading whitespace and dash (for YAML list items)
	// - "image:" keyword
	// - Optional quotes (single or double)
	// - The image reference (captured)
	// - Optional trailing comment
	imagePattern := regexp.MustCompile(`^\s*-?\s*image:\s*["']?([^\s"'#]+)["']?\s*(?:#.*)?$`)

	scanner := bufio.NewScanner(reader)
	// Increase buffer size to handle very long lines in Helm-rendered CRDs
	// (e.g., Calico/Tigera CRDs can exceed the default 64 KiB token limit).
	const maxTokenSize = 1024 * 1024 // 1 MiB
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxTokenSize)

	for scanner.Scan() {
		line := scanner.Text()

		matches := imagePattern.FindStringSubmatch(line)
		if len(matches) > 1 {
			img := strings.TrimSpace(matches[1])
			// Skip empty images or template variables
			if img == "" || strings.HasPrefix(img, "{{") {
				continue
			}

			// Normalize image reference
			img = NormalizeImageRef(img)

			if _, exists := seen[img]; !exists {
				seen[img] = struct{}{}
				images = append(images, img)
			}
		}
	}

	err := scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("scan manifest: %w", err)
	}

	return images, nil
}

// isPortNumber checks if a string consists only of digits (i.e., could be a port number).
func isPortNumber(s string) bool {
	if len(s) == 0 {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// stripTagFromRef strips a version tag (not a port) from a reference for registry detection.
// Returns the base ref without the tag, e.g., "nginx:1.25" -> "nginx".
func stripTagFromRef(ref string) string {
	before, after, ok := strings.Cut(ref, ":")
	if !ok {
		return ref
	}

	// If after colon is all digits, it's a port; otherwise it's a tag
	if isPortNumber(after) {
		return ref // Keep port for localhost:5000 detection
	}

	return before
}

// NormalizeImageRef normalizes an image reference to a fully qualified form.
// - Adds "docker.io/library/" prefix for images without registry and namespace
// - Adds "docker.io/" prefix for images without registry but with namespace
// - Adds ":latest" tag if no tag is specified (not for @sha256 digests).
func NormalizeImageRef(ref string) string {
	// Parse the image reference to determine if it has a registry
	// A registry is present if the first component (before first /) contains:
	// - A dot (.) like docker.io, ghcr.io, registry.k8s.io
	// - A colon (:) for port like localhost:5000 (but NOT for tag like nginx:1.25)
	// - Is exactly "localhost"
	parts := strings.Split(ref, "/")
	firstPart := parts[0]

	// For registry detection, we need to check the part before any tag or digest
	// Strip @sha256:... digest if present, then strip tag if present
	firstPartBase := firstPart
	if digestIdx := strings.Index(firstPartBase, "@"); digestIdx >= 0 {
		firstPartBase = firstPartBase[:digestIdx]
	}

	// Use a copy without digest for port detection so we do not depend on tag stripping.
	firstPartNoDigest := firstPart
	if digestIdx := strings.Index(firstPartNoDigest, "@"); digestIdx >= 0 {
		firstPartNoDigest = firstPartNoDigest[:digestIdx]
	}

	firstPartBase = stripTagFromRef(firstPartBase)

	// Detect hostname:port/... style registries where the first path component
	// has a colon followed by a numeric port. Only treat this as a registry when
	// there is at least one "/" in the reference so that "nginx:1.25" is not
	// misclassified as a host:port registry.
	hasPort := false
	if len(parts) > 1 {
		if colonIdx := strings.LastIndex(firstPartNoDigest, ":"); colonIdx >= 0 && colonIdx < len(firstPartNoDigest)-1 {
			portPart := firstPartNoDigest[colonIdx+1:]
			if isPortNumber(portPart) {
				hasPort = true
			}
		}
	}

	// Check if first part looks like a registry
	hasRegistry := strings.Contains(firstPartBase, ".") ||
		firstPartBase == "localhost" ||
		strings.HasPrefix(firstPart, "localhost:") ||
		hasPort

	if hasRegistry {
		// Already has registry, just ensure tag (unless it's a digest)
		return ensureTag(ref)
	}

	if len(parts) == 1 {
		// Simple image name like "nginx" or "nginx:1.25" -> "docker.io/library/nginx"
		return ensureTag("docker.io/library/" + ref)
	}

	// Has namespace but no registry like "bitnami/nginx" -> "docker.io/bitnami/nginx"
	return ensureTag("docker.io/" + ref)
}

// ensureTag adds :latest tag if no tag is present.
func ensureTag(ref string) string {
	// Check for @sha256 digest
	if strings.Contains(ref, "@sha256:") {
		return ref
	}

	// Check if already has a tag (contains : after the last /)
	lastSlash := strings.LastIndex(ref, "/")

	afterSlash := ref
	if lastSlash >= 0 {
		afterSlash = ref[lastSlash+1:]
	}

	if !strings.Contains(afterSlash, ":") {
		return ref + ":latest"
	}

	return ref
}

// ExtractImagesFromMultipleManifests extracts images from multiple manifest strings.
// Images are deduplicated across all manifests.
func ExtractImagesFromMultipleManifests(manifests ...string) ([]string, error) {
	seen := make(map[string]struct{})

	var allImages []string

	for _, manifest := range manifests {
		if manifest == "" {
			continue
		}

		reader := strings.NewReader(manifest)

		images, err := extractImagesFromReader(reader, seen)
		if err != nil {
			return nil, err
		}

		allImages = append(allImages, images...)
	}

	return allImages, nil
}

// ExtractImagesFromBytes extracts images from raw YAML bytes.
func ExtractImagesFromBytes(data []byte) ([]string, error) {
	reader := bytes.NewReader(data)
	seen := make(map[string]struct{})

	return extractImagesFromReader(reader, seen)
}
