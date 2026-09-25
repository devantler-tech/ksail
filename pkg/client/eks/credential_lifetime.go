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

	eksValues, err := validateCredentialLifetime(ctx, eksOptions.Options().Credentials)
	if err != nil {
		return fmt.Errorf("validate EKS upgrade credentials: %w", err)
	}

	stsValues, err := validateCredentialLifetime(ctx, stsOptions.Options().Credentials)
	if err != nil {
		return fmt.Errorf("validate STS upgrade credentials: %w", err)
	}

	// Sign every later call with the values just validated, not a fresh retrieval.
	c.upgradeEKS = frozenCredentials(eksValues)
	c.upgradeSTS = frozenCredentials(stsValues)

	return nil
}

// frozenCredentials returns a provider that always yields exactly these values.
func frozenCredentials(values aws.Credentials) aws.CredentialsProvider {
	return aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return values, nil
	})
}

// eksOptions pins validated upgrade credentials onto an EKS call once they exist.
func (c *Client) eksOptions() []func(*awseks.Options) {
	if c.upgradeEKS == nil {
		return nil
	}

	provider := c.upgradeEKS

	return []func(*awseks.Options){func(options *awseks.Options) { options.Credentials = provider }}
}

// stsOptions pins validated upgrade credentials onto an STS call once they exist.
func (c *Client) stsOptions() []func(*sts.Options) {
	if c.upgradeSTS == nil {
		return nil
	}

	provider := c.upgradeSTS

	return []func(*sts.Options){func(options *sts.Options) { options.Credentials = provider }}
}

// validateCredentialLifetime checks one effective provider and preserves retrieval
// and cancellation errors before allowing an irreversible upgrade submission.
func validateCredentialLifetime(
	ctx context.Context, provider aws.CredentialsProvider,
) (aws.Credentials, error) {
	err := ctx.Err()
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("validate upgrade credential lifetime: %w", err)
	}

	if provider == nil {
		return aws.Credentials{}, ErrUpgradeCredentialLifetime
	}

	values, err := provider.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("read upgrade credentials: %w", err)
	}

	if !credentialsCoverUpgradeWait(ctx, values) {
		return aws.Credentials{}, ErrUpgradeCredentialLifetime
	}

	err = ctx.Err()
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("validate upgrade credential lifetime: %w", err)
	}

	return values, nil
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
