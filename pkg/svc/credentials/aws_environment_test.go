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

func (f awsResolverFixture) EnvVar(key credentials.Key) string {
	if name := f.envVars[key]; name != "" {
		return name
	}

	return credentials.DefaultEnvVar(key)
}

func (f awsResolverFixture) Value(key credentials.Key) string { return f.values[key] }

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
		"KSAIL_PROFILE",
	} {
		assertEnvKeyCount(t, child, key, 0)
	}

	assertEnvEntry(t, child, "PATH", "usr/bin")
	assertEnvEntry(t, child, "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "credentials/selected")
	assertEnvEntry(t, child, "AWS_CONTAINER_CREDENTIALS_FULL_URI", "127.0.0.1/selected")
	assertEnvEntry(t, child, "AWS_CONTAINER_AUTHORIZATION_TOKEN", "selected-token")
	assertEnvEntry(t, child, "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", "selected-auth-token")
}

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

func assertEnvKeyCount(t *testing.T, environment []string, key string, expected int) {
	t.Helper()

	assert.Equal(t, expected, envKeyCount(environment, key), "unexpected count for %s", key)
}

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
