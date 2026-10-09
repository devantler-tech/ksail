package calicoinstaller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	fixtureDependencyPath   = "/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicies/calico-fixture"
	fixtureInstallationPath = "/apis/operator.tigera.io/v1/installations/default"
)

func seedOperatorDependency(t *testing.T, fixture *prerequisiteFixture) {
	t.Helper()
	fixture.seedOperatorHistory()
	fixture.objects[fixtureInstallationPath] = map[string]any{
		"apiVersion": "operator.tigera.io/v1", "kind": "Installation",
		"metadata": map[string]any{
			"name": "default", "uid": "installation-uid", "resourceVersion": "5",
			"labels": map[string]any{"app.kubernetes.io/managed-by": "Helm"},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      "calico",
				"meta.helm.sh/release-namespace": "tigera-operator",
			},
		},
	}
	fixture.objects[fixtureDependencyPath] = map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicy",
		"metadata": map[string]any{
			"name": "calico-fixture", "uid": "external-policy", "resourceVersion": "6",
			"labels": map[string]any{"operator.tigera.io/validating-admission-policy": "managed"},
			"ownerReferences": []any{map[string]any{
				"apiVersion": "operator.tigera.io/v1", "kind": "Installation",
				"name": "default", "uid": "installation-uid", "controller": true,
			}},
		},
		"spec": map[string]any{"failurePolicy": "Fail"},
	}
}

func dependencyInstaller(fixture *prerequisiteFixture) *Installer {
	return NewInstaller(nil, fixture.kubeconfig, "test-context", time.Second,
		v1alpha1.DistributionVanilla, false)
}

func TestCalicoObservesBoundOperatorPrerequisiteWithoutAdoptingIt(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	seedOperatorDependency(t, fixture)

	ctx := context.Background()
	plan, err := dependencyInstaller(fixture).planPrerequisites(ctx, prerequisiteFixtureManifest)
	require.NoError(t, err)
	require.NoError(t, plan.apply(ctx))

	for _, write := range fixture.writes {
		require.NotContains(t, write, fixtureDependencyPath,
			"an external operator prerequisite must never be applied or labelled")
	}

	require.Len(t, plan.state.Resources, 1)
	require.Equal(
		t,
		"external-policy",
		fixtureMetadata(fixture.objects[fixtureDependencyPath])["uid"],
	)
	inventory, recorded, err := readInstallerInventory(ctx, dependencyInstaller(fixture))
	require.NoError(t, err)
	require.NotNil(t, inventory)
	require.Len(t, recorded.Dependencies, 1)
	require.Equal(t, "external-policy", string(recorded.Dependencies[0].UID))

	removal, err := dependencyInstaller(fixture).planPrerequisiteRemoval(ctx)
	require.NoError(t, err)

	for _, ref := range removal.plan.state.Resources {
		require.NotEqual(t, "external-policy", string(ref.UID),
			"external operator objects cannot enter the removal inventory")
	}
}

func TestCalicoRefusesUnprovenOrChangedOperatorPrerequisitesBeforeWrites(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*prerequisiteFixture){
		"missing history":      func(f *prerequisiteFixture) { delete(f.objects, fixtureOperatorHistoryPath) },
		"missing installation": func(f *prerequisiteFixture) { delete(f.objects, fixtureInstallationPath) },
		"wrong controller UID": func(f *prerequisiteFixture) {
			fixtureMetadata(f.objects[fixtureDependencyPath])["ownerReferences"] = []any{
				map[string]any{
					"apiVersion": "operator.tigera.io/v1", "kind": "Installation",
					"name": "default", "uid": "another-installation", "controller": true,
				},
			}
		},
		"changed policy": func(f *prerequisiteFixture) {
			f.objects[fixtureDependencyPath]["spec"] = map[string]any{"failurePolicy": "Ignore"}
		},
		"foreign helm owner": func(f *prerequisiteFixture) {
			fixtureMetadata(f.objects[fixtureDependencyPath])["annotations"] = map[string]any{
				"meta.helm.sh/release-name": "foreign",
			}
		},
		"terminating": func(f *prerequisiteFixture) {
			fixtureMetadata(f.objects[fixtureDependencyPath])["deletionTimestamp"] = "2026-01-01T00:00:00Z"
		},
		"operator label alone": func(f *prerequisiteFixture) {
			delete(fixtureMetadata(f.objects[fixtureDependencyPath]), "ownerReferences")
		},
		"foreign installation": func(f *prerequisiteFixture) {
			fixtureMetadata(f.objects[fixtureInstallationPath])["annotations"] = map[string]any{
				"meta.helm.sh/release-name":      "other",
				"meta.helm.sh/release-namespace": "tigera-operator",
			}
		},
		"changed expression": func(f *prerequisiteFixture) {
			f.objects[fixtureDependencyPath]["spec"] = map[string]any{
				"failurePolicy": "Fail",
				"validations":   []any{map[string]any{"expression": "false"}},
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newPrerequisiteFixture(t)
			seedOperatorDependency(t, fixture)
			mutate(fixture)
			_, err := dependencyInstaller(
				fixture,
			).planPrerequisites(context.Background(), prerequisiteFixtureManifest)
			require.Error(t, err)
			require.Empty(t, fixture.writes)
		})
	}
}

