package awslbcontrollerinstaller_test

import (
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		region      string
		wantErr     error
		description string
	}{
		{
			name: "creates installer with cluster name", clusterName: "prod-eks",
			description: "A named cluster is the one required input",
		},
		{
			name: "creates installer with cluster name and region", clusterName: "prod-eks",
			region:      "eu-north-1",
			description: "Region is optional and accepted",
		},
		{
			name: "rejects empty cluster name", clusterName: "",
			wantErr:     awslbcontrollerinstaller.ErrClusterNameRequired,
			description: "The chart's clusterName value is required; there is no safe default",
		},
		{
			name: "rejects whitespace-only cluster name", clusterName: "   ",
			wantErr:     awslbcontrollerinstaller.ErrClusterNameRequired,
			description: "Whitespace must not smuggle past the required-name check",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			installer, err := awslbcontrollerinstaller.NewInstaller(
				mockClient, 5*time.Minute, testCase.clusterName, testCase.region, false,
			)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr, testCase.description)
				assert.Nil(t, installer, testCase.description)
			} else {
				require.NoError(t, err, testCase.description)
				require.NotNil(t, installer, testCase.description)
			}
		})
	}
}

func TestNewInstaller_HAEnabled(t *testing.T) {
	t.Parallel()

	mockClient := helm.NewMockInterface(t)
	installer, err := awslbcontrollerinstaller.NewInstaller(
		mockClient, 5*time.Minute, "prod-eks", "eu-north-1", true,
	)

	require.NoError(t, err)
	require.NotNil(t, installer)
}

func TestBuildValuesYaml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		region      string
		haEnabled   bool
		want        string
	}{
		{
			name: "cluster name only", clusterName: "prod-eks",
			want: "clusterName: prod-eks\nenableServiceMutatorWebhook: false\nreplicaCount: 1",
		},
		{
			name:        "cluster name and region",
			clusterName: "prod-eks",
			region:      "eu-north-1",
			want:        "clusterName: prod-eks\nenableServiceMutatorWebhook: false\nregion: eu-north-1\nreplicaCount: 1",
		},
		{
			name: "ha enabled", clusterName: "prod-eks", haEnabled: true,
			want: "clusterName: prod-eks\nenableServiceMutatorWebhook: false\nreplicaCount: 2",
		},
		{
			name:        "ha enabled with region",
			clusterName: "prod-eks",
			region:      "eu-north-1",
			haEnabled:   true,
			want:        "clusterName: prod-eks\nenableServiceMutatorWebhook: false\nregion: eu-north-1\nreplicaCount: 2",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := awslbcontrollerinstaller.BuildValuesYamlForTest(
				testCase.clusterName, testCase.region, testCase.haEnabled,
			)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestChartVersion(t *testing.T) {
	t.Parallel()

	version := awslbcontrollerinstaller.ChartVersionForTest()

	assert.NotEmpty(t, version, "the embedded Chart.yaml pin must parse to a version")
	assert.NotEqual(t, "0.0.0", version,
		"the parser must return the dependency pin, not the wrapper chart's own version")
	assert.False(t, strings.HasPrefix(version, "v"),
		"Helm chart versions are unprefixed")
}
