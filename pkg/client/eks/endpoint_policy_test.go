package eks_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	awsconfigutil "github.com/devantler-tech/ksail/v7/pkg/awsconfig"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFrozenEndpointIgnorePolicyUsesLoadedConfig preserves the SDK's distinction
// between ignored environment endpoints and deliberate programmatic overrides.
func TestFrozenEndpointIgnorePolicyUsesLoadedConfig(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "true")
	setEndpointVariable(t, "AWS_ENDPOINT_URL_EKS", "")
	setEndpointVariable(t, "AWS_ENDPOINT_URL_STS", "")

	for _, explicit := range []bool{false, true} {
		t.Setenv("AWS_ENDPOINT_URL", "https://ignored-global.test")
		cfg, err := config.LoadDefaultConfig(
			t.Context(),
			config.WithRegion("us-east-1"),
			config.WithSharedConfigFiles([]string{}),
			config.WithSharedCredentialsFiles([]string{}),
			config.WithCredentialsProvider(
				awscredentials.NewStaticCredentialsProvider("FROZENENDPOINT", "secret", ""),
			),
		)
		require.NoError(t, err)
		require.Nil(
			t,
			cfg.BaseEndpoint,
			"the SDK loader must apply the ignore policy before freezing",
		)

		wantEKS, wantSTS := "eks.us-east-1.amazonaws.com", "sts.us-east-1.amazonaws.com"

		if explicit {
			cfg.BaseEndpoint = aws.String("https://explicit-base.test")
			wantEKS, wantSTS = "explicit-base.test", "explicit-base.test"
		}

		calls := 0
		cfg.HTTPClient = endpointSnapshotHTTPFixture(t, wantEKS, wantSTS, &calls)
		cfg = awsconfigutil.FreezeEndpointSources(cfg)

		t.Setenv("AWS_ENDPOINT_URL", "https://later-global.invalid")
		client, err := eksclient.NewClient(t.Context(), "us-east-1", eksclient.WithAWSConfig(cfg))
		require.NoError(t, err)
		_, err = client.DescribeCluster(t.Context(), "demo")
		require.NoError(t, err)
		_, err = client.CallerAccountID(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
	}
}
