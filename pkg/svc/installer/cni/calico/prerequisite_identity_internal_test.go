package calicoinstaller

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCalicoPrerequisitePlanRejectsRecordedUIDReplacement(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	fixture.seedRecordedIdentity(t)
	installer := NewInstaller(
		nil,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	_, err := installer.planPrerequisites(context.Background(), prerequisiteFixtureManifest)
	require.ErrorContains(t, err, "identity changed")
	require.Empty(t, fixture.writes)
}

func TestCalicoUninstallRejectsRecordedUIDReplacement(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	fixture.seedRecordedIdentity(t)
	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico-crds", prerequisiteNamespace).
		Return(nil, nil)
	installer := NewInstaller(
		client,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	err := installer.Uninstall(context.Background())
	require.ErrorContains(t, err, "identity changed")
	require.Empty(t, fixture.writes)
}
