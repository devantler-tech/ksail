package credentials_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
)

// stubResolver stands in for an injected resolver (the Settings/secure-store one the local API
// service carries). It reports names and values independently so a test can pin which of the two
// layers a result came from.
type stubResolver struct {
	names  map[credentials.Key]string
	values map[credentials.Key]string
}

func (s stubResolver) EnvVar(key credentials.Key) string {
	if name := s.names[key]; name != "" {
		return name
	}

	return credentials.DefaultEnvVar(key)
}

func (s stubResolver) Value(key credentials.Key) string { return s.values[key] }

func recordedOptions() v1alpha1.OptionsAWS {
	return v1alpha1.OptionsAWS{
		ProfileEnvVar:         "RECORDED_PROFILE",
		RegionEnvVar:          "RECORDED_REGION",
		AccessKeyIDEnvVar:     "RECORDED_ACCESS",
		SecretAccessKeyEnvVar: "RECORDED_SECRET",
		SessionTokenEnvVar:    "RECORDED_SESSION",
	}
}

// TestRecordedAWSResolverReportsTheRecordedNames pins the half of the composition that cannot come
// from the base resolver. The frozen resolution carries these names onward to scrub the child
// process environment, so reporting the base's canonical name for a value that was read from a
// recorded alias would leave the alias in place for the provisioner to re-resolve.
func TestRecordedAWSResolverReportsTheRecordedNames(t *testing.T) {
	t.Parallel()

	resolver := credentials.NewRecordedAWSResolver(
		stubResolver{names: map[credentials.Key]string{
			credentials.AWSAccessKeyID: "SETTINGS_ACCESS",
		}},
		recordedOptions(),
	)

	assert.Equal(t, "RECORDED_ACCESS", resolver.EnvVar(credentials.AWSAccessKeyID),
		"the record's captured variable name did not survive the composition")
	assert.Equal(t, "RECORDED_PROFILE", resolver.EnvVar(credentials.AWSProfile))
	assert.Equal(t, "RECORDED_REGION", resolver.EnvVar(credentials.AWSRegion))
}

// TestRecordedAWSResolverPrefersTheBaseValue keeps the composition strictly additive. A secure-store
// value is name-independent operator intent, so a credential the injected resolver already resolves
// must keep resolving exactly as it did before the record was consulted.
func TestRecordedAWSResolverPrefersTheBaseValue(t *testing.T) {
	t.Setenv("RECORDED_ACCESS", "from-recorded-alias")

	resolver := credentials.NewRecordedAWSResolver(
		stubResolver{values: map[credentials.Key]string{
			credentials.AWSAccessKeyID: "from-secure-store",
		}},
		recordedOptions(),
	)

	assert.Equal(t, "from-secure-store", resolver.Value(credentials.AWSAccessKeyID),
		"layering the record over the base displaced a value the base already resolved")
}

// TestRecordedAWSResolverFallsBackToTheRecordedAlias is the defect itself: credentials that made the
// cluster remain reachable only under the names capture persisted. Without this fallback a Delete,
// Start, or Stop resolves nothing and fails as unavailable credentials.
func TestRecordedAWSResolverFallsBackToTheRecordedAlias(t *testing.T) {
	t.Setenv("RECORDED_SECRET", "from-recorded-alias")

	resolver := credentials.NewRecordedAWSResolver(stubResolver{}, recordedOptions())

	assert.Equal(t, "from-recorded-alias", resolver.Value(credentials.AWSSecretAccessKey),
		"a credential reachable only under the recorded alias did not resolve")
}

// TestRecordedAWSResolverWithoutABaseResolvesFromTheEnvironment covers the nil base the local API
// service passes whenever no Settings resolver was injected, which is the default for the CLI.
func TestRecordedAWSResolverWithoutABaseResolvesFromTheEnvironment(t *testing.T) {
	t.Setenv("RECORDED_PROFILE", "recorded-profile")

	resolver := credentials.NewRecordedAWSResolver(nil, recordedOptions())

	assert.Equal(t, "recorded-profile", resolver.Value(credentials.AWSProfile),
		"a nil base resolver did not fall through to the recorded alias")
}

// TestRecordedAWSResolverKeepsCanonicalNamesForALegacyRecord pins the empty-mapping case. A record
// predating the mapping schema must resolve exactly as the canonical environment does, so composing
// one can never make an already-working cluster stop resolving.
func TestRecordedAWSResolverKeepsCanonicalNamesForALegacyRecord(t *testing.T) {
	t.Parallel()

	resolver := credentials.NewRecordedAWSResolver(stubResolver{}, v1alpha1.OptionsAWS{})

	assert.Equal(t, credentials.DefaultEnvVar(credentials.AWSAccessKeyID),
		resolver.EnvVar(credentials.AWSAccessKeyID),
		"an empty recorded mapping did not fall back to the canonical variable name")
}
