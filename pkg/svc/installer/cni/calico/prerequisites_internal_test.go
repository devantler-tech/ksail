package calicoinstaller

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errOversizedCalicoRelease = errors.New(
	"calico-crds release exceeds the Kubernetes Secret size limit",
)

func TestCalicoInstallDoesNotStorePrerequisiteChartInHelm(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	client := prerequisiteInstallClient(t)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ReleaseName == "calico"
		})).
		Return(nil, nil)
	installer := NewInstaller(client, fixture.kubeconfig, "test-context", time.Second,
		v1alpha1.DistributionVanilla, false)
	require.NoError(t, installer.Install(context.Background()))
	require.NotNil(t, fixture.objects[fixtureCRDPath])
	require.NotNil(
		t,
		fixture.objects["/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicies/calico-fixture"],
	)

	_, state, err := readInstallerInventory(context.Background(), installer)
	require.NoError(t, err)
	require.True(t, state.Complete)
	require.Len(t, state.Resources, 2)
}

func TestCalicoPrerequisitesResumeWithRecordedLiveIdentities(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	inst := NewInstaller(
		nil,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	ctx := context.Background()
	plan, err := inst.planPrerequisites(ctx, prerequisiteFixtureManifest)
	require.NoError(t, err)
	require.NoError(t, plan.apply(ctx))
	resumed, err := inst.planPrerequisites(ctx, prerequisiteFixtureManifest)
	require.NoError(t, err)
	require.NoError(
		t,
		resumed.apply(ctx),
		"reconcile the recorded objects at their real resource endpoints",
	)
	require.NoError(t, resumed.established(ctx, time.Second))
	require.Len(t, resumed.state.Resources, 2)
}

func prerequisiteInstallClient(t *testing.T) *helm.MockInterface {
	t.Helper()
	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico-crds", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().AddRepository(mock.Anything, mock.Anything, mock.Anything).Return(nil)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ReleaseName == "calico-crds"
		})).
		Return(nil, errOversizedCalicoRelease).
		Maybe()
	client.EXPECT().TemplateChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
		return spec.ReleaseName == "calico-crds"
	})).Return(prerequisiteFixtureManifest, nil)
	client.EXPECT().RefreshDiscovery().Return(nil)

	return client
}
