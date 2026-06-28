package hetzner

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ErrTokenRequired indicates that the Hetzner API token environment variable is
// not set, so no authenticated client can be constructed.
var ErrTokenRequired = errors.New("hetzner API token not set")

// NewProviderFromOptions creates a Hetzner provider from the given options.
// It resolves the API token from the configured token environment variable
// (falling back to DefaultHetznerTokenEnvVar when unset), then creates an
// authenticated Hetzner client.
//
// Unlike the Omni equivalent, this returns the underlying *hcloud.Client as well
// so callers that need direct API access — such as the Talos provisioner wiring
// the SnapshotManager — can reuse the same authenticated client.
//
// This function is the credential-resolution path used by the Talos provisioner
// factory, so a custom spec.provider.hetzner.tokenEnvVar is honored there. The
// cluster info/diagnose status helpers still read HCLOUD_TOKEN directly and do
// not yet route through here (tracked separately); they honor only the default
// env var.
func NewProviderFromOptions(opts v1alpha1.OptionsHetzner) (*Provider, *hcloud.Client, error) {
	tokenEnvVar := opts.TokenEnvVar
	if tokenEnvVar == "" {
		tokenEnvVar = v1alpha1.DefaultHetznerTokenEnvVar
	}

	token := os.Getenv(tokenEnvVar)
	if token == "" {
		return nil, nil, fmt.Errorf(
			"%w: environment variable %s is not set",
			ErrTokenRequired,
			tokenEnvVar,
		)
	}

	client := hcloud.NewClient(hcloud.WithToken(token))

	return NewProvider(client), client, nil
}

// ValidateCredentials verifies the configured Hetzner token authenticates by making one cheap
// authenticated API call (listing locations). It returns ErrTokenRequired when the token is unset,
// or the API error when the call fails; nil means the credentials work.
func ValidateCredentials(ctx context.Context, opts v1alpha1.OptionsHetzner) error {
	_, client, err := NewProviderFromOptions(opts)
	if err != nil {
		return err
	}

	_, err = client.Location.All(ctx)
	if err != nil {
		return fmt.Errorf("hetzner API request failed: %w", err)
	}

	return nil
}
