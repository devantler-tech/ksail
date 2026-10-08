package calicoinstaller

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCalicoValuesDriftDetectsPrerequisiteMigration(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		state *prerequisiteState
		want  bool
	}{
		"legacy release without inventory": {want: true},
		"interrupted installation":         {state: &prerequisiteState{Version: chartVersion()}, want: true},
		"previous chart version":           {state: &prerequisiteState{Version: "previous", Complete: true}, want: true},
		"complete current installation":    {state: &prerequisiteState{Version: chartVersion(), Complete: true}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newPrerequisiteFixture(t)

			if testCase.state != nil {
				testCase.state.Resources = []prerequisiteRef{
					{
						Group:    "apiextensions.k8s.io",
						Version:  "v1",
						Resource: "customresourcedefinitions",
						Name:     "installations.operator.tigera.io",
						UID:      "owned",
					},
				}
				fixture.objects[fixtureCRDPath] = fixtureCRD("owned", "7")
				fixture.setInventory(t, *testCase.state)
			}

			client := helm.NewMockInterface(t)
			client.EXPECT().
				ReleaseExists(mock.Anything, "calico", prerequisiteNamespace).
				Return(true, nil)
			client.EXPECT().
				GetReleaseStorageLabels(mock.Anything, "calico", prerequisiteNamespace).
				Return(nil, nil)
			client.EXPECT().
				GetReleaseStorageLabels(mock.Anything, "calico-crds", prerequisiteNamespace).
				Return(nil, nil)
			inst := NewInstaller(
				client,
				fixture.kubeconfig,
				"test-context",
				time.Second,
				v1alpha1.DistributionVanilla,
				false,
			)
			values, err := helm.UserSuppliedValues(inst.chartSpec())
			require.NoError(t, err)
			client.EXPECT().
				GetReleaseValues(mock.Anything, "calico", prerequisiteNamespace).
				Return(values, nil).
				Maybe()

			drifted, err := inst.ValuesDrifted(context.Background())
			require.NoError(t, err)
			require.Equal(t, testCase.want, drifted)
			require.Empty(t, fixture.writes, "drift detection must not change the cluster")
		})
	}
}

func (fixture *prerequisiteFixture) setInventory(t *testing.T, state prerequisiteState) {
	t.Helper()

	data, err := json.Marshal(state)
	require.NoError(t, err)

	fixture.objects["/api/v1/namespaces/tigera-operator/configmaps/ksail-calico-prerequisites"] = map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":            prerequisiteInventory,
			"namespace":       prerequisiteNamespace,
			"uid":             "inventory",
			"resourceVersion": "1",
			"labels":          map[string]any{prerequisiteOwnerKey: prerequisiteOwner},
		},
		"data": map[string]any{"inventory.json": string(data)},
	}
}

func TestCalicoValuesDriftResumesNonDeployedOperator(t *testing.T) {
	t.Parallel()

	for _, recorded := range []bool{false, true} {
		t.Run(strconv.FormatBool(recorded), func(t *testing.T) {
			t.Parallel()

			fixture := newPrerequisiteFixture(t)
			if recorded {
				fixture.setInventory(t, prerequisiteState{Version: chartVersion()})
			}

			client := helm.NewMockInterface(t)
			client.EXPECT().
				ReleaseExists(mock.Anything, "calico", prerequisiteNamespace).
				Return(false, nil)

			for _, release := range []string{"calico", "calico-crds"} {
				client.EXPECT().
					GetReleaseStorageLabels(mock.Anything, release, prerequisiteNamespace).
					Return(nil, nil).
					Maybe()
			}

			inst := NewInstaller(
				client,
				fixture.kubeconfig,
				"test-context",
				time.Second,
				v1alpha1.DistributionVanilla,
				false,
			)
			drifted, err := inst.ValuesDrifted(context.Background())
			require.NoError(t, err)
			require.Equal(
				t,
				recorded,
				drifted,
				"a recorded installation must retry a missing or failed operator",
			)
			require.Empty(t, fixture.writes)
		})
	}
}
