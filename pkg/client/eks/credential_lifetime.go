package eks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// ErrUpgradeCredentialLifetime reports credentials that cannot safely cover an accepted upgrade.
var ErrUpgradeCredentialLifetime = errors.New(
	"EKS upgrade credentials must remain valid through the bounded wait plus one minute; " +
		"use a credential provider with a sufficient known session expiry",
)

// ValidateUpgradeCredentialLifetime checks the credentials actually selected by EKS and STS
// after all service options have run. A detached configuration's provider cannot establish
// the lifetime of either service's credentials. Injected clients must expose their SDK options.
func (c *Client) ValidateUpgradeCredentialLifetime(ctx context.Context) error {
	eksOptions, eksOK := c.describer.(interface{ Options() awseks.Options })

	stsOptions, stsOK := c.identityGetter.(interface{ Options() sts.Options })
	if !eksOK || !stsOK {
		return ErrUpgradeCredentialLifetime
	}

	for _, service := range []struct {
		name     string
		provider aws.CredentialsProvider
	}{
		{"EKS", eksOptions.Options().Credentials},
		{"STS", stsOptions.Options().Credentials},
	} {
		err := validateCredentialLifetime(ctx, service.provider)
		if err != nil {
			return fmt.Errorf("validate %s upgrade credentials: %w", service.name, err)
		}
	}

	return nil
}

func validateCredentialLifetime(ctx context.Context, provider aws.CredentialsProvider) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("validate upgrade credential lifetime: %w", err)
	}

	if provider == nil {
		return ErrUpgradeCredentialLifetime
	}

	values, err := provider.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("read upgrade credentials: %w", err)
	}

	if !credentialsCoverUpgradeWait(ctx, values) {
		return ErrUpgradeCredentialLifetime
	}

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("validate upgrade credential lifetime: %w", err)
	}

	return nil
}

// credentialsCoverUpgradeWait requires known expiry for temporary sessions and
// leaves a one-minute margin beyond the caller's bounded upgrade deadline.
func credentialsCoverUpgradeWait(ctx context.Context, values aws.Credentials) bool {
	if values.AccessKeyID == "" || values.SecretAccessKey == "" {
		return false
	}

	if !values.CanExpire {
		return values.SessionToken == ""
	}

	deadline, ok := ctx.Deadline()

	return ok && values.Expires.After(deadline.Add(time.Minute))
}
