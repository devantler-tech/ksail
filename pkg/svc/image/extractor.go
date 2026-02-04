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

// ExtractImagesFromReader extracts images from a YAML reader.
func extractImagesFromReader(r io.Reader, seen map[string]struct{}) ([]string, error) {
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

	scanner := bufio.NewScanner(r)
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
			img = normalizeImageRef(img)

			if _, exists := seen[img]; !exists {
				seen[img] = struct{}{}
				images = append(images, img)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan manifest: %w", err)
	}

	return images, nil
}

// normalizeImageRef normalizes an image reference to a fully qualified form.
// - Adds "docker.io/library/" prefix for images without registry and namespace
// - Adds "docker.io/" prefix for images without registry but with namespace
// - Adds ":latest" tag if no tag is specified (not for @sha256 digests)
func normalizeImageRef(ref string) string {
	// Parse the image reference to determine if it has a registry
	// A registry is present if the first component (before first /) contains:
	// - A dot (.) like docker.io, ghcr.io, registry.k8s.io
	// - A colon (:) for port like localhost:5000 (but NOT for tag like nginx:1.25)
	// - Is exactly "localhost"
	parts := strings.Split(ref, "/")
	firstPart := parts[0]

	// For registry detection, we need to check the part before any tag or digest
	// because version tags like 1.25 contain dots
	// Examples:
	//   "nginx:1.25" -> firstPartBase = "nginx" (no dot = not registry)
	//   "nginx@sha256:abc" -> firstPartBase = "nginx" (no dot = not registry)
	//   "docker.io/nginx:1.25" -> firstPartBase = "docker.io" (has dot = registry)
	//   "localhost:5000/img" -> handled by localhost check
	firstPartBase := firstPart

	// Strip @sha256:... digest if present
	if digestIdx := strings.Index(firstPartBase, "@"); digestIdx >= 0 {
		firstPartBase = firstPartBase[:digestIdx]
	}

	// Strip :tag if present (but keep port numbers for localhost)
	if colonIdx := strings.Index(firstPartBase, ":"); colonIdx >= 0 {
		afterColon := firstPartBase[colonIdx+1:]
		// If after colon is all digits, it's a port; otherwise it's a tag
		isPort := len(afterColon) > 0
		for _, c := range afterColon {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if !isPort {
			// It's a tag like nginx:1.25, strip it for registry detection
			firstPartBase = firstPartBase[:colonIdx]
		}
	}

	// Check if first part looks like a registry
	hasRegistry := strings.Contains(firstPartBase, ".") ||
		firstPartBase == "localhost" ||
		strings.HasPrefix(firstPart, "localhost:")

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
