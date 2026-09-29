package clusterautoscalerinstaller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

var errDriftProbe = errors.New("probe failed")

func newDriftInstaller(
	t *testing.T,
	client helm.Interface,
) *clusterautoscalerinstaller.Installer {
	t.Helper()

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		v1alpha1.NodeAutoscalerConfig{
			Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
			Pools: []v1alpha1.NodePool{
				{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 3},
			},
			MaxNodesTotal: 10,
		},
		true,
		true,
		true,
	)
	require.NoError(t, err)

	return installer
}

// renderedValues parses the installer's rendered values the way Helm stores a
// release's user-supplied values.
func renderedValues(t *testing.T, installer *clusterautoscalerinstaller.Installer) map[string]any {
	t.Helper()

	var values map[string]any

	require.NoError(t, yaml.Unmarshal([]byte(installer.RenderedValuesYAML()), &values))

	return values
}

func expectDeployedValues(client *helm.MockInterface, values map[string]any, err error) {
	client.EXPECT().
		ReleaseExists(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(true, nil)
	client.EXPECT().
		GetReleaseValues(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(values, err)
}

// TestValuesDrifted_DetectsAReleaseMissingTheCPULimit replays ksail#7366: a
// release installed before ksail#7145 carries no CPU limit, and a KSail that
// renders one must report drift so `cluster update` upgrades it.
func TestValuesDrifted_DetectsAReleaseMissingTheCPULimit(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(t, client)

	deployed := renderedValues(t, installer)
	resources, ok := deployed["resources"].(map[string]any)
	require.True(t, ok, "rendered values must declare resources")
	limits, ok := resources["limits"].(map[string]any)
	require.True(t, ok, "rendered values must declare resource limits")
	require.Contains(t, limits, "cpu", "this test needs the rendered CPU limit to remove")
	delete(limits, "cpu")

	expectDeployedValues(client, deployed, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.True(t, drifted, "a release without the rendered CPU limit must report drift")
}

// TestValuesDrifted_StaysSilentWhenTheReleaseMatches is the control: the same
// values, even with integers decoded as a different Go type, are not drift.
func TestValuesDrifted_StaysSilentWhenTheReleaseMatches(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(t, client)

	deployed := renderedValues(t, installer)
	deployed["replicas"] = int64(2)

	expectDeployedValues(client, deployed, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted, "a release carrying the rendered values must not report drift")
}

// TestValuesDrifted_LeavesAMissingReleaseToTheEnabledField keeps installation
// with the enabled field's handler: no release means nothing to compare.
func TestValuesDrifted_LeavesAMissingReleaseToTheEnabledField(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := newDriftInstaller(t, client)

	client.EXPECT().
		ReleaseExists(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(false, nil)

	drifted, err := installer.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestValuesDrifted_SurfacesProbeErrors never turns an unreadable release into
// a clean "no drift" verdict.
func TestValuesDrifted_SurfacesProbeErrors(t *testing.T) {
	t.Parallel()

	t.Run("ReleaseExists", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		installer := newDriftInstaller(t, client)

		client.EXPECT().
			ReleaseExists(mock.Anything, "cluster-autoscaler", "kube-system").
			Return(false, errDriftProbe)

		_, err := installer.ValuesDrifted(context.Background())
		require.ErrorIs(t, err, errDriftProbe)
	})

	t.Run("GetReleaseValues", func(t *testing.T) {
		t.Parallel()

		client := helm.NewMockInterface(t)
		installer := newDriftInstaller(t, client)

		expectDeployedValues(client, nil, errDriftProbe)

		_, err := installer.ValuesDrifted(context.Background())
		require.ErrorIs(t, err, errDriftProbe)
	})
}
