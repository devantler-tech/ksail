package calicoinstaller

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCalicoInstallWaitsForOperatorDependencyMigration(t *testing.T) {
	t.Parallel()
	fixture := NewDependencyMigrationForTest(t, "previous")
	desired := map[string]any{"failurePolicy": "Fail", "validations": []any{
		map[string]any{"expression": "true"},
	}}
	observations := 0
	installer := dependencyCompletionInstaller(t, fixture, time.Second, func() {
		observations++

		if observations == 2 {
			fixture.fixture.objects[fixtureDependencyPath]["spec"] = desired
		}
	})

	require.NoError(t, installer.Install(t.Context()))

	version, complete, _, err := fixture.Inventory()
	require.NoError(t, err)
	require.Equal(t, chartVersion(), version)
	require.True(t, complete)
	require.Equal(t, "external-policy", string(fixture.Dependency().GetUID()))

	for _, write := range fixture.Writes() {
		require.NotContains(t, write, "/validatingadmissionpolicies/",
			"waiting for the operator must never apply or adopt its policy")
	}
}

func TestCalicoInstallBoundsOperatorDependencyMigrationWait(t *testing.T) {
	t.Parallel()
	fixture := NewDependencyMigrationForTest(t, "previous")
	installer := dependencyCompletionInstaller(t, fixture, 300*time.Millisecond, func() {})

	err := installer.Install(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)

	version, complete, _, inventoryErr := fixture.Inventory()
	require.NoError(t, inventoryErr)
	require.Equal(t, "previous", version)
	require.False(t, complete)
}

func TestCalicoInstallDoesNotRetryUnsafeDependencyChanges(t *testing.T) {
	t.Parallel()

	changes := map[string]func(map[string]any){
		"unknown content": func(object map[string]any) {
			object["spec"] = map[string]any{"failurePolicy": "Ignore"}
		},
		"replacement": func(object map[string]any) {
			fixtureMetadata(object)["uid"] = "replacement"
		},
		"foreign manager": func(object map[string]any) {
			fixtureMetadata(object)["labels"] = map[string]any{
				"argocd.argoproj.io/instance": "foreign",
			}
		},
		"changed controller": func(object map[string]any) {
			fixtureMetadata(object)["ownerReferences"] = []any{map[string]any{
				"apiVersion": "operator.tigera.io/v1", "kind": "Installation",
				"name": "default", "uid": "replacement-installation", "controller": true,
			}}
		},
	}

	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := NewDependencyMigrationForTest(t, "previous")
			observations := 0
			installer := dependencyCompletionInstaller(t, fixture, time.Second, func() {
				observations++

				change(fixture.fixture.objects[fixtureDependencyPath])
			})

			err := installer.Install(t.Context())
			require.ErrorIs(t, err, errInvalidPrerequisites)
			require.Equal(t, 1, observations, "an unsafe observation must fail immediately")

			version, complete, _, inventoryErr := fixture.Inventory()
			require.NoError(t, inventoryErr)
			require.Equal(t, "previous", version)
			require.False(t, complete)
		})
	}
}

// Only Helm operations are replaced: planning, API provenance checks, polling,
// and inventory persistence execute against the local HTTP fixture.
func dependencyCompletionInstaller(
	t *testing.T, fixture *DependencyMigrationForTest, timeout time.Duration, observe func(),
) *Installer {
	t.Helper()
	client := helm.NewMockInterface(t)
	client.EXPECT().GetReleaseStorageLabels(mock.Anything, "calico", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().GetReleaseStorageLabels(mock.Anything, "calico-crds", prerequisiteNamespace).
		Return(nil, nil)
	client.EXPECT().AddRepository(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	manifest := strings.Replace(prerequisiteFixtureManifest, "{failurePolicy: Fail}",
		"{failurePolicy: Fail, validations: [{expression: 'true'}]}", 1)
	client.EXPECT().TemplateChart(mock.Anything, mock.Anything).Return(manifest, nil)
	client.EXPECT().RefreshDiscovery().Return(nil)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			return spec.ReleaseName == "calico"
		})).
		Run(func(_ context.Context, _ *helm.ChartSpec) {
			fixture.fixture.mu.Lock()
			defer fixture.fixture.mu.Unlock()

			fixture.fixture.beforeGet = func(_ http.ResponseWriter, request *http.Request) bool {
				if request.URL.Path == fixtureDependencyPath {
					observe()
				}

				return false
			}
		}).
		Return(nil, nil).
		Once()

	return NewInstaller(client, fixture.fixture.kubeconfig, "test-context", timeout,
		v1alpha1.DistributionVanilla, false)
}
