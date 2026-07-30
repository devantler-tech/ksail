package credentials_test

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type awsResolverFixture struct {
	envVars map[credentials.Key]string
	values  map[credentials.Key]string
}

// EnvVar returns the fixture's configured source name or the credential's canonical default.
func (f awsResolverFixture) EnvVar(key credentials.Key) string {
	if name := f.envVars[key]; name != "" {
		return name
	}

	return credentials.DefaultEnvVar(key)
}

// Value returns the fixture value associated with a credential key.
func (f awsResolverFixture) Value(key credentials.Key) string { return f.values[key] }

// TestNewAWSOptionsResolver_UsesConfiguredNamesWithoutMutatingParent verifies
// immutable alias lookup leaves process state untouched.
func TestNewAWSOptionsResolver_UsesConfiguredNamesWithoutMutatingParent(t *testing.T) {
	// Not parallel: t.Setenv changes the process environment.
	t.Setenv("KSAIL_PROFILE", "selected-profile")
	t.Setenv("KSAIL_ACCESS", "fixture-access")
	t.Setenv("KSAIL_SECRET", "fixture-secret")
	t.Setenv("KSAIL_SESSION", "fixture-session")
	t.Setenv("AWS_PROFILE", "stale-profile")

	resolver := credentials.NewAWSOptionsResolver(v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		AccessKeyIDEnvVar:     "KSAIL_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_SECRET",
		SessionTokenEnvVar:    "KSAIL_SESSION",
	})
	resolved := credentials.ResolveAWS(resolver)

	assert.Equal(t, "KSAIL_PROFILE", resolver.EnvVar(credentials.AWSProfile))
	assert.Equal(t, "selected-profile", resolved.Profile)
	assert.Equal(t, "fixture-access", resolved.AccessKeyID)
	assert.NotEmpty(t, resolved.SecretAccessKey)
	assert.NotEmpty(t, resolved.SessionToken)
	assert.Equal(t, "stale-profile", os.Getenv("AWS_PROFILE"))
}

// TestAWSResolution_ChildEnvironmentCanonicalizesAndIsolatesCredentials
// verifies aliases become canonical values in an isolated child environment.
func TestAWSResolution_ChildEnvironmentCanonicalizesAndIsolatesCredentials(t *testing.T) {
	t.Parallel()

	parent := []string{
		"PATH=/usr/bin",
		"HOME=/home/ksail",
		"AWS_CONFIG_FILE=/home/ksail/.aws/config",
		"AWS_PROFILE=stale-profile",
		"AWS_ACCESS_KEY_ID=stale-access",
		"AWS_SECRET_ACCESS_KEY=stale-secret",
		"AWS_SESSION_TOKEN=stale-session",
		"KSAIL_PROFILE=selected-profile",
		"KSAIL_ACCESS=fixture-access",
		"KSAIL_SECRET=fixture-secret",
		"KSAIL_SESSION=fixture-session",
	}
	before := slices.Clone(parent)
	resolver := awsResolverFixture{
		envVars: map[credentials.Key]string{
			credentials.AWSProfile:         "KSAIL_PROFILE",
			credentials.AWSAccessKeyID:     "KSAIL_ACCESS",
			credentials.AWSSecretAccessKey: "KSAIL_SECRET",
			credentials.AWSSessionToken:    "KSAIL_SESSION",
		},
		values: map[credentials.Key]string{
			credentials.AWSProfile:         "selected-profile",
			credentials.AWSAccessKeyID:     "fixture-access",
			credentials.AWSSecretAccessKey: "fixture-secret",
			credentials.AWSSessionToken:    "fixture-session",
		},
	}

	child := credentials.ResolveAWS(resolver).ChildEnvironment(parent)

	assert.Equal(t, before, parent, "building a child environment must not mutate its input")
	assertEnvEntry(t, child, "PATH", "usr/bin")
	assertEnvEntry(t, child, "HOME", "home/ksail")
	assertEnvEntry(t, child, "AWS_CONFIG_FILE", ".aws/config")
	assertEnvEntry(t, child, "AWS_PROFILE", "selected-profile")
	assertEnvEntry(t, child, "AWS_ACCESS_KEY_ID", "fixture-access")
	assertEnvEntry(t, child, "AWS_SECRET_ACCESS_KEY", "fixture-secret")
	assertEnvEntry(t, child, "AWS_SESSION_TOKEN", "fixture-session")
	assertEnvKeyCount(t, child, "AWS_PROFILE", 1)
	assertEnvKeyCount(t, child, "AWS_ACCESS_KEY_ID", 1)
	assertEnvKeyCount(t, child, "AWS_SECRET_ACCESS_KEY", 1)
	assertEnvKeyCount(t, child, "AWS_SESSION_TOKEN", 1)
	assertEnvKeyCount(t, child, "KSAIL_PROFILE", 0)
	assertEnvKeyCount(t, child, "KSAIL_ACCESS", 0)
	assertEnvKeyCount(t, child, "KSAIL_SECRET", 0)
	assertEnvKeyCount(t, child, "KSAIL_SESSION", 0)
}

