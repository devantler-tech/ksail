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
	"runtime"
	"strings"

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
	// GCPProject is the Google Cloud project ID used for GKE operations.
	GCPProject Key = "gcp.project"
	// GCPLocation is the GKE location (zone or region); empty means all locations.
	GCPLocation Key = "gcp.location"
	// AzureSubscriptionID is the Azure subscription ID used for AKS operations.
	AzureSubscriptionID Key = "azure.subscriptionId"
	// AzureResourceGroup is the Azure resource group hosting AKS clusters; empty means the whole
	// subscription.
	AzureResourceGroup Key = "azure.resourceGroup"
	// CopilotToken is the GitHub Copilot token the AI assistant authenticates with. Not a cloud
	// provider, but resolved the same way (secure store override, otherwise the environment) so it can
	// be configured from the Settings page instead of only via the environment.
	CopilotToken Key = "copilot.token"
)

// Default environment variable names. These mirror the canonical defaults declared on the v1alpha1
// provider option structs (and the omni provider package); credentials_test asserts they stay in
// sync so this package does not pull the heavyweight provider clients into every importer.
const (
	defaultOmniEndpointEnvVar       = "OMNI_ENDPOINT"
	defaultOmniServiceAccountKey    = "OMNI_SERVICE_ACCOUNT_KEY"
	defaultAWSRegionEnvVar          = "AWS_REGION"
	defaultAWSProfileEnvVar         = "AWS_PROFILE"
	defaultAWSAccessKeyIDEnvVar     = "AWS_ACCESS_KEY_ID"
	defaultAWSSecretAccessEnvVar    = "AWS_SECRET_ACCESS_KEY" //nolint:gosec // env var NAME, not a secret
	defaultAWSSessionTokenEnvVar    = "AWS_SESSION_TOKEN"     //nolint:gosec // env var NAME, not a secret
	defaultGCPProjectEnvVar         = "GOOGLE_CLOUD_PROJECT"
	defaultGCPLocationEnvVar        = "GOOGLE_CLOUD_LOCATION"
	defaultAzureSubscriptionEnvVar  = "AZURE_SUBSCRIPTION_ID"
	defaultAzureResourceGroupEnvVar = "AZURE_RESOURCE_GROUP"
	// defaultCopilotTokenEnvVar is the primary variable webchat.copilotToken() reads first (it also
	// falls back to COPILOT_TOKEN); using it as the default keeps a stored token resolvable via Overlay.
	defaultCopilotTokenEnvVar = "KSAIL_COPILOT_TOKEN" //nolint:gosec // env var NAME, not a secret
	// copilotEnvFallback is the secondary variable webchat.copilotToken() reads after the primary; the
	// credential resolution mirrors it so Settings recognizes a Copilot token set only via COPILOT_TOKEN.
	copilotEnvFallback = "COPILOT_TOKEN"
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
		GCPProject,
		GCPLocation,
		AzureSubscriptionID,
		AzureResourceGroup,
		CopilotToken,
	}
}

