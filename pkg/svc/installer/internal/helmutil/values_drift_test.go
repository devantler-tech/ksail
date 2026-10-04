package helmutil_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errValuesProbe = errors.New("values probe failed")

const (
	driftRelease   = "drift-release"
	driftNamespace = "drift-namespace"
)

// newDriftBase builds a Base whose values come from both a values YAML and
// --set entries, the two inline sources KSail installers use.
func newDriftBase(t *testing.T, spec *helm.ChartSpec) (*helmutil.Base, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	spec.ReleaseName = driftRelease
	spec.Namespace = driftNamespace

	return helmutil.NewBase(
		"drift-component",
		client,
		time.Minute,
		&helm.RepositoryEntry{Name: "drift", URL: "https://example.com/charts"},
		spec,
	), client
}

func driftSpec() *helm.ChartSpec {
	return &helm.ChartSpec{
		ValuesYaml: "args:\n  - --secure\nresources:\n  limits:\n    cpu: 100m\n",
		SetValues: map[string]string{
			"replicaCount":                "2",
			"installCRDs":                 "true",
			"podDisruptionBudget.enabled": "true",
		},
	}
}

// storedValues mimics what `helm get values` returns for a release installed
// with values: Helm persists a release as JSON, so every number reads back as
// float64 whatever Go type the installer rendered.
func storedValues(t *testing.T, values map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(values)
	require.NoError(t, err)

	var stored map[string]any

	require.NoError(t, json.Unmarshal(raw, &stored))

	return stored
}

func expectRelease(client *helm.MockInterface, labels map[string]string) {
	client.EXPECT().
		ReleaseExists(mock.Anything, driftRelease, driftNamespace).
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, driftRelease, driftNamespace).
		Return(labels, nil)
}

func expectDeployed(client *helm.MockInterface, values map[string]any) {
	expectRelease(client, map[string]string{"owner": "helm"})
	client.EXPECT().
		GetReleaseValues(mock.Anything, driftRelease, driftNamespace).
		Return(values, nil)
}

// TestBaseValuesDrifted_StaysSilentWhenTheStoredValuesMatch is the control: a
// release installed with exactly the rendered values, read back the way Helm
// stores them (numbers as float64), is not drift. Without it every update
// would upgrade every release it looks at.
func TestBaseValuesDrifted_StaysSilentWhenTheStoredValuesMatch(t *testing.T) {
	t.Parallel()

	base, client := newDriftBase(t, driftSpec())

	rendered, err := base.RenderedValues()
	require.NoError(t, err)
	require.Equal(t, int64(2), rendered["replicaCount"],
		"the control needs a --set number that Helm stores as another type")

	expectDeployed(client, storedValues(t, rendered))

	drifted, err := base.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted, "matching stored values must not report drift")
}

// TestBaseValuesDrifted_DetectsChangedRenderedValues is the core of ksail#7366
// for every Base-backed component: a KSail release that renders a value the
// installed release lacks, or renders a different one, must report drift.
func TestBaseValuesDrifted_DetectsChangedRenderedValues(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(map[string]any){
		"values YAML entry missing": func(values map[string]any) {
			resources, _ := values["resources"].(map[string]any)
			delete(resources, "limits")
		},
		"--set entry missing": func(values map[string]any) {
			delete(values, "podDisruptionBudget")
		},
		"--set entry differs": func(values map[string]any) {
			values["replicaCount"] = float64(1)
		},
		"extra deployed entry": func(values map[string]any) {
			values["legacy"] = "value KSail no longer renders"
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base, client := newDriftBase(t, driftSpec())

			rendered, err := base.RenderedValues()
			require.NoError(t, err)

			deployed := storedValues(t, rendered)
			mutate(deployed)
			expectDeployed(client, deployed)

			drifted, err := base.ValuesDrifted(context.Background())
			require.NoError(t, err)
			assert.True(t, drifted, "values that differ from the rendered ones must report drift")
		})
	}
}

