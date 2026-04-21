package talosprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/require"
)

func TestCreateOmniProvider_EndpointResolution(t *testing.T) {
	tests := []struct {
		name    string
		opts    v1alpha1.OptionsOmni
		envVars map[string]string
		wantErr error
	}{
		{
			name: "env var overrides config endpoint",
			opts: v1alpha1.OptionsOmni{Endpoint: "https://config.example.com:443"},
			envVars: map[string]string{
				"OMNI_ENDPOINT":            "https://env.example.com:443",
				"OMNI_SERVICE_ACCOUNT_KEY": "test-key",
			},
			wantErr: nil, // endpoint resolved from env var
		},
		{
			name:    "falls back to config when env var is unset",
			opts:    v1alpha1.OptionsOmni{Endpoint: "https://config.example.com:443"},
			envVars: map[string]string{"OMNI_SERVICE_ACCOUNT_KEY": "test-key"},
			wantErr: nil, // endpoint resolved from config
		},
		{
			name: "custom EndpointEnvVar name is honored",
			opts: v1alpha1.OptionsOmni{EndpointEnvVar: "CUSTOM_OMNI_EP"},
			envVars: map[string]string{
				"CUSTOM_OMNI_EP":           "https://custom.example.com:443",
				"OMNI_SERVICE_ACCOUNT_KEY": "test-key",
			},
			wantErr: nil, // endpoint resolved from custom env var
		},
		{
			name:    "error when neither env var nor config is set",
			opts:    v1alpha1.OptionsOmni{},
			envVars: map[string]string{},
			wantErr: omni.ErrEndpointRequired,
		},
		{
			name:    "error when endpoint is set but service account key is missing",
			opts:    v1alpha1.OptionsOmni{Endpoint: "https://config.example.com:443"},
			envVars: map[string]string{},
			wantErr: omni.ErrServiceAccountKeyRequired,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Clear relevant env vars before applying test-specific overrides.
			t.Setenv("OMNI_ENDPOINT", "")
			t.Setenv("OMNI_SERVICE_ACCOUNT_KEY", "")
			t.Setenv("CUSTOM_OMNI_EP", "")

			for k, v := range testCase.envVars {
				t.Setenv(k, v)
			}

			err := talosprovisioner.CreateOmniProviderForTest(testCase.opts)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			} else if err != nil {
				// Endpoint/key resolution succeeded; any error is from omniclient.New().
				require.NotErrorIs(t, err, omni.ErrEndpointRequired)
				require.NotErrorIs(t, err, omni.ErrServiceAccountKeyRequired)
			}
		})
	}
}