// TestAWSResolution_ChildEnvironmentRemovesStaleOptionalValues verifies omitted
// optional credentials cannot survive from ambient state.
func TestAWSResolution_ChildEnvironmentRemovesStaleOptionalValues(t *testing.T) {
	t.Parallel()

	resolver := awsResolverFixture{
		envVars: map[credentials.Key]string{
			credentials.AWSProfile:      "KSAIL_PROFILE",
			credentials.AWSSessionToken: "KSAIL_SESSION",
		},
		values: map[credentials.Key]string{
			credentials.AWSProfile: "selected-profile",
		},
	}
	child := credentials.ResolveAWS(resolver).ChildEnvironment([]string{
		"PATH=/usr/bin",
		"AWS_PROFILE=stale-profile",
		"AWS_SESSION_TOKEN=stale-session",
		"KSAIL_SESSION=stale-custom-session",
	})

	assertEnvEntry(t, child, "AWS_PROFILE", "selected-profile")
	assertEnvKeyCount(t, child, "AWS_SESSION_TOKEN", 0)
	assertEnvKeyCount(t, child, "KSAIL_SESSION", 0)
}

// TestAWSResolution_CustomSourcesRemoveCompetingAmbientCredentialProviders
// verifies aliases suppress competing environment identity providers.
func TestAWSResolution_CustomSourcesRemoveCompetingAmbientCredentialProviders(t *testing.T) {
	t.Parallel()

	resolution := credentials.ResolveAWS(awsResolverFixture{
		envVars: map[credentials.Key]string{
			credentials.AWSProfile: "KSAIL_PROFILE",
		},
		values: map[credentials.Key]string{
			credentials.AWSProfile: "selected-profile",
		},
	})
	child := resolution.ChildEnvironment([]string{
		"PATH=/usr/bin",
		"AWS_DEFAULT_PROFILE=stale-default-profile",
		"AWS_ACCESS_KEY=stale-legacy-access",
		"AWS_SECRET_KEY=stale-legacy-secret",
		"AWS_WEB_IDENTITY_TOKEN_FILE=/tmp/stale-token",
		"AWS_ROLE_ARN=arn:aws:iam::123456789012:role/stale",
		"AWS_ROLE_SESSION_NAME=stale-session",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI=/v2/credentials/selected",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI=http://127.0.0.1/selected",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN=selected-token",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE=/tmp/selected-auth-token",
		"KSAIL_PROFILE=selected-profile",
	})

	assertEnvEntry(t, child, "AWS_PROFILE", "selected-profile")

	for _, key := range []string{
		"AWS_DEFAULT_PROFILE",
		"AWS_ACCESS_KEY",
		"AWS_SECRET_KEY",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_ROLE_ARN",
		"AWS_ROLE_SESSION_NAME",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
		"KSAIL_PROFILE",
	} {
		assertEnvKeyCount(t, child, key, 0)
	}

	assertEnvEntry(t, child, "PATH", "usr/bin")
}

// TestAWSResolution_ReportsCustomCredentialSourcesEvenWhenUnset verifies
// configured aliases remain fail-closed when their values are absent.
func TestAWSResolution_ReportsCustomCredentialSourcesEvenWhenUnset(t *testing.T) {
	t.Parallel()

	resolution := credentials.ResolveAWS(awsResolverFixture{
		envVars: map[credentials.Key]string{
			credentials.AWSProfile: "KSAIL_PROFILE",
		},
		values: map[credentials.Key]string{},
	})

	assert.True(t, resolution.HasCustomCredentialSources())
}

