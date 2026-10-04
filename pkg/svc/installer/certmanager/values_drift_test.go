package certmanagerinstaller_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	certmanagerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/certmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// storedRenderedValues returns the values an installer renders as Helm stores
// and returns them: persisted as JSON, so --set numbers read back as float64.
func storedRenderedValues(t *testing.T, installer *certmanagerinstaller.Installer) map[string]any {
	t.Helper()

	rendered, err := installer.RenderedValues()
	require.NoError(t, err)

	raw, err := json.Marshal(rendered)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))

	return stored
}

func expectDeployedCertManager(client *helm.MockInterface, values map[string]any) {
	client.EXPECT().
		ReleaseExists(mock.Anything, "cert-manager", "cert-manager").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "cert-manager", "cert-manager").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "cert-manager", "cert-manager").
		Return(values, nil)
}

// TestValuesDrifted_DetectsAReleaseMissingRenderedHAValues replays ksail#7444:
// a cert-manager release installed with other values than the ones this KSail
// renders (here, without its HA replicas, PodDisruptionBudgets and topology
// spread) must report drift, so `cluster update` upgrades it even though
// ksail.yaml did not change.
func TestValuesDrifted_DetectsAReleaseMissingRenderedHAValues(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := certmanagerinstaller.NewInstaller(client, 5*time.Minute, true)

	deployed := storedRenderedValues(
		t,
		certmanagerinstaller.NewInstaller(helm.NewMockInterface(t), 5*time.Minute, false),
	)
	expectDeployedCertManager(client, deployed)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.True(t, drifted, "a release without the rendered HA values must report drift")
}

// TestValuesDrifted_StaysSilentWhenTheReleaseMatches is the control: a release
// that carries exactly the rendered values, including the --set entries Helm
// stores with another number type, is not drift.
func TestValuesDrifted_StaysSilentWhenTheReleaseMatches(t *testing.T) {
	t.Parallel()

	for _, haEnabled := range []bool{false, true} {
		client := helm.NewMockInterface(t)
		installer := certmanagerinstaller.NewInstaller(client, 5*time.Minute, haEnabled)

		expectDeployedCertManager(client, storedRenderedValues(t, installer))

		drifted, err := installer.ValuesDrifted(context.Background())
		require.NoError(t, err)
		assert.False(t, drifted, "matching values must not report drift (HA=%t)", haEnabled)
	}
}