func TestCalicoRefusesOperatorDependencyReplacementBeforeWritesAndOnRetry(t *testing.T) {
	t.Parallel()

	for _, applied := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "before writes", true: "interrupted retry"}[applied],
			func(t *testing.T) {
				t.Parallel()
				fixture := newPrerequisiteFixture(t)
				seedOperatorDependency(t, fixture)
				installer := dependencyInstaller(fixture)
				ctx := context.Background()
				plan, err := installer.planPrerequisites(ctx, prerequisiteFixtureManifest)
				require.NoError(t, err)

				if applied {
					require.NoError(t, plan.apply(ctx))
				}

				writes := len(fixture.writes)
				fixtureMetadata(fixture.objects[fixtureDependencyPath])["uid"] = "replacement"

				if applied {
					_, err = installer.planPrerequisites(ctx, prerequisiteFixtureManifest)
				} else {
					err = plan.apply(ctx)
				}

				require.Error(t, err)
				require.Len(t, fixture.writes, writes)
			},
		)
	}
}

func TestCalicoRefusesPreviouslyOwnedPrerequisiteReclassification(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	seedOperatorDependency(t, fixture)
	fixture.setInventory(t, prerequisiteState{Resources: []prerequisiteRef{
		{
			Group:    "admissionregistration.k8s.io",
			Version:  "v1",
			Resource: "validatingadmissionpolicies",
			Name:     "calico-fixture",
			UID:      "external-policy",
		},
	}})
	_, err := dependencyInstaller(
		fixture,
	).planPrerequisites(context.Background(), prerequisiteFixtureManifest)
	require.ErrorContains(t, err, "cannot become an external dependency")
	require.Empty(t, fixture.writes)
}

func TestCalicoDependencyFingerprintAllowsOnlyAdmissionDefaults(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	seedOperatorDependency(t, fixture)

	manifest := strings.Replace(
		prerequisiteFixtureManifest,
		"spec: {failurePolicy: Fail}",
		"spec: {matchConstraints: {resourceRules: [{apiGroups: [''], apiVersions: [v1], "+
			"operations: [CREATE], resources: [pods]}]}}",
		1,
	)
	fixture.objects[fixtureDependencyPath]["spec"] = map[string]any{
		"failurePolicy": "Fail", "matchConstraints": map[string]any{
			"matchPolicy":       "Equivalent",
			"namespaceSelector": map[string]any{},
			"objectSelector":    map[string]any{},
			"resourceRules": []any{map[string]any{
				"apiGroups": []any{""}, "apiVersions": []any{"v1"}, "operations": []any{"CREATE"},
				"resources": []any{"pods"}, "scope": "*",
			}},
		},
	}
	plan, err := dependencyInstaller(fixture).planPrerequisites(context.Background(), manifest)
	require.NoError(t, err, "the API server's official defaults are equivalent to the chart")

	spec, ok := fixture.objects[fixtureDependencyPath]["spec"].(map[string]any)
	require.True(t, ok)

	spec["failurePolicy"] = "Ignore"

	require.Error(t, plan.revalidateDependencies(context.Background()))
	require.Empty(t, fixture.writes, "changed policy semantics cannot be accepted")
}