// TestOptionsForAWSResolutionMapsValuesAndCustomRequirement verifies resolved
// values and fail-closed intent reach credential consumers together.
func TestOptionsForAWSResolutionMapsValuesAndCustomRequirement(t *testing.T) {
	t.Parallel()

	type option struct {
		profile      string
		accessKeyID  string
		secretKey    string
		sessionToken string
		required     bool
	}

	resolution := credentials.ResolveAWS(awsResolverFixture{
		envVars: map[credentials.Key]string{
			credentials.AWSProfile: "KSAIL_PROFILE",
		},
		values: map[credentials.Key]string{
			credentials.AWSProfile:         "selected-profile",
			credentials.AWSAccessKeyID:     "selected-access",
			credentials.AWSSecretAccessKey: "selected-secret",
			credentials.AWSSessionToken:    "selected-session",
		},
	})
	options := credentials.OptionsForAWSResolution(
		resolution,
		func(profile, accessKeyID, secretKey, sessionToken string) option {
			return option{
				profile:      profile,
				accessKeyID:  accessKeyID,
				secretKey:    secretKey,
				sessionToken: sessionToken,
				required:     false,
			}
		},
		func() option { return option{required: true} },
	)

	require.Len(t, options, 2)
	assert.Equal(t, option{
		profile:      "selected-profile",
		accessKeyID:  "selected-access",
		secretKey:    "selected-secret",
		sessionToken: "selected-session",
		required:     false,
	}, options[0])
	assert.True(t, options[1].required)
}

// TestOptionsForAWSResolutionPreservesDefaultCredentialChain verifies canonical
// defaults do not impose an explicit credential requirement.
func TestOptionsForAWSResolutionPreservesDefaultCredentialChain(t *testing.T) {
	t.Parallel()

	resolution := credentials.ResolveAWS(awsResolverFixture{})
	options := credentials.OptionsForAWSResolution(
		resolution,
		func(_, _, _, _ string) string { return "values" },
		func() string { return "required" },
	)

	assert.Equal(t, []string{"values"}, options)
}

// TestOptionsForAWSChildEnvironmentCanonicalizesAndRequiresCustomSources
// verifies child-process isolation and its requirement stay paired.
func TestOptionsForAWSChildEnvironmentCanonicalizesAndRequiresCustomSources(t *testing.T) {
	t.Parallel()

	resolution := credentials.ResolveAWS(awsResolverFixture{
		envVars: map[credentials.Key]string{
			credentials.AWSProfile: "KSAIL_PROFILE",
		},
		values: map[credentials.Key]string{
			credentials.AWSProfile: "selected-profile",
		},
	})
	options := credentials.OptionsForAWSChildEnvironment(
		resolution,
		[]string{"PATH=/usr/bin", "AWS_PROFILE=stale-profile"},
		func(environment []string) []string { return environment },
		func() []string { return []string{"required"} },
	)

	require.Len(t, options, 2)
	assertEnvEntry(t, options[0], "PATH", "usr/bin")
	assertEnvEntry(t, options[0], "AWS_PROFILE", "selected-profile")
	assert.Equal(t, []string{"required"}, options[1])
}

// TestResolveAWSClientOptionsBuildsBothCredentialBoundaries verifies one
// snapshot configures child-process and SDK consumers consistently.
func TestResolveAWSClientOptionsBuildsBothCredentialBoundaries(t *testing.T) {
	t.Parallel()

	type environmentOption struct {
		environment []string
		required    bool
	}

	type credentialOption struct {
		profile  string
		required bool
	}

	resolution, environmentOptions, credentialOptions := credentials.ResolveAWSClientOptions(
		awsResolverFixture{
			envVars: map[credentials.Key]string{
				credentials.AWSProfile: "KSAIL_PROFILE",
			},
			values: map[credentials.Key]string{
				credentials.AWSProfile: "selected-profile",
			},
		},
		[]string{"PATH=/usr/bin", "AWS_PROFILE=stale-profile"},
		func(environment []string) environmentOption {
			return environmentOption{environment: environment}
		},
		func() environmentOption { return environmentOption{required: true} },
		func(profile, _, _, _ string) credentialOption {
			return credentialOption{profile: profile}
		},
		func() credentialOption { return credentialOption{required: true} },
	)

	assert.Equal(t, "selected-profile", resolution.Profile)
	require.Len(t, environmentOptions, 2)
	assertEnvEntry(t, environmentOptions[0].environment, "PATH", "usr/bin")
	assertEnvEntry(t, environmentOptions[0].environment, "AWS_PROFILE", "selected-profile")
	assert.True(t, environmentOptions[1].required)
	require.Len(t, credentialOptions, 2)
	assert.Equal(t, "selected-profile", credentialOptions[0].profile)
	assert.True(t, credentialOptions[1].required)
}

