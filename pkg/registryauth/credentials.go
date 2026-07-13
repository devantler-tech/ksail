// Package registryauth centralizes registry credential environment conventions.
package registryauth

import (
	"os"
	"strings"
)

const (
	// GHCRHost is the canonical GitHub Container Registry host.
	GHCRHost = "ghcr.io"
	// GHCRTokenEnvVar is the legacy token used for both push and pull operations.
	GHCRTokenEnvVar = "GHCR_TOKEN"
	// GHCRPullTokenEnvVar is the optional least-privilege token used by cluster pull paths.
	//nolint:gosec // This is an environment variable name, not a credential.
	GHCRPullTokenEnvVar = "GHCR_PULL_TOKEN"
	// CredentialPurposeAnnotation marks the intended operation for credentials
	// persisted in a cluster so a pull-only secret is never reused for pushes.
	//nolint:gosec // G101: this is a Kubernetes annotation key, not a credential.
	CredentialPurposeAnnotation = "ksail.io/credential-purpose"
	// PullCredentialPurpose identifies credentials that may only be used for pulls.
	PullCredentialPurpose = "pull"
)

// PullPassword returns the dedicated GHCR pull token when configured, otherwise
// it preserves the configured registry password. The override is host-scoped so
// an ambient GHCR token cannot replace credentials for another registry.
func PullPassword(host, configuredPassword string) string {
	if !strings.EqualFold(strings.TrimSpace(host), GHCRHost) {
		return configuredPassword
	}

	pullToken, exists := os.LookupEnv(GHCRPullTokenEnvVar)
	if !exists || pullToken == "" {
		return configuredPassword
	}

	return pullToken
}

// PullEnvLookup resolves environment variables for cluster pull configuration.
// An unset or empty GHCR_PULL_TOKEN falls back to GHCR_TOKEN for compatibility.
func PullEnvLookup(name string) (string, bool) {
	if name != GHCRPullTokenEnvVar && name != GHCRTokenEnvVar {
		return os.LookupEnv(name)
	}

	pullToken, exists := os.LookupEnv(GHCRPullTokenEnvVar)
	if exists && pullToken != "" {
		return pullToken, true
	}

	return os.LookupEnv(GHCRTokenEnvVar)
}
