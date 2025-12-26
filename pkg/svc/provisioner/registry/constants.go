package registry

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// Shared registry constants used across services and CLI layers.
const (
	// LocalRegistryContainerName is the docker container name for the developer registry.
	LocalRegistryContainerName = "local-registry"
	// LocalRegistryClusterHost is the hostname clusters use to reach the local registry.
	LocalRegistryClusterHost = LocalRegistryContainerName
	// DefaultLocalArtifactTag is used when no explicit tag is provided for a workload
	// artifact. The "dev" tag is intended only for local development and will
	// typically point to the most recently built image, which is convenient but
	// not suitable for reproducible or production deployments where explicit
	// immutable version tags (for example, semantic versions or digests) should
	// be used instead.
	DefaultLocalArtifactTag = "dev"
	// DefaultRepoName is used when no repository name can be derived.
	DefaultRepoName = "ksail-workloads"
)

// SanitizeRepoName converts a source directory path into a DNS-compliant repository name.
// This function is used by both the OCI push command and Flux installer to ensure
// the artifact repository name matches what Flux expects.
//
// The function:
//   - Converts to lowercase
//   - Replaces non-alphanumeric characters with hyphens
//   - Collapses consecutive hyphens
//   - Trims leading/trailing hyphens
//   - Truncates to DNS1123LabelMaxLength (63 chars)
//   - Falls back to DefaultRepoName if result is invalid
//
//nolint:cyclop // name sanitization requires character-by-character validation
func SanitizeRepoName(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return DefaultRepoName
	}

	var builder strings.Builder

	previousHyphen := false

	for _, char := range trimmed {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)

			previousHyphen = false
		case char >= '0' && char <= '9':
			builder.WriteRune(char)

			previousHyphen = false
		default:
			if !previousHyphen {
				builder.WriteRune('-')

				previousHyphen = true
			}
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if sanitized == "" {
		return DefaultRepoName
	}

	if len(sanitized) > validation.DNS1123LabelMaxLength {
		sanitized = sanitized[:validation.DNS1123LabelMaxLength]
		sanitized = strings.Trim(sanitized, "-")
	}

	if sanitized == "" {
		return DefaultRepoName
	}

	if len(validation.IsDNS1123Label(sanitized)) == 0 {
		return sanitized
	}

	return DefaultRepoName
}
