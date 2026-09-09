package eks_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	awsconfigutil "github.com/devantler-tech/ksail/v7/pkg/awsconfig"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrozenEndpointsPreserveExplicitServiceOptions(t *testing.T) {
	setEndpointVariable(t, "AWS_ENDPOINT_URL", "")
	setEndpointVariable(t, "AWS_ENDPOINT_URL_EKS", "")
	setEndpointVariable(t, "AWS_ENDPOINT_URL_STS", "")

	cfg := endpointSnapshotConfig(endpointSnapshotCase{})
	calls := 0
	overrides := map[string]int{}
	cfg.HTTPClient = endpointSnapshotHTTPFixture(
		t,
		"eks.explicit.test",
		"sts.explicit.test",
		&calls,
	)
	cfg.ServiceOptions = []func(string, any){func(_ string, options any) {
		switch value := options.(type) {
		case *awseks.Options:
			overrides["EKS"]++
			value.BaseEndpoint = aws.String("https://eks.explicit.test")
		case *sts.Options:
			overrides["STS"]++
			value.BaseEndpoint = aws.String("https://sts.explicit.test")
		case *cloudformation.Options:
			overrides["CloudFormation"]++
		}
	}}
	cfg = awsconfigutil.FreezeEndpointSources(cfg)

	t.Setenv("AWS_ENDPOINT_URL", "https://ambient.invalid")
	client, err := eksclient.NewClient(t.Context(), "us-east-1", eksclient.WithAWSConfig(cfg))
	require.NoError(t, err)
	_, err = client.DescribeCluster(t.Context(), "demo")
	require.NoError(t, err)
	_, err = client.CallerAccountID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(
		t,
		map[string]int{"EKS": 1, "STS": 1, "CloudFormation": 1},
		overrides,
		"each service callback must run once",
	)
}
