package omni_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProviderFromOptions_ErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("MissingEndpoint_NoEnvVar_NoConfig", func(t *testing.T) {
		t.Parallel()

		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar: "TEST_KSAIL_OMNI_ENDPOINT_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.ErrorIs(t, err, omni.ErrEndpointRequired)
	})

	t.Run("MissingEndpoint_FallsBackToConfigEndpoint", func(t *testing.T) {
		t.Parallel()

		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar:          "TEST_KSAIL_OMNI_ENDPOINT_FALLBACK",
			Endpoint:                "https://omni.example.com",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_OMNI_KEY_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.ErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
	})

	t.Run("MissingServiceAccountKey", func(t *testing.T) {
		t.Parallel()

		opts := v1alpha1.OptionsOmni{
			Endpoint:                "https://omni.example.com",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_OMNI_KEY_MISSING",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.ErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
	})
}

// TestNewProviderFromOptions_DefaultEnvVarNames is a separate, non-parallel
// test so that t.Setenv can clear the default env vars. This prevents ambient
// environment state (e.g. OMNI_ENDPOINT set on a developer machine) from
// causing a false failure.
func TestNewProviderFromOptions_DefaultEnvVarNames(t *testing.T) {
	t.Setenv("OMNI_ENDPOINT", "")
	t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", "")

	opts := v1alpha1.OptionsOmni{}

	_, err := omni.NewProviderFromOptions(opts)

	require.ErrorIs(t, err, omni.ErrEndpointRequired)
}

func TestNewProviderFromOptions_EnvVarResolution(t *testing.T) {
	t.Parallel()

	t.Run("CustomEnvVarNames", func(t *testing.T) {
		t.Parallel()

		opts := v1alpha1.OptionsOmni{
			EndpointEnvVar:          "TEST_KSAIL_CUSTOM_ENDPOINT",
			ServiceAccountKeyEnvVar: "TEST_KSAIL_CUSTOM_KEY",
		}

		_, err := omni.NewProviderFromOptions(opts)

		require.ErrorIs(t, err, omni.ErrEndpointRequired)
		assert.Contains(t, err.Error(), "TEST_KSAIL_CUSTOM_ENDPOINT")
	})
}

// TestNewProviderFromOptions_EnvVarPrecedence is a separate, non-parallel test
// because t.Setenv is incompatible with t.Parallel on the parent.
func TestNewProviderFromOptions_EnvVarPrecedence(t *testing.T) {
	t.Setenv("TEST_KSAIL_OMNI_EP_PREC", "https://env-endpoint.example.com")

	opts := v1alpha1.OptionsOmni{
		EndpointEnvVar:          "TEST_KSAIL_OMNI_EP_PREC",
		Endpoint:                "https://config-endpoint.example.com",
		ServiceAccountKeyEnvVar: "TEST_KSAIL_OMNI_KEY_PREC_MISSING",
	}

	_, err := omni.NewProviderFromOptions(opts)

	require.ErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
}
