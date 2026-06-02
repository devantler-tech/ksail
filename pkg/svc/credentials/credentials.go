// Package credentials resolves cloud-provider credentials for KSail's local UI backend.
//
// A credential resolves from a secure-store override (set via the UI Settings page) when present,
// otherwise from the process environment under a configurable key name that defaults to each
// provider's conventional variable (e.g. HCLOUD_TOKEN). It provides the credential Key/Resolver
// contract together with EnvResolver (environment-only), the secure Store (OS keyring + in-memory),
// and Manager, which layers the keyring store and a settings file on top of that contract to back
// the UI Settings page and export resolved values into the process environment.
package credentials

import (
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// Key identifies a single provider credential. Keys are stable, dotted identifiers so they can be
// used as map keys, secure-store entry names, and wire identifiers for the Settings API.
type Key string

// These constants are credential *identifiers* (stable map keys / wire IDs), not secret values.
//
//nolint:gosec // G101: dotted identifiers, not hardcoded credentials.
const (
	// HetznerToken is the Hetzner Cloud API token.
	HetznerToken Key = "hetzner.token"
	// OmniEndpoint is the Sidero Omni API endpoint URL.
	OmniEndpoint Key = "omni.endpoint"
	// OmniServiceAccountKey is the Sidero Omni service-account key.
	OmniServiceAccountKey Key = "omni.serviceAccountKey"
	// AWSRegion is the AWS region used for EKS operations.
	AWSRegion Key = "aws.region"
	// AWSProfile is the AWS named profile.
	AWSProfile Key = "aws.profile"
	// AWSAccessKeyID is the AWS access key ID.
	AWSAccessKeyID Key = "aws.accessKeyId"
	// AWSSecretAccessKey is the AWS secret access key.
	AWSSecretAccessKey Key = "aws.secretAccessKey"
	// AWSSessionToken is the AWS session token (for temporary credentials).
	AWSSessionToken Key = "aws.sessionToken"
)

// Default environment variable names. These mirror the canonical defaults declared on the v1alpha1
// provider option structs (and the omni provider package); credentials_test asserts they stay in
// sync so this package does not pull the heavyweight provider clients into every importer.
const (
	defaultOmniEndpointEnvVar    = "OMNI_ENDPOINT"
	defaultOmniServiceAccountKey = "OMNI_SERVICE_ACCOUNT_KEY"
	defaultAWSRegionEnvVar       = "AWS_REGION"
	defaultAWSProfileEnvVar      = "AWS_PROFILE"
	defaultAWSAccessKeyIDEnvVar  = "AWS_ACCESS_KEY_ID"
	defaultAWSSecretAccessEnvVar = "AWS_SECRET_ACCESS_KEY" //nolint:gosec // env var NAME, not a secret
	defaultAWSSessionTokenEnvVar = "AWS_SESSION_TOKEN"     //nolint:gosec // env var NAME, not a secret
)

// AllKeys returns every credential key in a stable order. The Settings UI and API iterate this.
func AllKeys() []Key {
	return []Key{
		HetznerToken,
		OmniEndpoint,
		OmniServiceAccountKey,
		AWSRegion,
		AWSProfile,
		AWSAccessKeyID,
		AWSSecretAccessKey,
		AWSSessionToken,
	}
}

// DefaultEnvVar returns the conventional environment-variable name a credential resolves from when
// no override key has been configured.
func DefaultEnvVar(key Key) string {
	switch key {
	case HetznerToken:
		return v1alpha1.DefaultHetznerTokenEnvVar
	case OmniEndpoint:
		return defaultOmniEndpointEnvVar
	case OmniServiceAccountKey:
		return defaultOmniServiceAccountKey
	case AWSRegion:
		return defaultAWSRegionEnvVar
	case AWSProfile:
		return defaultAWSProfileEnvVar
	case AWSAccessKeyID:
		return defaultAWSAccessKeyIDEnvVar
	case AWSSecretAccessKey:
		return defaultAWSSecretAccessEnvVar
	case AWSSessionToken:
		return defaultAWSSessionTokenEnvVar
	default:
		return ""
	}
}

// Resolver resolves credential values and the environment-variable names they resolve from.
// Implementations are safe for concurrent use.
type Resolver interface {
	// Value returns the resolved value for key: a secure-store override when set, otherwise
	// os.Getenv(EnvVar(key)). It returns "" when the credential is unset.
	Value(key Key) string
	// EnvVar returns the environment-variable name key resolves from (the configured override name,
	// or DefaultEnvVar(key)).
	EnvVar(key Key) string
}

// EnvResolver resolves purely from the process environment using the default variable names. It is
// the zero-config resolver used when no secure store / Settings overrides are configured.
type EnvResolver struct{}

// EnvVar returns the default environment-variable name for key.
func (EnvResolver) EnvVar(key Key) string { return DefaultEnvVar(key) }

// Value returns the process-environment value for key's default variable, or "".
func (EnvResolver) Value(key Key) string {
	name := DefaultEnvVar(key)
	if name == "" {
		return ""
	}

	return os.Getenv(name)
}

// Ensure EnvResolver satisfies Resolver.
var _ Resolver = EnvResolver{}
