package awslbcontrollerinstaller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	awslbcontrollerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/awslbcontroller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type newInstallerCase struct {
	name           string
	clusterName    string
	region         string
	serviceAccount string
	wantErr        error
	description    string
}

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	tests := []newInstallerCase{
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

	runNewInstallerCases(t, tests)
}

func TestNewInstaller_ServiceAccount(t *testing.T) {
	t.Parallel()

	tests := []newInstallerCase{
		{
			name: "creates installer with pre-created service account", clusterName: "prod-eks",
			serviceAccount: "aws-load-balancer-controller",
			description:    "A valid pre-created IRSA service account name is accepted",
		},
		{
			name: "treats whitespace-only service account as unset", clusterName: "prod-eks",
			serviceAccount: "   ",
			description:    "Whitespace-only means unset: the chart keeps creating its own SA",
		},
		{
			name:           "rejects invalid service account name",
			clusterName:    "prod-eks",
			serviceAccount: "Not_A_Valid_SA!",
			wantErr:        awslbcontrollerinstaller.ErrInvalidServiceAccountName,
			description:    "A non-DNS-1123-subdomain SA name must fail loud, not reach Helm values",
		},
		{
			name: "rejects service account name with newline", clusterName: "prod-eks",
			serviceAccount: "sa\ninjected: true",
			wantErr:        awslbcontrollerinstaller.ErrInvalidServiceAccountName,
			description:    "A newline-bearing name must never be interpolated into values YAML",
		},
	}

	runNewInstallerCases(t, tests)
}

func runNewInstallerCases(t *testing.T, tests []newInstallerCase) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			installer, err := awslbcontrollerinstaller.NewInstaller(
				mockClient, 5*time.Minute,
				testCase.clusterName, testCase.region, testCase.serviceAccount, false,
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
		mockClient, 5*time.Minute, "prod-eks", "eu-north-1", "", true,
	)

	require.NoError(t, err)
	require.NotNil(t, installer)
}

func TestUninstallPreservesGitOpsOwnership(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "aws-load-balancer-controller", "kube-system").
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": "aws-load-balancer-controller"}, nil)
	installer, err := awslbcontrollerinstaller.NewInstaller(
		client, 5*time.Minute, "prod-eks", "eu-north-1", "", false, true,
	)
	require.NoError(t, err)

	err = installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestIsGitOpsManagedReportsFluxOwnership(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "aws-load-balancer-controller", "kube-system").
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": "aws-load-balancer-controller"}, nil)
	installer, err := awslbcontrollerinstaller.NewInstaller(
		client, 5*time.Minute, "prod-eks", "eu-north-1", "", false,
	)
	require.NoError(t, err)

	managed, err := installer.IsGitOpsManaged(context.Background())

	require.NoError(t, err)
	assert.True(t, managed)
}

func TestUninstallAllowsKSailOwnedRelease(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "aws-load-balancer-controller", "kube-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		UninstallRelease(mock.Anything, "aws-load-balancer-controller", "kube-system").
		Return(nil)
	installer, err := awslbcontrollerinstaller.NewInstaller(
		client, 5*time.Minute, "prod-eks", "eu-north-1", "", false, true,
	)
	require.NoError(t, err)

	err = installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestUninstallFailsClosedWhenOwnershipUnknown(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "aws-load-balancer-controller", "kube-system").
		Return(nil, assert.AnError)
	installer, err := awslbcontrollerinstaller.NewInstaller(
		client, 5*time.Minute, "prod-eks", "eu-north-1", "", false, true,
	)
	require.NoError(t, err)

	err = installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "check release ownership for aws-load-balancer-controller")
}

func TestUninstallPreservesReleaseWithoutKSailOwnership(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer, err := awslbcontrollerinstaller.NewInstaller(
		client, 5*time.Minute, "prod-eks", "eu-north-1", "", false,
	)
	require.NoError(t, err)

	err = installer.Uninstall(context.Background())

	require.NoError(t, err)
}

type buildValuesCase struct {
	name           string
	clusterName    string
	region         string
	serviceAccount string
	haEnabled      bool
	want           string
}

func TestBuildValuesYaml(t *testing.T) {
	t.Parallel()

	tests := []buildValuesCase{
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

	runBuildValuesCases(t, tests)
}

func TestBuildValuesYaml_ServiceAccount(t *testing.T) {
	t.Parallel()

	tests := []buildValuesCase{
		{
			name:           "pre-created service account",
			clusterName:    "prod-eks",
			serviceAccount: "aws-load-balancer-controller",
			want: "clusterName: prod-eks\nenableServiceMutatorWebhook: false\n" +
				"serviceAccount:\n  create: false\n  name: \"aws-load-balancer-controller\"\nreplicaCount: 1",
		},
		{
			name:           "pre-created service account with region and ha",
			clusterName:    "prod-eks",
			region:         "eu-north-1",
			serviceAccount: "aws-load-balancer-controller",
			haEnabled:      true,
			want: "clusterName: prod-eks\nenableServiceMutatorWebhook: false\nregion: eu-north-1\n" +
				"serviceAccount:\n  create: false\n  name: \"aws-load-balancer-controller\"\nreplicaCount: 2",
		},
		{
			name:           "whitespace-only service account is unset",
			clusterName:    "prod-eks",
			serviceAccount: "   ",
			want:           "clusterName: prod-eks\nenableServiceMutatorWebhook: false\nreplicaCount: 1",
		},
		{
			// "123" is a valid DNS-1123 name that YAML would otherwise parse
			// as a number — the quoting is what keeps it a string.
			name:           "numeric service account name stays a string",
			clusterName:    "prod-eks",
			serviceAccount: "123",
			want: "clusterName: prod-eks\nenableServiceMutatorWebhook: false\n" +
				"serviceAccount:\n  create: false\n  name: \"123\"\nreplicaCount: 1",
		},
	}

	runBuildValuesCases(t, tests)
}

func runBuildValuesCases(t *testing.T, tests []buildValuesCase) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := awslbcontrollerinstaller.BuildValuesYamlForTest(
				testCase.clusterName, testCase.region, testCase.serviceAccount, testCase.haEnabled,
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
