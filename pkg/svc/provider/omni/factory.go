package omni

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
)

// Default environment variable names for Omni credentials.
const (
	// DefaultEndpointEnvVar is the default environment variable
	// name for the Omni API endpoint URL.
	DefaultEndpointEnvVar = "OMNI_ENDPOINT"
	// DefaultServiceAccountKeyEnvVar is the default environment variable
	// name for the Omni service account key used for API authentication.
	DefaultServiceAccountKeyEnvVar = "OMNI_SERVICE_ACCOUNT_KEY"
)

// NewProviderFromOptions creates an Omni provider from the given options.
// It resolves the endpoint and service account key from environment variables
// (using the configured env var names or defaults), then creates an authenticated
// Omni client.
//
// This function is used by both the Talos provisioner factory and the centralized
// kubeconfig refresh hook.
func NewProviderFromOptions(opts v1alpha1.OptionsOmni) (*Provider, error) {
	endpointEnvVar := opts.EndpointEnvVar
	if endpointEnvVar == "" {
		endpointEnvVar = DefaultEndpointEnvVar
	}

	endpoint := os.Getenv(endpointEnvVar)
	if endpoint == "" {
		endpoint = opts.Endpoint
	}

	if endpoint == "" {
		return nil, fmt.Errorf(
			"%w: set via environment variable %s or spec.cluster.omni.endpoint in config",
			ErrEndpointRequired,
			endpointEnvVar,
		)
	}

	keyEnvVar := opts.ServiceAccountKeyEnvVar
	if keyEnvVar == "" {
		keyEnvVar = DefaultServiceAccountKeyEnvVar
	}

	serviceAccountKey := os.Getenv(keyEnvVar)
	if serviceAccountKey == "" {
		return nil, fmt.Errorf(
			"%w: environment variable %s is not set",
			ErrServiceAccountKeyRequired,
			keyEnvVar,
		)
	}

	client, err := omniclient.New(
		endpoint,
		omniclient.WithServiceAccount(serviceAccountKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Omni client: %w", err)
	}

	return NewProvider(client), nil
}
