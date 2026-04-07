package omni_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProviderFromOptions(t *testing.T) {
	// Not parallel: EnvVarTakesPrecedenceOverConfig uses t.Setenv.

	t.Run("MissingEndpoint_NoEnvVar_NoConfig", func(t *testing.T) {
		t.Parallel()

		// Use a custom env var name that won't be set in any environment.
		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar: "TEST_KSAIL_OMNI_ENDPOINT_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.Error(t, err)
		assert.ErrorIs(t, err, omni.ErrEndpointRequired)
	})

	t.Run("MissingEndpoint_FallsBackToConfigEndpoint", func(t *testing.T) {
		t.Parallel()

		// Use a custom env var name that won't be set, but provide a config endpoint.
		// This will pass the endpoint check but fail on the service account key.
		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar:          "TEST_KSAIL_OMNI_ENDPOINT_FALLBACK",
			Endpoint:                "https://omni.example.com",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_OMNI_KEY_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.Error(t, err)
		assert.ErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
	})

	t.Run("MissingServiceAccountKey", func(t *testing.T) {
		t.Parallel()

		opts := v1alpha1.OptionsOmni{
			Endpoint:                "https://omni.example.com",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_OMNI_KEY_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.Error(t, err)
		assert.ErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
	})

	t.Run("DefaultEnvVarNames", func(t *testing.T) {
		t.Parallel()

		// With empty env var names, the defaults should be used.
		// Since default env vars are unlikely to be set in test env,
		// this should fail with ErrEndpointRequired.
		opts := v1alpha1.OptionsOmni{}

		_, err := omni.NewProviderFromOptions(opts)

		require.Error(t, err)
		assert.ErrorIs(t, err, omni.ErrEndpointRequired)
	})

	t.Run("CustomEnvVarNames", func(t *testing.T) {
		t.Parallel()

		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar:          "TEST_KSAIL_CUSTOM_ENDPOINT",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_CUSTOM_KEY",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.Error(t, err)
		// Should use the custom env var name in the error.
		assert.ErrorIs(t, err, omni.ErrEndpointRequired)
		assert.Contains(t, err.Error(), "TEST_KSAIL_CUSTOM_ENDPOINT")
	})

	t.Run("EnvVarTakesPrecedenceOverConfig", func(t *testing.T) {
		// Cannot use t.Parallel() with t.Setenv.
		t.Setenv("TEST_KSAIL_OMNI_EP_PREC", "https://env-endpoint.example.com")

		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar:          "TEST_KSAIL_OMNI_EP_PREC",
			Endpoint:                "https://config-endpoint.example.com",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_OMNI_KEY_PREC_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.Error(t, err)
		// Should get past endpoint resolution and fail on key.
		assert.ErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
	})
}
