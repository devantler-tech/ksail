package calicoinstaller

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestLegacyDiscoveryListsMetadataWithoutFetchingUnrelatedSchemas(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	fixture.seedLegacyCRD("legacy-uid", "7")
	fixture.objects[fixtureCRDPath+"-foreign"] = fixtureCRD("foreign-uid", "8")
	listedMetadata := false
	fullReads := 0
	fixture.beforeGet = func(writer http.ResponseWriter, request *http.Request) bool {
		if request.URL.Path == "/apis/apiextensions.k8s.io/v1/customresourcedefinitions" {
			listedMetadata = strings.Contains(
				request.Header.Get("Accept"),
				"PartialObjectMetadataList",
			)
			if !listedMetadata {
				fixtureStatus(writer, http.StatusNotAcceptable, "NotAcceptable")

				return true
			}
		} else if strings.HasPrefix(request.URL.Path, "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/") {
			fullReads++
		}

		return false
	}
	client, _, meta, err := dependencyInstaller(fixture).prerequisiteClients()
	require.NoError(t, err)

	plan := &prerequisitePlan{client: client, meta: meta}
	require.NoError(t, plan.captureLegacyResource(context.Background(), prerequisiteResources()[0]))
	require.True(t, listedMetadata)
	require.Zero(t, fullReads)
	require.Len(t, plan.state.Resources, 1)
	require.Equal(t, "legacy-uid", string(plan.state.Resources[0].UID))
	require.Empty(t, fixture.writes)
}

func TestLegacyDiscoveryFetchesOnlyRecordedDependencyAndBindsItsListedIdentity(t *testing.T) {
	t.Parallel()

	for _, changed := range []string{"", "uid", "resourceVersion"} {
		t.Run(changed, func(t *testing.T) {
			t.Parallel()
			fixture := newPrerequisiteFixture(t)
			seedOperatorDependency(t, fixture)
			plan, err := dependencyInstaller(
				fixture,
			).planPrerequisites(context.Background(), prerequisiteFixtureManifest)
			require.NoError(t, err)

			listedMetadata := false
			fullReads := 0
			fixture.beforeGet = func(writer http.ResponseWriter, request *http.Request) bool {
				if request.URL.Path == "/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicies" {
					listedMetadata = strings.Contains(
						request.Header.Get("Accept"),
						"PartialObjectMetadataList",
					)
					if !listedMetadata {
						fixtureStatus(writer, http.StatusNotAcceptable, "NotAcceptable")

						return true
					}
				}

				if request.URL.Path == fixtureDependencyPath {
					fullReads++

					if changed != "" {
						fixtureMetadata(fixture.objects[fixtureDependencyPath])[changed] = "replacement"
					}
				}

				return false
			}
			resource := schema.GroupVersionResource{
				Group:    "admissionregistration.k8s.io",
				Version:  "v1",
				Resource: "validatingadmissionpolicies",
			}

			err = plan.captureLegacyResource(context.Background(), resource)
			if changed == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "identity changed")
			}

			require.True(t, listedMetadata)
			require.Equal(t, 1, fullReads)
			require.Empty(t, plan.state.Resources)
			require.Len(t, plan.state.Dependencies, 1)
			require.Empty(t, fixture.writes)
		})
	}
}