func TestCalicoDependencyAPIVersionTransitionPreservesIncarnation(t *testing.T) {
	t.Parallel()

	for _, replaced := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "same UID", true: "replacement"}[replaced],
			func(t *testing.T) {
				t.Parallel()
				fixture := newPrerequisiteFixture(t)
				seedOperatorDependency(t, fixture)
				policy := fixture.objects[fixtureDependencyPath]
				digest, err := prerequisiteSpecDigest(&unstructured.Unstructured{Object: policy})
				require.NoError(t, err)
				fixture.setInventory(t, prerequisiteState{Dependencies: []prerequisiteDependency{{
					prerequisiteRef: prerequisiteRef{
						Group:    admissionRegistrationGroup,
						Version:  "v1beta1",
						Resource: "validatingadmissionpolicies",
						Name:     "calico-fixture",
						UID:      "external-policy",
					},
					InstallationUID: "installation-uid", SpecSHA256: digest,
				}}})
				installer := dependencyInstaller(fixture)
				ctx := context.Background()
				_, recorded, err := readInstallerInventory(ctx, installer)
				require.NoError(t, err)
				client, core, meta, err := installer.prerequisiteClients()
				require.NoError(t, err)

				observation := &prerequisitePlan{
					client: client,
					core:   core,
					meta:   meta,
					state:  recorded,
				}

				if replaced {
					fixtureMetadata(policy)["uid"] = "replacement"
				}

				err = observation.revalidateDependencies(ctx)
				plan, planErr := installer.planPrerequisites(ctx, prerequisiteFixtureManifest)

				if replaced {
					require.Error(t, err)
					require.Error(t, planErr)
				} else {
					require.NoError(t, err)
					require.NoError(t, planErr)
					require.Equal(t, "v1", observation.state.Dependencies[0].Version)
					require.Equal(t, "v1", plan.state.Dependencies[0].Version)
				}

				require.Empty(t, fixture.writes, "an API transition only observes the same object")
			},
		)
	}
}

func TestCalicoObservesAllOperatorAdmissionKindsAndRefusesChangesAtCompletion(t *testing.T) {
	t.Parallel()

	kinds := map[string]string{
		"MutatingAdmissionPolicy":          "mutatingadmissionpolicies",
		"MutatingAdmissionPolicyBinding":   "mutatingadmissionpolicybindings",
		"ValidatingAdmissionPolicy":        "validatingadmissionpolicies",
		"ValidatingAdmissionPolicyBinding": "validatingadmissionpolicybindings",
	}
	for kind, resource := range kinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			fixture := newPrerequisiteFixture(t)
			seedOperatorDependency(t, fixture)
			policy := fixture.objects[fixtureDependencyPath]
			delete(fixture.objects, fixtureDependencyPath)

			path := "/apis/" + admissionRegistrationGroup + "/v1/" + resource + "/calico-fixture"
			fixture.objects[path] = policy
			policy["kind"] = kind

			label := "operator.tigera.io/validating-admission-policy"
			if strings.HasPrefix(kind, "Mutating") {
				label = "operator.tigera.io/mutating-admission-policy"
			}

			fixtureMetadata(policy)["labels"] = map[string]any{label: "managed"}
			if strings.HasSuffix(kind, "Binding") {
				policy["spec"] = map[string]any{"policyName": "calico-policy"}
			}

			spec, err := json.Marshal(policy["spec"])
			require.NoError(t, err)

			manifest := strings.ReplaceAll(
				prerequisiteFixtureManifest,
				"ValidatingAdmissionPolicy",
				kind,
			)
			manifest = strings.Replace(manifest, "{failurePolicy: Fail}", string(spec), 1)
			ctx := context.Background()
			plan, err := dependencyInstaller(fixture).planPrerequisites(ctx, manifest)
			require.NoError(t, err)
			require.NoError(t, plan.apply(ctx))

			for _, write := range fixture.writes {
				require.NotContains(t, write, path)
			}

			writes := len(fixture.writes)
			policy["spec"] = map[string]any{
				"policyName":    "another-policy",
				"failurePolicy": "Ignore",
			}

			require.Error(t, plan.complete(ctx))
			require.False(t, plan.state.Complete)
			require.Len(t, fixture.writes, writes)
		})
	}
}

func TestCalicoRefusesOverlappingDependencyInventoryBeforeWrites(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	seedOperatorDependency(t, fixture)

	ref := prerequisiteRef{
		Group: admissionRegistrationGroup, Version: "v1",
		Resource: "validatingadmissionpolicies", Name: "calico-fixture", UID: "external-policy",
	}
	fixture.setInventory(t, prerequisiteState{
		Resources: []prerequisiteRef{ref},
		Dependencies: []prerequisiteDependency{{
			prerequisiteRef: ref,
			InstallationUID: "installation-uid", SpecSHA256: strings.Repeat("a", 64),
		}},
	})
	_, err := dependencyInstaller(
		fixture,
	).planPrerequisites(context.Background(), prerequisiteFixtureManifest)
	require.ErrorContains(t, err, "overlapping identities")
	require.Empty(t, fixture.writes)
}