// TestBaseValuesDrifted_LeavesAMissingReleaseToItsSpecField keeps installation
// with the component's spec field: no release means nothing to compare.
func TestBaseValuesDrifted_LeavesAMissingReleaseToItsSpecField(t *testing.T) {
	t.Parallel()

	base, client := newDriftBase(t, driftSpec())

	client.EXPECT().
		ReleaseExists(mock.Anything, driftRelease, driftNamespace).
		Return(false, nil)

	drifted, err := base.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestBaseValuesDrifted_LeavesGitOpsOwnedReleasesAlone keeps a Flux- or
// Argo CD-owned release out of the report: Install skips such a release, so
// drift would claim a reconcile that never happens and resurface on every run.
func TestBaseValuesDrifted_LeavesGitOpsOwnedReleasesAlone(t *testing.T) {
	t.Parallel()

	for name, labels := range map[string]map[string]string{
		"Flux":    {helmutil.FluxNameLabel: "drift-component"},
		"Argo CD": {helmutil.ArgoCDManagedByLabel: "argocd"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base, client := newDriftBase(t, driftSpec())
			expectRelease(client, labels)

			drifted, err := base.ValuesDrifted(context.Background())
			require.NoError(t, err)
			assert.False(t, drifted, "a GitOps-owned release must never report KSail drift")
		})
	}
}

// TestBaseValuesDrifted_ComparesWhenReleaseStorageIsUnlabelled treats missing
// release storage metadata as "not GitOps-owned", the same way Install does.
func TestBaseValuesDrifted_ComparesWhenReleaseStorageIsUnlabelled(t *testing.T) {
	t.Parallel()

	base, client := newDriftBase(t, driftSpec())

	client.EXPECT().
		ReleaseExists(mock.Anything, driftRelease, driftNamespace).
		Return(true, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, driftRelease, driftNamespace).
		Return(nil, helm.ErrNoReleaseStorage)
	client.EXPECT().
		GetReleaseValues(mock.Anything, driftRelease, driftNamespace).
		Return(map[string]any{}, nil)

	drifted, err := base.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.True(t, drifted, "an unlabelled release with stale values must still report drift")
}

// TestBaseValuesDrifted_IgnoresEmptyRenderedValues never reports drift Helm
// cannot apply: an upgrade with no values keeps the release's existing ones, so
// reporting it would schedule the same no-op upgrade on every update.
func TestBaseValuesDrifted_IgnoresEmptyRenderedValues(t *testing.T) {
	t.Parallel()

	base, _ := newDriftBase(t, &helm.ChartSpec{})

	drifted, err := base.ValuesDrifted(context.Background())
	require.NoError(t, err)
	assert.False(t, drifted)
}

// TestBaseValuesDrifted_SurfacesProbeErrors never turns an unreadable release,
// ownership label set or value source into a clean "no drift" verdict.
func TestBaseValuesDrifted_SurfacesProbeErrors(t *testing.T) {
	t.Parallel()

	t.Run("ReleaseExists", func(t *testing.T) {
		t.Parallel()

		base, client := newDriftBase(t, driftSpec())
		client.EXPECT().
			ReleaseExists(mock.Anything, driftRelease, driftNamespace).
			Return(false, errValuesProbe)

		_, err := base.ValuesDrifted(context.Background())
		require.ErrorIs(t, err, errValuesProbe)
	})

	t.Run("GetReleaseStorageLabels", func(t *testing.T) {
		t.Parallel()

		base, client := newDriftBase(t, driftSpec())
		client.EXPECT().
			ReleaseExists(mock.Anything, driftRelease, driftNamespace).
			Return(true, nil)
		client.EXPECT().
			GetReleaseStorageLabels(mock.Anything, driftRelease, driftNamespace).
			Return(nil, errValuesProbe)

		_, err := base.ValuesDrifted(context.Background())
		require.ErrorIs(t, err, errValuesProbe)
	})

	t.Run("GetReleaseValues", func(t *testing.T) {
		t.Parallel()

		base, client := newDriftBase(t, driftSpec())
		expectRelease(client, nil)
		client.EXPECT().
			GetReleaseValues(mock.Anything, driftRelease, driftNamespace).
			Return(nil, errValuesProbe)

		_, err := base.ValuesDrifted(context.Background())
		require.ErrorIs(t, err, errValuesProbe)
	})

	t.Run("chart-relative values", func(t *testing.T) {
		t.Parallel()

		base, _ := newDriftBase(t, &helm.ChartSpec{ValueFiles: []string{"values.yaml"}})

		_, err := base.ValuesDrifted(context.Background())
		require.ErrorIs(t, err, helm.ErrChartRelativeValues)
	})
}
