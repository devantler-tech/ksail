// Package registryauth centralizes registry credential conventions that are shared
// between the paths that persist credentials into a cluster and the paths that read
// them back.
//
// Which environment variable holds a registry token is declared in configuration
// (see LocalRegistry.Credentials), not inferred here: no registry host carries
// special meaning and no environment variable has implicit runtime semantics.
package registryauth

const (
	// CredentialPurposeAnnotation marks the intended operation for credentials
	// persisted in a cluster so a pull-only secret is never reused for pushes.
	//nolint:gosec // G101: this is a Kubernetes annotation key, not a credential.
	CredentialPurposeAnnotation = "ksail.io/credential-purpose"
	// PullCredentialPurpose identifies credentials that may only be used for pulls.
	PullCredentialPurpose = "pull"
)
