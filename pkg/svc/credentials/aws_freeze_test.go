package credentials_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFrozenCredentialQuery = errors.New("credential query failed")

type frozenResolverFixture struct {
	envVars map[credentials.Key]string
	values  map[credentials.Key]string
}

func (f frozenResolverFixture) EnvVar(key credentials.Key) string {
	if name := f.envVars[key]; name != "" {
		return name
	}

	return credentials.DefaultEnvVar(key)
}

func (f frozenResolverFixture) Value(key credentials.Key) string { return f.values[key] }

type rotatingCredentialProvider struct {
	credentials []aws.Credentials
	calls       int
	err         error
}

func (p *rotatingCredentialProvider) Retrieve(context.Context) (aws.Credentials, error) {
	if p.err != nil {
		return aws.Credentials{}, p.err
	}

	credential := p.credentials[p.calls]
	p.calls++

	return credential, nil
}

func frozenCredential(accessKeyID, secretAccessKey, sessionToken string) aws.Credentials {
	return aws.Credentials{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    sessionToken,
	}
}

func TestResolveFrozenAWSConvertsStaticSelectionToConcreteSnapshot(t *testing.T) {
	t.Parallel()

	resolution, err := credentials.ResolveFrozenAWS(
		t.Context(),
		frozenResolverFixture{values: map[credentials.Key]string{
			credentials.AWSProfile:         "ignored-profile",
			credentials.AWSAccessKeyID:     "selected-access",
			credentials.AWSSecretAccessKey: "selected-secret",
			credentials.AWSSessionToken:    "selected-session",
		}},
		"eu-north-1",
	)
	require.NoError(t, err)
	assert.True(t, resolution.IsFrozen())
	assert.Empty(t, resolution.Profile)
	assert.Equal(t, "selected-access", resolution.AccessKeyID)
	assert.Equal(t, "selected-secret", resolution.SecretAccessKey)
	assert.Equal(t, "selected-session", resolution.SessionToken)
}

