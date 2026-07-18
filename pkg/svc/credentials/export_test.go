package credentials

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// FreezeAWSResolutionForTest exposes credential-provider injection to external package tests.
func FreezeAWSResolutionForTest(
	ctx context.Context,
	region string,
	selection AWSResolution,
	loader func(context.Context, ...func(*config.LoadOptions) error) (aws.Config, error),
) (AWSResolution, error) {
	return freezeAWSResolution(ctx, region, selection, awsConfigLoader(loader))
}
