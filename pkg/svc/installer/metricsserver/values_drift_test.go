package metricsserverinstaller_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	metricsserverinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/metricsserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newDriftInstaller(
	client helm.Interface,
	distribution v1alpha1.Distribution,
	haEnabled bool,
) *metricsserverinstaller.Installer {
	return metricsserverinstaller.NewInstaller(client, 5*time.Minute, distribution, haEnabled)
}

// storedRenderedValues returns the values an installer renders as Helm stores
// and returns them: persisted as JSON, so --set numbers read back as float64.
func storedRenderedValues(
	t *testing.T,
	installer *metricsserverinstaller.Installer,
) map[string]any {
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
		ReleaseExists(mock.Anything, "metrics-server", "kube-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "metrics-server", "kube-system").
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "metrics-server", "kube-system").
		Return(values, nil)
}

// TestValuesDrifted_DetectsAReleaseWithOtherRenderedValues replays ksail#7703:
// a release installed with other values than the ones this KSail renders must
// report drift, so `cluster update` upgrades it even though ksail.yaml did not
// change. One case differs in the --set values and the topology spread (HA),
// the other only in a start-up argument (the VCluster kubelet flag).
func TestValuesDrifted_DetectsAReleaseWithOtherRenderedValues(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		deployed *metricsserverinstaller.Installer
	}{
		"high availability": {
			deployed: newDriftInstaller(
				helm.NewMockInterface(t), v1alpha1.DistributionVanilla, false,
			),
		},
		"start-up argument": {
			deployed: newDriftInstaller(
				helm.NewMockInterface(t), v1alpha1.DistributionVCluster, true,
			),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := helm.NewMockInterface(t)
			installer := newDriftInstaller(client, v1alpha1.DistributionVanilla, true)

			expectDeployedRelease(client, storedRenderedValues(t, testCase.deployed))

			drifted, err := installer.ValuesDrifted(context.Background())
			require.NoError(t, err)
			assert.True(t, drifted, "a release with other values must report drift")
		})
	}
}

// TestValuesDrifted_StaysSilentWhenTheReleaseMatches is the control: a release
// that carries exactly the rendered values, including the --set entries Helm
// stores with another type, is not drift.
func TestValuesDrifted_StaysSilentWhenTheReleaseMatches(t *testing.T) {
	t.Parallel()

	for _, haEnabled := range []bool{false, true} {
		client := helm.NewMockInterface(t)
		installer := newDriftInstaller(client, v1alpha1.DistributionVanilla, haEnabled)

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
	installer := newDriftInstaller(client, v1alpha1.DistributionVanilla, true)

	client.EXPECT().
		ReleaseExists(mock.Anything, "metrics-server", "kube-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "metrics-server", "kube-system").
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": "metrics-server"}, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_LeavesAMissingReleaseAlone leaves installing a release that
// does not exist to the cluster.metricsServer field.
func TestValuesDrifted_LeavesAMissingReleaseAlone(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, v1alpha1.DistributionVanilla, false)

	client.EXPECT().
		ReleaseExists(mock.Anything, "metrics-server", "kube-system").
		Return(false, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}
