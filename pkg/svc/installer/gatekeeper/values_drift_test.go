package gatekeeperinstaller_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	gatekeeperinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/gatekeeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newDriftInstaller(client helm.Interface, haEnabled bool) *gatekeeperinstaller.Installer {
	return gatekeeperinstaller.NewInstaller(client, "", "", 5*time.Minute, haEnabled)
}

// storedRenderedValues returns the values an installer renders as Helm stores
// and returns them: persisted as JSON, so --set numbers read back as float64.
func storedRenderedValues(t *testing.T, installer *gatekeeperinstaller.Installer) map[string]any {
	t.Helper()

	rendered, err := installer.RenderedValues()
	require.NoError(t, err)

	raw, err := json.Marshal(rendered)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))

	return stored
}

func expectDeployedRelease(client *helm.MockInterface, values map[string]any) {
	client.EXPECT().
		ReleaseExists(mock.Anything, "gatekeeper", "gatekeeper-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "gatekeeper", "gatekeeper-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "gatekeeper", "gatekeeper-system").
		Return(values, nil)
}

// TestValuesDrifted_DetectsAReleaseMissingRenderedHAValues replays ksail#7651:
// a release installed with other values than the ones this KSail renders
// (here, without its HA replicas, disruption budget and topology spread) must
// report drift, so `cluster update` upgrades it even though ksail.yaml did
// not change.
func TestValuesDrifted_DetectsAReleaseMissingRenderedHAValues(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, true)

	expectDeployedRelease(
		client,
		storedRenderedValues(t, newDriftInstaller(helm.NewMockInterface(t), false)),
	)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.True(t, drifted, "a release without the rendered HA values must report drift")
}

// TestValuesDrifted_StaysSilentWhenTheReleaseMatches is the control: a release
// that carries exactly the rendered values, including the --set entries Helm
// stores with another type, is not drift.
func TestValuesDrifted_StaysSilentWhenTheReleaseMatches(t *testing.T) {
	t.Parallel()

	for _, haEnabled := range []bool{false, true} {
		client := helm.NewMockInterface(t)
		installer := newDriftInstaller(client, haEnabled)

		expectDeployedRelease(client, storedRenderedValues(t, installer))

		drifted, err := installer.ValuesDrifted(context.Background())
		require.NoError(t, err)
		assert.False(t, drifted, "matching values must not report drift (HA=%t)", haEnabled)
	}
}

// TestValuesDrifted_LeavesAGitOpsOwnedReleaseAlone never reports drift for a
// release a GitOps controller owns: Install skips it, so the reported upgrade
// would never happen.
func TestValuesDrifted_LeavesAGitOpsOwnedReleaseAlone(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, true)

	client.EXPECT().
		ReleaseExists(mock.Anything, "gatekeeper", "gatekeeper-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "gatekeeper", "gatekeeper-system").
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": "gatekeeper"}, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}
