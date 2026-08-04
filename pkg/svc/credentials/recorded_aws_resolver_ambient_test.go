package credentials_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// explicitStubResolver stands in for the Settings/secure-store manager: it separates a *stored*
// value (deliberate operator intent, name-independent) from the ambient environment value it would
// otherwise fall through to. Only the stored half may outrank an ownership record.
type explicitStubResolver struct {
	stored map[credentials.Key]string
	names  map[credentials.Key]string
}

func (s explicitStubResolver) EnvVar(key credentials.Key) string {
	if name := s.names[key]; name != "" {
		return name
	}

	return credentials.DefaultEnvVar(key)
}

// Value mirrors Manager.Value: a stored value when present, otherwise the process-environment value
// for the configured (canonical, by default) variable name.
func (s explicitStubResolver) Value(key credentials.Key) string {
	if value := s.stored[key]; value != "" {
		return value
	}

	return os.Getenv(s.EnvVar(key))
}

func (s explicitStubResolver) ExplicitValue(key credentials.Key) string { return s.stored[key] }

func aliasedOptions() v1alpha1.OptionsAWS {
	return v1alpha1.OptionsAWS{AccessKeyIDEnvVar: "RECORDED_ACCESS"}
}

// TestRecordedAWSResolverPrefersTheRecordOverAmbientCanonical is the defect this file was written
// for. The web UI resolves through credentials.Manager, whose Value falls through to the canonical
// process environment whenever nothing is stored. Composed under an ownership record, that fall-
// through outranked the record — so a cluster created under a custom alias resolved whatever
// AWS_ACCESS_KEY_ID happened to be set to, which is the ambient identity the record exists to pin.
func TestRecordedAWSResolverPrefersTheRecordOverAmbientCanonical(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-account-a")
	t.Setenv("RECORDED_ACCESS", "recorded-account-b")

	resolver := credentials.NewRecordedAWSResolver(explicitStubResolver{}, aliasedOptions())

	assert.Equal(t, "recorded-account-b", resolver.Value(credentials.AWSAccessKeyID),
		"the ambient canonical variable displaced the alias the ownership record captured")
}

// TestRecordedAWSResolverStillPrefersAStoredValue keeps the deliberate half of the precedence
// intact: a secure-store credential is name-independent operator intent and must still outrank the
// record, exactly as before. This is the control for the test above — without it, the fix could
// simply be "the record always wins", which would silently break Settings overrides.
func TestRecordedAWSResolverStillPrefersAStoredValue(t *testing.T) {
	t.Setenv("RECORDED_ACCESS", "recorded-account-b")

	resolver := credentials.NewRecordedAWSResolver(
		explicitStubResolver{stored: map[credentials.Key]string{
			credentials.AWSAccessKeyID: "from-secure-store",
		}},
		aliasedOptions(),
	)

	assert.Equal(t, "from-secure-store", resolver.Value(credentials.AWSAccessKeyID),
		"a stored secure-store credential stopped outranking the ownership record")
}

// TestRecordedAWSResolverFallsBackToAmbientForAnUnrecordedKey pins that the fix narrows nothing it
// should not: a key the record carries no alias for still resolves from the environment, so an
// operator who aliased only one variable does not lose the rest.
func TestRecordedAWSResolverFallsBackToAmbientForAnUnrecordedKey(t *testing.T) {
	t.Setenv("AWS_SESSION_TOKEN", "ambient-session")

	resolver := credentials.NewRecordedAWSResolver(explicitStubResolver{}, aliasedOptions())

	assert.Equal(t, "ambient-session", resolver.Value(credentials.AWSSessionToken),
		"a key the record carries no alias for stopped resolving from the environment")
}

// TestRecordedAWSResolverWithNilBasePrefersTheRecord covers the CLI shape, where no Settings
// resolver is injected at all. A nil base used to become a plain canonical EnvResolver, which put
// the ambient environment ahead of the record for exactly the same reason.
func TestRecordedAWSResolverWithNilBasePrefersTheRecord(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-account-a")
	t.Setenv("RECORDED_ACCESS", "recorded-account-b")

	resolver := credentials.NewRecordedAWSResolver(nil, aliasedOptions())

	assert.Equal(t, "recorded-account-b", resolver.Value(credentials.AWSAccessKeyID),
		"with no injected resolver the ambient canonical variable still displaced the record")
}

// TestRecordedAWSResolverNameAndValueAgree pins the property that made the defect more than a
// precedence question. The frozen resolution carries EnvVar onward to scrub the child process
// environment, so reporting the record's alias while returning a value read from the canonical
// variable left the two describing different variables — and the scrub then missed the one the
// value actually came from.
func TestRecordedAWSResolverNameAndValueAgree(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-account-a")
	t.Setenv("RECORDED_ACCESS", "recorded-account-b")

	resolver := credentials.NewRecordedAWSResolver(explicitStubResolver{}, aliasedOptions())

	name := resolver.EnvVar(credentials.AWSAccessKeyID)
	require.Equal(t, "RECORDED_ACCESS", name)

	assert.Equal(t, "recorded-account-b", resolver.Value(credentials.AWSAccessKeyID),
		"EnvVar reported %q while Value came from a different variable", name)
}

// TestRecordedAWSResolverOverARealManager is the production shape: `ksail open web` composes the
// ownership record over credentials.Manager, not over a stub. Manager.Value falls through to the
// canonical environment whenever the secure store holds nothing, and that fall-through is what used
// to outrank the record. Both arms run against a real Manager so the composition is proven where it
// actually happens.
func TestRecordedAWSResolverOverARealManager(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_ACCESS_KEY_ID", "ambient-account-a")
	t.Setenv("RECORDED_ACCESS", "recorded-account-b")

	manager, store := newManager(t)
	resolver := credentials.NewRecordedAWSResolver(manager, aliasedOptions())

	// Nothing stored: the record's alias must win over the ambient canonical variable.
	assert.Equal(t, "recorded-account-b", resolver.Value(credentials.AWSAccessKeyID),
		"Manager's ambient fall-through displaced the ownership record's alias")

	// Stored: deliberate operator intent still outranks the record.
	require.NoError(t, store.Set(credentials.AWSAccessKeyID, "from-secure-store"))
	assert.Equal(t, "from-secure-store", resolver.Value(credentials.AWSAccessKeyID),
		"a stored secure-store credential stopped outranking the ownership record")
}
