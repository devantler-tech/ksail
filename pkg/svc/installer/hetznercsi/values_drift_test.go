package hetznercsiinstaller_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hetznercsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	csiRelease   = "hcloud-csi"
	csiNamespace = "kube-system"
)

// valuesDriftChecker is the method `cluster update`'s chart-values probe
// asserts on the CSI installer (ksail#7747). If the installer stopped
// providing it, the probe would skip the driver without any signal.
type valuesDriftChecker interface {
	ValuesDrifted(ctx context.Context) (bool, error)
}

func newDriftInstaller(client helm.Interface, haEnabled bool) *hetznercsiinstaller.Installer {
	return hetznercsiinstaller.NewInstaller(
		client, "/tmp/kubeconfig", "admin@prod", 5*time.Minute, "", haEnabled,
	)
}

// storedRenderedValues returns the values an installer renders as Helm stores
// and returns them: persisted as JSON, so numbers read back as float64.
func storedRenderedValues(t *testing.T, installer *hetznercsiinstaller.Installer) map[string]any {
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
	client.EXPECT().ReleaseExists(mock.Anything, csiRelease, csiNamespace).Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, csiRelease, csiNamespace).
		Return(map[string]string{"owner": "helm"}, nil)
	client.EXPECT().GetReleaseValues(mock.Anything, csiRelease, csiNamespace).Return(values, nil)
}

func TestInstallerComparesItsRenderedValues(t *testing.T) {
	t.Parallel()

	var installer any = newDriftInstaller(helm.NewMockInterface(t), true)

	_, ok := installer.(valuesDriftChecker)
	assert.True(t, ok, "the CSI installer must expose ValuesDrifted for cluster update")
}

// TestValuesDrifted_DetectsAReleaseWithOtherRenderedValues: a high-availability
// cluster whose driver was installed without the controller replica count must
// report drift, so `cluster update` upgrades it although ksail.yaml is unchanged.
func TestValuesDrifted_DetectsAReleaseWithOtherRenderedValues(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, true)

	expectDeployedRelease(client, map[string]any{})

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.True(t, drifted, "a release without the rendered values must report drift")
}

// TestValuesDrifted_StaysSilentWhenTheReleaseMatches is the control.
func TestValuesDrifted_StaysSilentWhenTheReleaseMatches(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, true)

	expectDeployedRelease(client, storedRenderedValues(t, installer))

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted, "matching values must not report drift")
}

// TestValuesDrifted_RendersNothingWithoutHighAvailability: a single-controller
// cluster renders no values, and an upgrade without values keeps the deployed
// ones, so there is nothing to compare and the release is never read.
func TestValuesDrifted_RendersNothingWithoutHighAvailability(t *testing.T) {
	t.Parallel()

	installer := newDriftInstaller(helm.NewMockInterface(t), false)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_LeavesAGitOpsOwnedReleaseAlone never reports drift for a
// release a GitOps controller owns: Install skips it, so the reported upgrade
// would never happen.
func TestValuesDrifted_LeavesAGitOpsOwnedReleaseAlone(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, true)

	client.EXPECT().ReleaseExists(mock.Anything, csiRelease, csiNamespace).Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, csiRelease, csiNamespace).
		Return(map[string]string{"helm.toolkit.fluxcd.io/name": csiRelease}, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_LeavesAMissingReleaseAlone leaves installing a release that
// does not exist to the cluster.csi field.
func TestValuesDrifted_LeavesAMissingReleaseAlone(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(client, true)

	client.EXPECT().ReleaseExists(mock.Anything, csiRelease, csiNamespace).Return(false, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}