// TestFreezeAWSStaticSelectionPreservesProfileRegion verifies complete static credentials replace
// only the identity provider. A valid selected profile may still supply non-credential settings,
// including the region that both the SDK and eksctl mutation must share.
func TestFreezeAWSStaticSelectionPreservesProfileRegion(t *testing.T) {
	t.Parallel()

	selection := credentials.ResolveAWS(frozenResolverFixture{values: map[credentials.Key]string{
		credentials.AWSProfile:         "region-profile",
		credentials.AWSAccessKeyID:     "selected-access",
		credentials.AWSSecretAccessKey: "selected-secret",
	}})
	loaderCalls := 0

	frozen, err := credentials.FreezeAWSResolutionForTest(
		t.Context(),
		"",
		selection,
		func(context.Context, ...func(*config.LoadOptions) error) (aws.Config, error) {
			loaderCalls++

			return aws.Config{Region: "ap-southeast-2"}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, loaderCalls)
	assert.Equal(t, "ap-southeast-2", frozen.Region)
	assert.Empty(t, frozen.Profile)
	assert.Equal(t, "selected-access", frozen.AccessKeyID)
	assert.Equal(t, "selected-secret", frozen.SecretAccessKey)
}

func TestResolveFrozenAWSStaticSelectionPreservesDefaultRegion(t *testing.T) {
	// Not parallel: t.Setenv changes the process environment.
	missingConfigDir := t.TempDir()
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(missingConfigDir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(missingConfigDir, "missing-credentials"))
	t.Setenv("AWS_ACCESS_KEY_ID", "selected-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "selected-secret")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "ca-central-1")

	frozen, err := credentials.ResolveFrozenAWS(t.Context(), nil, "")
	require.NoError(t, err)
	assert.Equal(t, "ca-central-1", frozen.Region)
	assert.Contains(t, frozen.ChildEnvironment(nil), "AWS_REGION=ca-central-1")
	assert.NotContains(t, frozen.ChildEnvironment(nil), "AWS_DEFAULT_REGION=ca-central-1")
}

// TestFrozenAWSConfigOptionPreservesNonIdentitySettings verifies the SDK snapshot retains resolved
// transport and endpoint behavior while removing unrelated ambient provider material and pinning
// the selected concrete credential tuple.
func TestFrozenAWSConfigOptionPreservesNonIdentitySettings(t *testing.T) {
	t.Parallel()

	selection := credentials.ResolveAWS(frozenResolverFixture{values: map[credentials.Key]string{
		credentials.AWSAccessKeyID:     "selected-access",
		credentials.AWSSecretAccessKey: "selected-secret",
	}})
	baseEndpoint := "https://aws.fixture.invalid"
	profileCredentials := frozenCredential("profile-access", "", "")

	frozen, err := credentials.FreezeAWSResolutionForTest(
		t.Context(),
		"eu-north-1",
		selection,
		func(context.Context, ...func(*config.LoadOptions) error) (aws.Config, error) {
			return aws.Config{
				HTTPClient:   http.DefaultClient,
				BaseEndpoint: aws.String(baseEndpoint),
				ConfigSources: []any{
					config.EnvConfig{
						Credentials:  frozenCredential("ambient-access", "ambient-secret", ""),
						BaseEndpoint: baseEndpoint,
					},
					//nolint:gosec // synthetic credentials verify identity fields are scrubbed
					config.SharedConfig{
						Credentials:       profileCredentials,
						CredentialProcess: "ambient-credential-process",
						BaseEndpoint:      baseEndpoint,
					},
				},
			}, nil
		},
	)
	require.NoError(t, err)

	options := credentials.OptionsForFrozenAWSConfig(
		frozen,
		func(config aws.Config) aws.Config { return config },
		func(_, _, _, _ string) aws.Config { return aws.Config{} },
		func() aws.Config { return aws.Config{} },
	)
	require.Len(t, options, 2)
	assert.Same(t, http.DefaultClient, options[0].HTTPClient)
	assert.Equal(t, baseEndpoint, aws.ToString(options[0].BaseEndpoint))

	selected, err := options[0].Credentials.Retrieve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "selected-access", selected.AccessKeyID)
	assert.Equal(t, "selected-secret", selected.SecretAccessKey)

	environmentSource, isEnvironmentConfig := options[0].ConfigSources[0].(config.EnvConfig)
	require.True(t, isEnvironmentConfig)
	assert.Empty(t, environmentSource.Credentials.AccessKeyID)
	assert.Equal(t, baseEndpoint, environmentSource.BaseEndpoint)

	sharedSource, isSharedConfig := options[0].ConfigSources[1].(config.SharedConfig)
	require.True(t, isSharedConfig)
	assert.Empty(t, sharedSource.Credentials.AccessKeyID)
	assert.Empty(t, sharedSource.CredentialProcess)
	assert.Equal(t, baseEndpoint, sharedSource.BaseEndpoint)
}

func TestFreezeAWSResolutionRetrievesProviderOnlyOnce(t *testing.T) {
	t.Parallel()

	provider := &rotatingCredentialProvider{credentials: []aws.Credentials{
		frozenCredential(
			"account-a-access",
			"account-a-secret",
			"account-a-session",
		),
		frozenCredential(
			"account-b-access",
			"account-b-secret",
			"account-b-session",
		),
	}}
	selection := credentials.ResolveAWS(frozenResolverFixture{
		envVars: map[credentials.Key]string{credentials.AWSProfile: "KSAIL_PROFILE"},
		values:  map[credentials.Key]string{credentials.AWSProfile: "rotating-profile"},
	})

	frozen, err := credentials.FreezeAWSResolutionForTest(
		t.Context(),
		"",
		selection,
		func(context.Context, ...func(*config.LoadOptions) error) (aws.Config, error) {
			return aws.Config{Region: "eu-west-3", Credentials: provider}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, provider.calls)
	assert.Equal(t, "eu-west-3", frozen.Region)

	child := frozen.ChildEnvironment([]string{
		"AWS_PROFILE=rotating-profile",
		"KSAIL_PROFILE=rotating-profile",
		"AWS_WEB_IDENTITY_TOKEN_FILE=/tmp/rotating-token",
		"AWS_REGION=stale-region",
	})
	assert.Contains(t, child, "AWS_ACCESS_KEY_ID=account-a-access")
	assert.Contains(t, child, "AWS_SECRET_ACCESS_KEY=account-a-secret")
	assert.Contains(t, child, "AWS_SESSION_TOKEN=account-a-session")
	assert.NotContains(t, child, "AWS_PROFILE=rotating-profile")
	assert.NotContains(t, child, "KSAIL_PROFILE=rotating-profile")
	assert.NotContains(t, child, "AWS_WEB_IDENTITY_TOKEN_FILE=/tmp/rotating-token")
	assert.Contains(t, child, "AWS_REGION=eu-west-3")
	assert.NotContains(t, child, "AWS_REGION=stale-region")
	assert.Equal(t, 1, provider.calls, "rendering mutation options must not re-resolve credentials")
}

func TestResolveFrozenAWSFailsClosedForInvalidOrUnavailableSelection(t *testing.T) {
	t.Parallel()

	t.Run("partial static tuple", func(t *testing.T) {
		t.Parallel()

		_, err := credentials.ResolveFrozenAWS(
			t.Context(),
			frozenResolverFixture{values: map[credentials.Key]string{
				credentials.AWSAccessKeyID: "access-only",
			}},
			"eu-north-1",
		)
		require.ErrorIs(t, err, credentials.ErrIncompleteAWSStaticCredentials)
	})

	t.Run("custom alias unavailable", func(t *testing.T) {
		t.Parallel()

		_, err := credentials.ResolveFrozenAWS(
			t.Context(),
			frozenResolverFixture{
				envVars: map[credentials.Key]string{credentials.AWSProfile: "KSAIL_PROFILE"},
				values:  map[credentials.Key]string{},
			},
			"eu-north-1",
		)
		require.ErrorIs(t, err, credentials.ErrExplicitAWSCredentialsUnavailable)
	})

	t.Run("provider query failure", func(t *testing.T) {
		t.Parallel()

		selection := credentials.ResolveAWS(frozenResolverFixture{})
		_, err := credentials.FreezeAWSResolutionForTest(
			t.Context(),
			"eu-north-1",
			selection,
			func(context.Context, ...func(*config.LoadOptions) error) (aws.Config, error) {
				return aws.Config{
					Credentials: &rotatingCredentialProvider{err: errFrozenCredentialQuery},
				}, nil
			},
		)
		require.ErrorIs(t, err, errFrozenCredentialQuery)
	})
}
