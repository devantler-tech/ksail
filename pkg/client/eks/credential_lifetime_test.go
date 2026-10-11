package eks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/require"
)

var errUpgradeCredentialProvider = errors.New("credential provider unavailable")

// TestUpgradeCredentialLifetimeBoundary exercises the strict one-minute expiry
// margin and rejects incomplete credentials through real SDK client options.
func TestUpgradeCredentialLifetimeBoundary(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(10 * time.Minute)
	for _, testCase := range []struct {
		name    string
		values  aws.Credentials
		wantErr bool
	}{
		{name: "permanent", values: aws.Credentials{AccessKeyID: "test", SecretAccessKey: "secret"}},
		{name: "exact_margin", values: aws.Credentials{
			AccessKeyID: "test", SecretAccessKey: "secret", CanExpire: true, Expires: deadline.Add(time.Minute),
		}, wantErr: true},
		{name: "after_margin", values: aws.Credentials{
			AccessKeyID: "test", SecretAccessKey: "secret", CanExpire: true,
			Expires: deadline.Add(time.Minute + time.Second),
		}},
		{name: "missing_access", values: aws.Credentials{SecretAccessKey: "secret"}, wantErr: true},
		{name: "missing_secret", values: aws.Credentials{AccessKeyID: "test"}, wantErr: true},
		{name: "missing_expiry", values: aws.Credentials{
			AccessKeyID: "test", SecretAccessKey: "secret", CanExpire: true,
		}, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithDeadline(t.Context(), deadline)
			defer cancel()

			client, err := eksclient.NewClient(ctx, "us-east-1", eksclient.WithAWSConfig(aws.Config{
				Credentials: aws.CredentialsProviderFunc(
					func(context.Context) (aws.Credentials, error) { return testCase.values, nil },
				),
			}))
			require.NoError(t, err)

			err = client.ValidateUpgradeCredentialLifetime(ctx)
			if testCase.wantErr {
				require.ErrorIs(t, err, eksclient.ErrUpgradeCredentialLifetime)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUpgradeCredentialLifetimePreservesProviderErrors keeps provider failures and
// canceled requests distinguishable from a session with insufficient lifetime.
func TestUpgradeCredentialLifetimePreservesProviderErrors(t *testing.T) {
	t.Parallel()

	client, err := eksclient.NewClient(t.Context(), "us-east-1", eksclient.WithAWSConfig(aws.Config{
		Credentials: aws.CredentialsProviderFunc(
			func(context.Context) (aws.Credentials, error) { return aws.Credentials{}, errUpgradeCredentialProvider },
		),
	}))
	require.NoError(t, err)
	require.ErrorIs(
		t,
		client.ValidateUpgradeCredentialLifetime(t.Context()),
		errUpgradeCredentialProvider,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, client.ValidateUpgradeCredentialLifetime(ctx), context.Canceled)
}