// TestAWSResolution_ChildEnvironmentPreservesCanonicalDefaults verifies
// canonical selection retains unrelated default-chain inputs.
func TestAWSResolution_ChildEnvironmentPreservesCanonicalDefaults(t *testing.T) {
	t.Parallel()

	resolver := awsResolverFixture{values: map[credentials.Key]string{
		credentials.AWSProfile:         "default-profile",
		credentials.AWSAccessKeyID:     "fixture-access",
		credentials.AWSSecretAccessKey: "fixture-secret",
	}}
	child := credentials.ResolveAWS(resolver).ChildEnvironment([]string{
		"AWS_PROFILE=default-profile",
		"AWS_ACCESS_KEY_ID=fixture-access",
		"AWS_SECRET_ACCESS_KEY=fixture-secret",
		"AWS_WEB_IDENTITY_TOKEN_FILE=/tmp/token",
		"AWS_ROLE_ARN=arn:aws:iam::123456789012:role/default",
		"HOME=/home/ksail",
	})

	assertEnvEntry(t, child, "AWS_PROFILE", "default-profile")
	assertEnvEntry(t, child, "AWS_ACCESS_KEY_ID", "fixture-access")
	assertEnvEntry(t, child, "AWS_SECRET_ACCESS_KEY", "fixture-secret")
	assertEnvKeyCount(t, child, "AWS_PROFILE", 1)
	assertEnvKeyCount(t, child, "AWS_ACCESS_KEY_ID", 1)
	assertEnvKeyCount(t, child, "AWS_SECRET_ACCESS_KEY", 1)
	assertEnvEntry(t, child, "AWS_WEB_IDENTITY_TOKEN_FILE", "/tmp/token")
	assertEnvEntry(t, child, "AWS_ROLE_ARN", "role/default")
}

// TestAWSResolution_ChildEnvironmentIsSafeForConcurrentInvocations verifies
// immutable resolution safely produces independent environments.
func TestAWSResolution_ChildEnvironmentIsSafeForConcurrentInvocations(t *testing.T) {
	t.Parallel()

	resolution := credentials.ResolveAWS(awsResolverFixture{
		values: map[credentials.Key]string{credentials.AWSProfile: "concurrent-profile"},
	})

	const invocations = 32

	errCh := make(chan error, invocations)

	var waitGroup sync.WaitGroup
	for index := range invocations {
		waitGroup.Go(func() {
			child := resolution.ChildEnvironment([]string{"INVOCATION=" + strconv.Itoa(index)})
			if envKeyCount(child, "AWS_PROFILE") != 1 {
				errCh <- assert.AnError
			}
		})
	}

	waitGroup.Wait()
	close(errCh)
	require.Empty(t, errCh)
}

// assertEnvEntry verifies exactly one environment entry carries the expected value fragment.
func assertEnvEntry(t *testing.T, environment []string, key, valueFragment string) {
	t.Helper()

	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found && name == key {
			assert.Contains(t, value, valueFragment)

			return
		}
	}

	assert.Fail(t, "expected environment key is missing", key)
}

// assertEnvKeyCount verifies an environment name occurs the expected number of times.
func assertEnvKeyCount(t *testing.T, environment []string, key string, expected int) {
	t.Helper()

	assert.Equal(t, expected, envKeyCount(environment, key), "unexpected count for %s", key)
}

// envKeyCount counts exact environment names independently of their values.
func envKeyCount(environment []string, key string) int {
	count := 0

	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && name == key {
			count++
		}
	}

	return count
}
