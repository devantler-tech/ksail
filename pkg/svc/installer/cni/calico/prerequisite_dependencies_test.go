package calicoinstaller_test

import (
	"strings"
	"testing"

	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCalicoDependencyMigrationRefusesMissingTargetBeforeWrites(t *testing.T) {
	t.Parallel()
	fixture := calicoinstaller.NewDependencyMigrationForTest(t, "previous")
	manifest := strings.Split(calicoinstaller.PrerequisiteManifestForTest, "\n---\n")[0]
	require.NoError(t, fixture.PlanManifest(manifest))
	require.ErrorContains(t, fixture.Apply(), "absent from the new chart")
	require.Empty(t, fixture.Writes())
}

func TestCalicoDependencyChartMigration(t *testing.T) {
	t.Parallel()

	for _, retry := range []string{"none", "before operator upgrade", "after operator upgrade"} {
		t.Run(retry, func(t *testing.T) {
			t.Parallel()
			fixture := calicoinstaller.NewDependencyMigrationForTest(t, "previous")
			desired := map[string]any{"failurePolicy": "Fail", "validations": []any{
				map[string]any{"expression": "object.metadata.name != 'forbidden'"},
			}}
			_, _, oldDigest, err := fixture.Inventory()
			require.NoError(t, err)
			require.NoError(t, fixture.Plan(desired))
			require.NoError(t, fixture.Apply())
			version, complete, digest, err := fixture.Inventory()
			require.NoError(t, err)
			require.Equal(
				t,
				"previous",
				version,
				"an interrupted upgrade must retain its source version",
			)
			require.False(t, complete)
			require.Equal(t, oldDigest, digest)

			if retry == "before operator upgrade" {
				require.NoError(t, fixture.Plan(desired))
				require.NoError(t, fixture.Apply())
			}

			policy := fixture.Dependency()
			policy.Object["spec"] = desired
			fixture.SetDependency(policy)

			if retry == "after operator upgrade" {
				require.NoError(t, fixture.Plan(desired))
				require.NoError(t, fixture.Apply())
			}

			require.NoError(t, fixture.Complete())
			version, complete, digest, err = fixture.Inventory()
			require.NoError(t, err)
			require.NotEqual(t, "previous", version)
			require.True(t, complete)

			wantedDigest, err := calicoinstaller.PrerequisiteSpecDigestForTest(policy)
			require.NoError(t, err)
			require.Equal(t, wantedDigest, digest)
			require.NotEqual(t, oldDigest, digest)

			for _, write := range fixture.Writes() {
				require.NotContains(t, write, "/validatingadmissionpolicies/",
					"KSail must never apply or adopt operator-owned dependencies")
			}
		})
	}
}

func TestCalicoDependencyMigrationRefusesChangedProvenanceAndUnknownContent(t *testing.T) {
	t.Parallel()

	changes := map[string]func(*unstructured.Unstructured){
		"replacement": func(object *unstructured.Unstructured) { object.SetUID("replacement") },
		"unknown content": func(object *unstructured.Unstructured) {
			object.Object["spec"] = map[string]any{"failurePolicy": "Ignore"}
		},
		"changed controller": func(object *unstructured.Unstructured) {
			owners := object.GetOwnerReferences()
			owners[0].UID = "replacement-installation"
			object.SetOwnerReferences(owners)
		},
		"GitOps ownership": func(object *unstructured.Unstructured) {
			labels := object.GetLabels()
			labels["argocd.argoproj.io/instance"] = "foreign"
			object.SetLabels(labels)
		},
		"Helm ownership": func(object *unstructured.Unstructured) {
			object.SetAnnotations(map[string]string{"meta.helm.sh/release-name": "foreign"})
		},
	}
	for _, stage := range []string{"before writes", "after operator upgrade"} {
		for name, mutate := range changes {
			t.Run(stage+"/"+name, func(t *testing.T) {
				t.Parallel()
				fixture := calicoinstaller.NewDependencyMigrationForTest(t, "previous")

				desired := map[string]any{"failurePolicy": "Fail", "validations": []any{
					map[string]any{"expression": "true"},
				}}
				if stage == "after operator upgrade" {
					require.NoError(t, fixture.Plan(desired))
					require.NoError(t, fixture.Apply())
				}

				policy := fixture.Dependency()
				if stage == "after operator upgrade" {
					policy.Object["spec"] = desired
				}

				mutate(policy)
				fixture.SetDependency(policy)

				writes := fixture.Writes()
				if stage == "before writes" {
					require.Error(t, fixture.Plan(desired))
				} else {
					require.Error(t, fixture.Complete())
				}

				require.Equal(t, writes, fixture.Writes())
				version, complete, _, err := fixture.Inventory()
				require.NoError(t, err)
				require.Equal(t, "previous", version)
				require.Equal(t, stage == "before writes", complete)
			})
		}
	}
}

func TestCalicoDependencyMigrationCannotCompleteBeforeOperatorContentChanges(t *testing.T) {
	t.Parallel()
	fixture := calicoinstaller.NewDependencyMigrationForTest(t, "previous")
	desired := map[string]any{"failurePolicy": "Fail", "validations": []any{
		map[string]any{"expression": "true"},
	}}
	require.NoError(t, fixture.Plan(desired))
	require.NoError(t, fixture.Apply())
	_, _, oldDigest, err := fixture.Inventory()
	require.NoError(t, err)

	writes := fixture.Writes()
	require.ErrorContains(t, fixture.Complete(), "content differs from the chart")
	version, complete, digest, err := fixture.Inventory()
	require.NoError(t, err)
	require.Equal(t, "previous", version)
	require.False(t, complete)
	require.Equal(t, oldDigest, digest)
	require.Equal(t, writes, fixture.Writes())
}

func TestCalicoDependencyCurrentChartStillRefusesDifferentContent(t *testing.T) {
	t.Parallel()
	fixture := calicoinstaller.NewDependencyMigrationForTest(
		t,
		calicoinstaller.CalicoChartVersionForTest(),
	)
	desired := map[string]any{"failurePolicy": "Fail", "validations": []any{
		map[string]any{"expression": "true"},
	}}
	require.ErrorContains(t, fixture.Plan(desired), "content differs from the chart")
	require.Empty(t, fixture.Writes())
}
