package fluxinstaller_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFluxOperatorRendersNoChartValues records why `cluster update` compares
// no Flux operator chart values (ksail#7772): KSail passes the chart none, and
// Flux's own configuration is the FluxInstance, which has its own drift checks.
// Rendering values later must add a drift probe for the operator.
func TestFluxOperatorRendersNoChartValues(t *testing.T) {
	t.Parallel()

	values, err := helm.UserSuppliedValues(
		fluxinstaller.NewInstaller(nil, 5*time.Minute, "").ChartSpec(),
	)

	require.NoError(t, err)
	assert.Empty(t, values)
}
