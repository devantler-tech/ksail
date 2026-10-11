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

func TestLegacyCalicoRemovalRecordsIdentityBeforeDestruction(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	fixture.seedOperatorHistory()
	fixture.seedLegacyCRD("original", "7")

	client := helm.NewMockInterface(t)
	for _, release := range []string{"calico", "calico-crds"} {
		client.EXPECT().
			GetReleaseStorageLabels(mock.Anything, release, prerequisiteNamespace).
			Return(nil, nil)
	}

	client.EXPECT().UninstallRelease(mock.Anything, "calico", prerequisiteNamespace).Run(
		func(_ context.Context, _, _ string) { delete(fixture.objects, fixtureOperatorHistoryPath) },
	).
		Return(nil).Once()

	fixture.beforeWrite = func(request *http.Request, _ map[string]any) {
		if request.Method == http.MethodDelete && request.URL.Path == fixtureCRDPath {
			fixtureMetadata(fixture.objects[fixtureCRDPath])["resourceVersion"] = "8"
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

	fixture.beforeWrite = nil
	fixture.seedLegacyCRD("replacement", "9")
	require.ErrorContains(t, inst.Uninstall(context.Background()), "identity changed")
	require.Equal(t, "replacement", fixtureMetadata(fixture.objects[fixtureCRDPath])["uid"])
	client.AssertNumberOfCalls(t, "UninstallRelease", 1)
}

func (fixture *prerequisiteFixture) seedLegacyCRD(uid, version string) {
	object := fixtureCRD(uid, version)
	fixtureMetadata(object)["labels"] = map[string]any{"app.kubernetes.io/managed-by": "Helm"}
	fixtureMetadata(object)["annotations"] = map[string]any{
		"meta.helm.sh/release-name":      "calico-crds",
		"meta.helm.sh/release-namespace": prerequisiteNamespace,
	}
	fixture.objects[fixtureCRDPath] = object
	fixture.objects["/api/v1/namespaces/tigera-operator/secrets/sh.helm.release.v1.calico-crds.v1"] = map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name": "sh.helm.release.v1.calico-crds.v1", "namespace": prerequisiteNamespace,
			"uid": "legacy-history", "resourceVersion": "1",
			"labels": map[string]any{"owner": "helm", "name": "calico-crds"},
		},
	}
}

func TestCalicoRemovalRejectsHistoryInAnotherStorageBackend(t *testing.T) {
	for _, driver := range []string{"configmap", "secret"} {
		t.Run(driver, func(t *testing.T) {
			t.Setenv("HELM_DRIVER", driver)
			fixture := newPrerequisiteFixture(t)
			fixture.seedOperatorHistory()

			if driver == "secret" {
				path := "/api/v1/namespaces/tigera-operator/configmaps/sh.helm.release.v1.calico.v1"
				fixture.objects[path] = fixture.objects[fixtureOperatorHistoryPath]
				delete(fixture.objects, fixtureOperatorHistoryPath)
			}

			fixture.seedRecordedIdentity(t)
			fixture.objects[fixtureCRDPath] = fixtureCRD("original", "2")

			client := helm.NewMockInterface(t)
			for _, release := range []string{"calico", "calico-crds"} {
				client.EXPECT().
					GetReleaseStorageLabels(mock.Anything, release, prerequisiteNamespace).
					Return(nil, nil)
			}

			inst := NewInstaller(
				client,
				fixture.kubeconfig,
				"test-context",
				time.Second,
				v1alpha1.DistributionVanilla,
				false,
			)
			require.ErrorContains(t, inst.Uninstall(context.Background()), "storage backend")
			require.Empty(t, fixture.writes, "a backend mismatch must be rejected before any write")
		})
	}
}
