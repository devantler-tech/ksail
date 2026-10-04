package detector_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

const scaleDownUnneededTimeField = "cluster.autoscaler.node.scaleDownUnneededTime"

// TestAutoscalerScaleDownUnneededTimeConverges replays ksail#7383 through the
// installer's rendered values, the detected baseline and the update diff
// together: an omitted duration must compare as the value the installer
// actually renders, so applying a reported change leaves the next diff clean
// instead of reporting the same change on every update.
func TestAutoscalerScaleDownUnneededTimeConverges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// installed is the duration the existing release was installed from.
		installed string
		// desired is the duration in the user's configuration.
		desired string
		// wantOld and wantNew describe the single expected change; both empty
		// means the diff must report no duration change at all.
		wantOld, wantNew string
	}{
		{name: "omitted desired against the rendered default"},
		{name: "explicit default against an omitted install", desired: "10m"},
		{name: "omitted desired against an explicit default install", installed: "10m"},
		{
			name:    "explicit change from the default",
			desired: "15m",
			wantOld: "10m",
			wantNew: "15m",
		},
		{
			name:      "removing an explicit value requests the default",
			installed: "15m",
			wantOld:   "15m",
			wantNew:   "10m",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			desired := nodeAutoscalerWithScaleDown(testCase.desired)
			baseline := detectInstalledAutoscaler(
				t, installAutoscaler(t, nodeAutoscalerWithScaleDown(testCase.installed)),
			)

			changes := scaleDownUnneededTimeChanges(baseline, desired)

			if testCase.wantNew == "" {
				assert.Empty(t, changes, "an unchanged effective duration must not be reported")
			} else {
				require.Len(t, changes, 1, "a changed effective duration must be reported once")
				assert.Equal(t, testCase.wantOld, changes[0].OldValue)
				assert.Equal(t, testCase.wantNew, changes[0].NewValue)
				assert.Equal(t, clusterupdate.ChangeCategoryInPlace, changes[0].Category)
			}

			// Applying the update installs the desired configuration; reading
			// that release back must leave nothing to apply.
			applied := detectInstalledAutoscaler(t, installAutoscaler(t, desired))
			assert.Empty(
				t, scaleDownUnneededTimeChanges(applied, desired),
				"the diff must be clean once the desired configuration is installed",
			)
		})
	}
}

// nodeAutoscalerWithScaleDown returns an enabled node autoscaler configuration with one pool and
// the given scale-down duration; an empty duration means the setting is omitted.
func nodeAutoscalerWithScaleDown(scaleDownUnneededTime string) v1alpha1.NodeAutoscalerConfig {
	return v1alpha1.NodeAutoscalerConfig{
		Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 3},
		},
		ScaleDownUnneededTime: scaleDownUnneededTime,
	}
}

// installAutoscaler runs the Cluster Autoscaler installer against a mock Helm
// client and returns the values it installed, decoded the way Helm stores a
// release's user-supplied values.
func installAutoscaler(t *testing.T, cfg v1alpha1.NodeAutoscalerConfig) map[string]any {
	t.Helper()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, helm.ErrNoReleaseStorage)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	var valuesYAML string

	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, spec *helm.ChartSpec) (*helm.ReleaseInfo, error) {
			valuesYAML = spec.ValuesYaml

			return &helm.ReleaseInfo{}, nil
		})

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client, time.Second, cfg, false, true, true,
	)
	require.NoError(t, err)
	require.NoError(t, installer.Install(context.Background()))

	var values map[string]any

	require.NoError(t, yaml.Unmarshal([]byte(valuesYAML), &values))

	return values
}

// detectInstalledAutoscaler reads an installed release's values back through
// the component detector, which builds the baseline the update diff compares
// against.
func detectInstalledAutoscaler(
	t *testing.T,
	values map[string]any,
) v1alpha1.NodeAutoscalerConfig {
	t.Helper()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		ReleaseExists(
			mock.Anything, detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
		).
		Return(true, nil)
	client.EXPECT().
		GetReleaseValues(
			mock.Anything, detector.ReleaseClusterAutoscaler, detector.NamespaceClusterAutoscaler,
		).
		Return(values, nil)

	cfg, err := detector.NewComponentDetector(client, fake.NewClientset(), nil).
		ExportDetectNodeAutoscaler(context.Background())
	require.NoError(t, err)

	return cfg
}

// scaleDownUnneededTimeChanges diffs a cluster whose node autoscaler is the
// detected baseline against one carrying the desired configuration, and returns
// only the scale-down duration changes.
func scaleDownUnneededTimeChanges(
	baseline, desired v1alpha1.NodeAutoscalerConfig,
) []clusterupdate.Change {
	current := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner,
	)
	current.Autoscaler.Node = baseline

	wanted := clusterupdate.DefaultCurrentSpec(
		v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner,
	)
	wanted.Autoscaler.Node = desired

	result := diff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner).
		ComputeDiff(current, wanted, nil, nil)

	var changes []clusterupdate.Change

	for _, change := range result.AllChanges() {
		if change.Field == scaleDownUnneededTimeField {
			changes = append(changes, change)
		}
	}

	return changes
}
