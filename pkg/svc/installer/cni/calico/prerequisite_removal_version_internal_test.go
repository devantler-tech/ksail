package calicoinstaller

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalicoRemovalFollowsServingVersionWithTheRecordedIdentity(t *testing.T) {
	t.Parallel()

	for _, resource := range []string{
		"mutatingadmissionpolicies", "mutatingadmissionpolicybindings",
		"validatingadmissionpolicies", "validatingadmissionpolicybindings",
	} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()

			for _, recordedVersion := range []string{"v1beta1", "v1"} {
				t.Run(recordedVersion, func(t *testing.T) {
					t.Parallel()
					fixture, path := removalVersionFixture(t, resource, recordedVersion)
					removal, err := dependencyInstaller(fixture).
						planPrerequisiteRemoval(context.Background())
					require.NoError(t, err)
					require.NoError(t, removal.remove(context.Background(), time.Second))
					require.Nil(t, fixture.objects[path],
						"the same owned object must be removed through its served version")
					require.Contains(t, fixture.writes, "DELETE "+path)
					require.Nil(t, fixture.objects[removalVersionInventoryPath])
				})
			}
		})
	}
}

func TestCalicoRemovalVersionFallbackRetainsIdentityAndOwnershipFences(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(map[string]any){
		"replacement UID":          func(meta map[string]any) { meta["uid"] = "replacement" },
		"different name":           func(meta map[string]any) { meta["name"] = "another-policy" },
		"different scope":          func(meta map[string]any) { meta["namespace"] = "foreign" },
		"missing resource version": func(meta map[string]any) { delete(meta, "resourceVersion") },
		"foreign owner":            func(meta map[string]any) { meta["labels"] = map[string]any{} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture, path := removalVersionFixture(t, "mutatingadmissionpolicies", "v1beta1")
			mutate(fixtureMetadata(fixture.objects[path]))
			_, err := dependencyInstaller(fixture).planPrerequisiteRemoval(context.Background())
			require.Error(t, err)
			require.Empty(t, fixture.writes,
				"an uncertain target must not retire any object or inventory")
			require.NotNil(t, fixture.objects[removalVersionInventoryPath])
		})
	}
}

func TestCalicoRemovalVersionFallbackFencesChangesAfterInspection(t *testing.T) {
	t.Parallel()
	fixture, path := removalVersionFixture(t, "mutatingadmissionpolicybindings", "v1beta1")
	removal, err := dependencyInstaller(fixture).planPrerequisiteRemoval(context.Background())
	require.NoError(t, err)

	fixture.beforeWrite = func(request *http.Request, _ map[string]any) {
		if request.Method == http.MethodDelete && request.URL.Path == path {
			meta := fixtureMetadata(fixture.objects[path])
			meta["resourceVersion"] = "8"
			meta["labels"] = map[string]any{"argocd.argoproj.io/instance": "transferred"}
		}
	}

	require.Error(t, removal.remove(context.Background(), time.Second))
	require.Empty(t, fixture.writes)
	require.NotNil(t, fixture.objects[path])
	require.NotNil(t, fixture.objects[removalVersionInventoryPath])
}

func TestCalicoRemovalRetainsInventoryWhenServingVersionChangesDuringDelete(t *testing.T) {
	t.Parallel()

	for _, liveUID := range []string{"owned", "replacement"} {
		t.Run(liveUID, func(t *testing.T) {
			t.Parallel()
			fixture, path := removalVersionFixture(t, "mutatingadmissionpolicies", "v1beta1")
			removal, err := dependencyInstaller(fixture).
				planPrerequisiteRemoval(context.Background())
			require.NoError(t, err)

			alternate := "/apis/admissionregistration.k8s.io/v1beta1/" +
				"mutatingadmissionpolicies/calico-fixture"

			fixture.beforeWrite = func(request *http.Request, _ map[string]any) {
				if request.Method == http.MethodDelete && request.URL.Path == path {
					object := fixture.objects[path]
					fixtureMetadata(object)["uid"] = liveUID
					fixture.objects[alternate] = object
					delete(fixture.objects, path)
				}
			}

			require.Error(t, removal.remove(context.Background(), time.Second))
			require.Empty(t, fixture.writes, "a version change cannot prove object absence")
			require.NotNil(t, fixture.objects[alternate])
			require.NotNil(t, fixture.objects[removalVersionInventoryPath])
		})
	}
}

const removalVersionInventoryPath = "/api/v1/namespaces/tigera-operator/configmaps/ksail-calico-prerequisites"

func removalVersionFixture(
	t *testing.T, resource, recordedVersion string,
) (*prerequisiteFixture, string) {
	t.Helper()
	fixture := newPrerequisiteFixture(t)
	fixture.setInventory(t, prerequisiteState{Resources: []prerequisiteRef{{
		Group: admissionRegistrationGroup, Version: recordedVersion, Resource: resource,
		Name: "calico-fixture", UID: "owned",
	}}})

	servedVersion := "v1"
	if recordedVersion == "v1" {
		servedVersion = admissionBetaVersion
	}

	path := "/apis/" + admissionRegistrationGroup + "/" + servedVersion + "/" + resource + "/calico-fixture"
	// The metadata client returns metadata GVK, not the original resource GVK.
	fixture.objects[path] = map[string]any{
		"apiVersion": "meta.k8s.io/v1", "kind": "PartialObjectMetadata",
		"metadata": map[string]any{
			"name": "calico-fixture", "uid": "owned", "resourceVersion": "7",
			"labels": map[string]any{prerequisiteOwnerKey: prerequisiteOwner},
		},
	}

	return fixture, path
}
