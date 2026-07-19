package credentials_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultEnvVar_MatchesCanonicalSources guards against drift between this package's locally
// declared default variable names and the canonical defaults owned by the API/provider packages.
func TestDefaultEnvVar_MatchesCanonicalSources(t *testing.T) {
	t.Parallel()

	assert.Equal(
		t,
		v1alpha1.DefaultHetznerTokenEnvVar,
		credentials.DefaultEnvVar(credentials.HetznerToken),
	)
	assert.Equal(t, omni.DefaultEndpointEnvVar, credentials.DefaultEnvVar(credentials.OmniEndpoint))
	assert.Equal(t,
		omni.DefaultServiceAccountKeyEnvVar,
		credentials.DefaultEnvVar(credentials.OmniServiceAccountKey),
	)
	// AWS defaults mirror the struct-tag defaults on v1alpha1.OptionsAWS (no exported constants).
	assert.Equal(t, "AWS_REGION", credentials.DefaultEnvVar(credentials.AWSRegion))
	assert.Equal(t, "AWS_PROFILE", credentials.DefaultEnvVar(credentials.AWSProfile))
	assert.Equal(t, "AWS_ACCESS_KEY_ID", credentials.DefaultEnvVar(credentials.AWSAccessKeyID))
	assert.Equal(
		t,
		"AWS_SECRET_ACCESS_KEY",
		credentials.DefaultEnvVar(credentials.AWSSecretAccessKey),
	)
	assert.Equal(t, "AWS_SESSION_TOKEN", credentials.DefaultEnvVar(credentials.AWSSessionToken))
	// GCP defaults mirror the struct-tag defaults on v1alpha1.OptionsGCP (no exported constants).
	assert.Equal(t, "GOOGLE_CLOUD_PROJECT", credentials.DefaultEnvVar(credentials.GCPProject))
	assert.Equal(t, "GOOGLE_CLOUD_LOCATION", credentials.DefaultEnvVar(credentials.GCPLocation))
	// Azure defaults mirror the struct-tag defaults on v1alpha1.OptionsAzure (no exported constants).
	assert.Equal(
		t,
		"AZURE_SUBSCRIPTION_ID",
		credentials.DefaultEnvVar(credentials.AzureSubscriptionID),
	)
	assert.Equal(
		t,
		"AZURE_RESOURCE_GROUP",
		credentials.DefaultEnvVar(credentials.AzureResourceGroup),
	)
	// The Copilot token has no external canonical source; it mirrors webchat's primary token variable.
	assert.Equal(t, "KSAIL_COPILOT_TOKEN", credentials.DefaultEnvVar(credentials.CopilotToken))
}

func TestDefaultEnvVar_UnknownKeyIsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, credentials.DefaultEnvVar(credentials.Key("nope.nope")))
}

//nolint:gosec // G101: these are environment-variable names, never credential values.
func TestAWSOptionsWithDefaultsPreservesCustomNamesAndFillsEmptyNames(t *testing.T) {
	t.Parallel()

	got := credentials.AWSOptionsWithDefaults(v1alpha1.OptionsAWS{
		ProfileEnvVar: "KSAIL_PROFILE",
		RegionEnvVar:  "KSAIL_REGION",
	})

	assert.Equal(t, v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		RegionEnvVar:          "KSAIL_REGION",
		AccessKeyIDEnvVar:     "AWS_ACCESS_KEY_ID",
		SecretAccessKeyEnvVar: "AWS_SECRET_ACCESS_KEY",
		SessionTokenEnvVar:    "AWS_SESSION_TOKEN",
	}, got)
}

// TestEnvResolver_CopilotTokenFallback verifies the Copilot credential resolves from COPILOT_TOKEN when
// the primary KSAIL_COPILOT_TOKEN is unset, mirroring webchat.copilotToken()'s two-variable lookup.
func TestEnvResolver_CopilotTokenFallback(t *testing.T) {
	// Not parallel: mutates process env via t.Setenv.
	t.Setenv("KSAIL_COPILOT_TOKEN", "")
	t.Setenv("COPILOT_TOKEN", "from-secondary")
	t.Setenv(v1alpha1.DefaultHetznerTokenEnvVar, "")

	var resolver credentials.EnvResolver

	assert.Equal(t, "from-secondary", resolver.Value(credentials.CopilotToken))

	// The primary variable wins when both are set.
	t.Setenv("KSAIL_COPILOT_TOKEN", "from-primary")
	assert.Equal(t, "from-primary", resolver.Value(credentials.CopilotToken))

	// The fallback is Copilot-specific: other credentials don't read COPILOT_TOKEN.
	t.Setenv("KSAIL_COPILOT_TOKEN", "")
	assert.Empty(t, resolver.Value(credentials.HetznerToken))
}

func TestEnvResolver_ReadsProcessEnvironment(t *testing.T) {
	t.Setenv(v1alpha1.DefaultHetznerTokenEnvVar, "tok-123")

	var resolver credentials.EnvResolver

	assert.Equal(t, v1alpha1.DefaultHetznerTokenEnvVar, resolver.EnvVar(credentials.HetznerToken))
	assert.Equal(t, "tok-123", resolver.Value(credentials.HetznerToken))
}

func TestEnvResolver_UnsetCredentialIsEmpty(t *testing.T) {
	t.Setenv(credentials.DefaultEnvVar(credentials.OmniEndpoint), "")

	var resolver credentials.EnvResolver

	assert.Empty(t, resolver.Value(credentials.OmniEndpoint))
}

func TestAllKeys_CoversEveryKeyWithDefault(t *testing.T) {
	t.Parallel()

	keys := credentials.AllKeys()
	require.NotEmpty(t, keys)

	for _, key := range keys {
		assert.NotEmptyf(
			t,
			credentials.DefaultEnvVar(key),
			"key %q must have a default env var",
			key,
		)
	}
}