// DefaultEnvVar returns the conventional environment-variable name a credential resolves from when
// no override key has been configured, or "" for an unknown key.
func DefaultEnvVar(key Key) string {
	return map[Key]string{
		HetznerToken:          v1alpha1.DefaultHetznerTokenEnvVar,
		OmniEndpoint:          defaultOmniEndpointEnvVar,
		OmniServiceAccountKey: defaultOmniServiceAccountKey,
		AWSRegion:             defaultAWSRegionEnvVar,
		AWSProfile:            defaultAWSProfileEnvVar,
		AWSAccessKeyID:        defaultAWSAccessKeyIDEnvVar,
		AWSSecretAccessKey:    defaultAWSSecretAccessEnvVar,
		AWSSessionToken:       defaultAWSSessionTokenEnvVar,
		GCPProject:            defaultGCPProjectEnvVar,
		GCPLocation:           defaultGCPLocationEnvVar,
		AzureSubscriptionID:   defaultAzureSubscriptionEnvVar,
		AzureResourceGroup:    defaultAzureResourceGroupEnvVar,
		CopilotToken:          defaultCopilotTokenEnvVar,
	}[key]
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

// EnvResolver resolves purely from the process environment using canonical
// variable names. Its zero value remains the default resolver.
type EnvResolver struct{}

// AWSOptionsResolver resolves from immutable per-cluster AWS variable-name
// overrides without mutating the process environment.
type AWSOptionsResolver struct {
	envVars map[Key]string
}

// NewAWSOptionsResolver returns an environment-only resolver honoring the
// variable names configured on one AWS provider spec. Empty names retain their
// canonical defaults. The resolver owns its map and is safe for concurrent use.
func NewAWSOptionsResolver(options v1alpha1.OptionsAWS) AWSOptionsResolver {
	return AWSOptionsResolver{envVars: map[Key]string{
		AWSRegion:          options.RegionEnvVar,
		AWSProfile:         options.ProfileEnvVar,
		AWSAccessKeyID:     options.AccessKeyIDEnvVar,
		AWSSecretAccessKey: options.SecretAccessKeyEnvVar,
		AWSSessionToken:    options.SessionTokenEnvVar,
	}}
}

// EnvVar returns the default environment-variable name for key.
func (EnvResolver) EnvVar(key Key) string { return DefaultEnvVar(key) }

// Value returns the process-environment value for key's default variable, or "".
func (EnvResolver) Value(key Key) string {
	return resolveEnvValue(key, DefaultEnvVar(key))
}

// EnvVar returns the configured environment-variable name for key, falling
// back to its canonical default.
func (r AWSOptionsResolver) EnvVar(key Key) string {
	if name := r.envVars[key]; name != "" {
		return name
	}

	return DefaultEnvVar(key)
}

// Value returns the process-environment value for key's configured variable, or "".
func (r AWSOptionsResolver) Value(key Key) string {
	return resolveEnvValue(key, r.EnvVar(key))
}

// AWSResolution is an immutable snapshot of the credential selection for one
// AWS operation. Source variable names are retained privately so child process
// environments can remove both stale canonical values and custom aliases.
type AWSResolution struct {
	Profile         string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	sourceEnvVars          [4]string
	hasCustomCredentialEnv bool
}

// ResolveAWS snapshots all AWS credential values and their configured source
// names from resolver. A nil resolver uses the canonical process environment.
func ResolveAWS(resolver Resolver) AWSResolution {
	if resolver == nil {
		resolver = EnvResolver{}
	}

	// Resolve every value before constructing the child environment. No parent
	// environment mutation is needed, so concurrent invocations remain isolated.
	resolution := AWSResolution{
		Profile:         resolver.Value(AWSProfile),
		AccessKeyID:     resolver.Value(AWSAccessKeyID),
		SecretAccessKey: resolver.Value(AWSSecretAccessKey),
		SessionToken:    resolver.Value(AWSSessionToken),
		sourceEnvVars: [4]string{
			resolver.EnvVar(AWSProfile),
			resolver.EnvVar(AWSAccessKeyID),
			resolver.EnvVar(AWSSecretAccessKey),
			resolver.EnvVar(AWSSessionToken),
		},
	}

	canonicalNames := [...]string{
		defaultAWSProfileEnvVar,
		defaultAWSAccessKeyIDEnvVar,
		defaultAWSSecretAccessEnvVar,
		defaultAWSSessionTokenEnvVar,
	}
	for index, sourceName := range resolution.sourceEnvVars {
		if sourceName != "" && sourceName != canonicalNames[index] {
			resolution.hasCustomCredentialEnv = true

			break
		}
	}

	return resolution
}

// HasCustomCredentialSources reports whether at least one credential is
// configured to resolve from a non-canonical variable name. Callers use this
// to fail closed instead of letting an SDK read stale canonical credentials
// when every configured custom source is unset.
func (r AWSResolution) HasCustomCredentialSources() bool {
	return r.hasCustomCredentialEnv
}

// OptionsForAWSResolution maps a resolved AWS identity into a consumer's
// option type. Custom source names add the consumer's fail-closed option so an
// unset alias cannot silently fall back to an unrelated ambient identity.
func OptionsForAWSResolution[T any](
	resolution AWSResolution,
	withCredentialValues func(profile, accessKeyID, secretAccessKey, sessionToken string) T,
	requireCredentialValues func() T,
) []T {
	option := withCredentialValues(
		resolution.Profile,
		resolution.AccessKeyID,
		resolution.SecretAccessKey,
		resolution.SessionToken,
	)

	return optionsWithCredentialRequirement(
		option,
		resolution.HasCustomCredentialSources(),
		requireCredentialValues,
	)
}

// OptionsForAWSChildEnvironment maps an AWS resolution's isolated child
// environment into a consumer's option type and adds its fail-closed option
// when credential aliases are custom.
func OptionsForAWSChildEnvironment[T any](
	resolution AWSResolution,
	parent []string,
	withEnvironment func(environment []string) T,
	requireCredentialValues func() T,
) []T {
	return optionsWithCredentialRequirement(
		withEnvironment(resolution.ChildEnvironment(parent)),
		resolution.HasCustomCredentialSources(),
		requireCredentialValues,
	)
}

func optionsWithCredentialRequirement[T any](
	option T,
	required bool,
	requireCredentialValues func() T,
) []T {
	options := []T{option}
	if required {
		options = append(options, requireCredentialValues())
	}

	return options
}

// ChildEnvironment returns a copy of parent with AWS credential aliases and
// stale canonical values removed, followed by the non-empty resolved values
// under the canonical names eksctl understands. When a custom credential source
// is configured, competing environment-based identity providers are also
// removed so they cannot override the explicit selection. Unrelated entries
// such as PATH, HOME, and AWS_CONFIG_FILE are preserved.
func (r AWSResolution) ChildEnvironment(parent []string) []string {
	caseInsensitiveNames := runtime.GOOS == "windows"
	strippedNames := r.strippedEnvironmentNames(caseInsensitiveNames)
	child := filterEnvironment(parent, strippedNames, caseInsensitiveNames)

	for _, binding := range []struct {
		name  string
		value string
	}{
		{name: defaultAWSProfileEnvVar, value: r.Profile},
		{name: defaultAWSAccessKeyIDEnvVar, value: r.AccessKeyID},
		{name: defaultAWSSecretAccessEnvVar, value: r.SecretAccessKey},
		{name: defaultAWSSessionTokenEnvVar, value: r.SessionToken},
	} {
		if binding.value != "" {
			child = append(child, binding.name+"="+binding.value)
		}
	}

	return child
}

func (r AWSResolution) strippedEnvironmentNames(caseInsensitive bool) map[string]struct{} {
	strippedNames := make(map[string]struct{})
	for _, name := range []string{
		defaultAWSProfileEnvVar,
		defaultAWSAccessKeyIDEnvVar,
		defaultAWSSecretAccessEnvVar,
		defaultAWSSessionTokenEnvVar,
	} {
		strippedNames[normalizeEnvironmentName(name, caseInsensitive)] = struct{}{}
	}

	for _, name := range r.sourceEnvVars {
		if name != "" {
			strippedNames[normalizeEnvironmentName(name, caseInsensitive)] = struct{}{}
		}
	}

	if r.hasCustomCredentialEnv {
		for _, name := range []string{
			"AWS_DEFAULT_PROFILE",
			"AWS_ACCESS_KEY",
			"AWS_SECRET_KEY",
			"AWS_WEB_IDENTITY_TOKEN_FILE",
			"AWS_ROLE_ARN",
			"AWS_ROLE_SESSION_NAME",
		} {
			strippedNames[normalizeEnvironmentName(name, caseInsensitive)] = struct{}{}
		}
	}

	return strippedNames
}

func filterEnvironment(
	parent []string,
	strippedNames map[string]struct{},
	caseInsensitive bool,
) []string {
	child := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if found {
			if _, stripped := strippedNames[normalizeEnvironmentName(name, caseInsensitive)]; stripped {
				continue
			}
		}

		child = append(child, entry)
	}

	return child
}

func normalizeEnvironmentName(name string, caseInsensitive bool) string {
	if caseInsensitive {
		return strings.ToUpper(name)
	}

	return name
}

// resolveEnvValue reads key's value from the process environment under envVar, applying the Copilot
// secondary-variable fallback (COPILOT_TOKEN) when the primary default is unset — matching
// webchat.copilotToken(), so a COPILOT_TOKEN-only setup still resolves (and reports as "env").
func resolveEnvValue(key Key, envVar string) string {
	if envVar == "" {
		return ""
	}

	value := os.Getenv(envVar)
	if value != "" {
		return value
	}

	if key == CopilotToken && envVar == defaultCopilotTokenEnvVar {
		return os.Getenv(copilotEnvFallback)
	}

	return ""
}

// Ensure EnvResolver satisfies Resolver.
var _ Resolver = EnvResolver{}

// Ensure AWSOptionsResolver satisfies Resolver.
var _ Resolver = AWSOptionsResolver{}
