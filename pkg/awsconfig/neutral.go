// Package awsconfig provides shared AWS SDK configuration policies.
package awsconfig

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

const frozenProfile = "__ksail_frozen__"

// LoadNeutral loads an AWS configuration with one explicit credential provider while disabling
// ambient shared profiles and credential files. The temporary profile prevents stale AWS_PROFILE
// values from redirecting the load, without mutating process environment.
func LoadNeutral(
	ctx context.Context,
	loader func(context.Context, ...func(*config.LoadOptions) error) (aws.Config, error),
	region string,
	provider aws.CredentialsProvider,
) (aws.Config, error) {
	file, err := os.CreateTemp("", "ksail-frozen-aws-config-*")
	if err != nil {
		return aws.Config{}, fmt.Errorf("create neutral AWS config: %w", err)
	}

	path := file.Name()

	defer func() { _ = os.Remove(path) }()

	_, writeErr := fmt.Fprintf(file, "[profile %s]\n", frozenProfile)
	closeErr := file.Close()

	if writeErr != nil {
		return aws.Config{}, fmt.Errorf("write neutral AWS config: %w", writeErr)
	}

	if closeErr != nil {
		return aws.Config{}, fmt.Errorf("close neutral AWS config: %w", closeErr)
	}

	return loader(
		ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(provider),
		config.WithSharedConfigProfile(frozenProfile),
		config.WithSharedConfigFiles([]string{path}),
		config.WithSharedCredentialsFiles([]string{}),
	)
}
