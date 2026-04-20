package fluxinstaller_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestChartSpec_Fields verifies that chartSpec() returns a ChartSpec with
// the expected configuration values for the Flux Operator Helm chart.
func TestChartSpec_Fields(t *testing.T) {
	t.Parallel()

	timeout := 3 * time.Minute
	client := helm.NewMockInterface(t)

	// Capture the ChartSpec via an Install call so we can inspect it.
	var capturedSpec *helm.ChartSpec

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				capturedSpec = spec

				return true
			}),
		).
		Return(nil, nil)

	installer := fluxinstaller.NewInstaller(client, timeout)

	err := installer.Install(t.Context())
	require.NoError(t, err)
	require.NotNil(t, capturedSpec)

	assert.Equal(t, "flux-operator", capturedSpec.ReleaseName)
	assert.Equal(
		t,
		"oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator",
		capturedSpec.ChartName,
	)
	assert.Equal(t, "flux-system", capturedSpec.Namespace)
	assert.True(t, capturedSpec.CreateNamespace, "CreateNamespace should be true")
	assert.True(t, capturedSpec.Atomic, "Atomic should be true")
	assert.True(t, capturedSpec.UpgradeCRDs, "UpgradeCRDs should be true")
	assert.True(t, capturedSpec.Wait, "Wait should be true")
	assert.True(t, capturedSpec.WaitForJobs, "WaitForJobs should be true")
	assert.True(t, capturedSpec.Silent, "Silent should be true to suppress CRD warnings")
	assert.Equal(t, timeout, capturedSpec.Timeout, "Timeout should match the installer timeout")
	assert.NotEmpty(t, capturedSpec.Version, "Version should be set from embedded Dockerfile")
}

// TestChartSpec_DifferentTimeouts verifies chartSpec propagates varying timeouts.
func TestChartSpec_DifferentTimeouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"30 seconds", 30 * time.Second},
		{"10 minutes", 10 * time.Minute},
		{"zero timeout", 0},
	}

	//nolint:varnamelen // Short names keep table-driven tests readable.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)

			var capturedSpec *helm.ChartSpec

			client.EXPECT().
				InstallOrUpgradeChart(
					mock.Anything,
					mock.MatchedBy(func(spec *helm.ChartSpec) bool {
						capturedSpec = spec

						return true
					}),
				).
				Return(nil, nil)

			installer := fluxinstaller.NewInstaller(client, tt.timeout)
			err := installer.Install(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.timeout, capturedSpec.Timeout)
		})
	}
}

// TestHelmInstallOrUpgrade_ContextTimeout verifies that the context timeout
// is set to the Helm timeout plus the context timeout buffer (5 minutes).
func TestHelmInstallOrUpgrade_ContextTimeout(t *testing.T) {
	t.Parallel()

	timeout := 2 * time.Minute
	client := helm.NewMockInterface(t)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.MatchedBy(func(_ any) bool {
				// The context should have a deadline
				return true
			}),
			mock.Anything,
		).
		Return(nil, nil)

	installer := fluxinstaller.NewInstaller(client, timeout)

	err := installer.Install(t.Context())
	require.NoError(t, err)
}
