package calicoinstaller

import (
	"context"
	"net/http"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCalicoUninstallResumesAfterOperatorWasRemoved(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	fixture.seedOperatorHistory()
	fixture.objects[fixtureCRDPath] = fixtureCRD("owned", "7")
	fixture.setInventory(
		t,
		prerequisiteState{
			Resources: []prerequisiteRef{
				{
					Group:    "apiextensions.k8s.io",
					Version:  "v1",
					Resource: "customresourcedefinitions",
					Name:     "installations.operator.tigera.io",
					UID:      "owned",
				},
			},
		},
	)
	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, "calico-crds", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().UninstallRelease(mock.Anything, "calico", prerequisiteNamespace).Run(
		func(_ context.Context, _, _ string) { delete(fixture.objects, fixtureOperatorHistoryPath) },
	).
		Return(nil).Once()

	changed := false
	fixture.beforeWrite = func(r *http.Request, _ map[string]any) {
		if r.Method == http.MethodDelete && r.URL.Path == fixtureCRDPath && !changed {
			fixtureMetadata(fixture.objects[fixtureCRDPath])["resourceVersion"] = "8"
			changed = true
		}
	}
	inst := NewInstaller(
		client,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	require.Error(t, inst.Uninstall(context.Background()))
	require.NotNil(t, fixture.objects[fixtureCRDPath])
	require.NoError(
		t,
		inst.Uninstall(context.Background()),
		"confirmed absent operator must not block cleanup retry",
	)
	client.AssertNumberOfCalls(t, "UninstallRelease", 1)
	require.Nil(t, fixture.objects[fixtureCRDPath])
	require.Nil(
		t,
		fixture.objects["/api/v1/namespaces/tigera-operator/configmaps/ksail-calico-prerequisites"],
	)
}
