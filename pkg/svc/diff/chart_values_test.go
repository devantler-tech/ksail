package diff_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckChartValuesSurfacesCertManagerDrift is ksail#7444: cert-manager
// values that a KSail upgrade renders differently behind an unchanged spec must
// surface as one in-place change named for cert-manager, or `cluster update`
// reports success and leaves the old values running.
func TestCheckChartValuesSurfacesCertManagerDrift(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	specdiff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker).
		CheckChartValues(specdiff.CertManagerValuesField, "cert-manager", true, result)

	require.Len(t, result.InPlaceChanges, 1)

	change := result.InPlaceChanges[0]
	assert.Equal(t, specdiff.CertManagerValuesField, change.Field)
	assert.Equal(t, clusterupdate.ChangeCategoryInPlace, change.Category)
	assert.Contains(t, change.Reason, "cert-manager")
	assert.True(t, strings.HasPrefix(change.Field, "cluster.certManager."),
		"the field must name the cert-manager component in the change summary")
	assert.Empty(t, result.RecreateRequired, "a values-only upgrade must never demand recreation")
	assert.Empty(t, result.RebootRequired, "a values-only upgrade must never demand a reboot")
}

// TestCheckChartValuesStaysSilentWithoutDrift keeps an up-to-date release out
// of the change summary.
func TestCheckChartValuesStaysSilentWithoutDrift(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	specdiff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker).
		CheckChartValues(specdiff.CertManagerValuesField, "cert-manager", false, result)

	assert.Zero(t, result.TotalChanges())
}
