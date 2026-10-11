package calicoinstaller

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestCalicoRenderedBundleCannotSupplyLiveIdentity(t *testing.T) {
	t.Parallel()

	manifest := strings.Replace(prerequisiteFixtureManifest,
		"name: installations.operator.tigera.io",
		"name: installations.operator.tigera.io\n  uid: guessed\n  resourceVersion: '7'", 1)
	_, err := parsePrerequisites(manifest)
	require.Error(t, err, "rendered identities cannot grant apply ownership")
}

const fixtureCRDPath = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/installations.operator.tigera.io"

func fixtureCRD(uid, version string) map[string]any {
	return map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
		"metadata": map[string]any{
			"name": "installations.operator.tigera.io", "uid": uid, "resourceVersion": version,
			"labels": map[string]any{prerequisiteOwnerKey: prerequisiteOwner},
		},
	}
}

func TestCalicoApplyRefusesAResourceCreatedAfterPreflight(t *testing.T) {
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
	plan, err := inst.planPrerequisites(context.Background(), prerequisiteFixtureManifest)
	require.NoError(t, err)

	foreign := fixtureCRD("foreign", "8")
	fixtureMetadata(foreign)["labels"] = map[string]any{"owner": "someone-else"}
	fixture.objects[fixtureCRDPath] = foreign
	err = plan.apply(context.Background())
	require.True(
		t,
		apierrors.IsAlreadyExists(err),
		"the preflight/write gap must use create-if-absent",
	)
	require.Equal(
		t,
		foreign,
		fixture.objects[fixtureCRDPath],
		"a competing object must never be adopted or changed",
	)
}

func TestCalicoApplyRefusesOwnershipTransferAfterPreflight(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
	fixture.objects[fixtureCRDPath] = fixtureCRD("owned", "7")
	inst := NewInstaller(
		nil,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	plan, err := inst.planPrerequisites(context.Background(), prerequisiteFixtureManifest)
	require.NoError(t, err)

	meta := fixtureMetadata(fixture.objects[fixtureCRDPath])
	meta["resourceVersion"] = "8"
	fixtureLabels(fixture.objects[fixtureCRDPath])["argocd.argoproj.io/instance"] = "transferred"
	err = plan.apply(context.Background())
	require.Error(t, err)
	require.Equal(
		t,
		"transferred",
		fixtureLabels(fixture.objects[fixtureCRDPath])["argocd.argoproj.io/instance"],
	)
}

func TestCalicoRemovalRefusesOwnershipTransferAfterPreflight(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)
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
	inst := NewInstaller(
		nil,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	plan, err := inst.planPrerequisiteRemoval(context.Background())
	require.NoError(t, err)

	fixture.beforeWrite = func(r *http.Request, _ map[string]any) {
		if r.Method == http.MethodDelete && r.URL.Path == fixtureCRDPath {
			meta := fixtureMetadata(fixture.objects[fixtureCRDPath])
			meta["resourceVersion"] = "8"
			fixtureLabels(fixture.objects[fixtureCRDPath])["kustomize.toolkit.fluxcd.io/name"] = "transferred"
		}
	}
	err = plan.remove(context.Background(), time.Second)
	require.Error(t, err)
	require.NotNil(t, fixture.objects[fixtureCRDPath])
	require.Empty(t, fixture.writes, "neither transferred resources nor inventory may be deleted")
}

func TestCalicoPreflightRejectsGitOpsMarkersByPresence(t *testing.T) {
	t.Parallel()

	for _, marker := range []string{
		"helm.toolkit.fluxcd.io/name", "helm.toolkit.fluxcd.io/namespace",
		"kustomize.toolkit.fluxcd.io/name", "argocd.argoproj.io/instance",
	} {
		t.Run(marker, func(t *testing.T) {
			t.Parallel()
			fixture := newPrerequisiteFixture(t)
			fixture.objects[fixtureCRDPath] = fixtureCRD("owned", "7")
			fixtureLabels(fixture.objects[fixtureCRDPath])[marker] = ""
			inst := NewInstaller(
				nil,
				fixture.kubeconfig,
				"test-context",
				time.Second,
				v1alpha1.DistributionVanilla,
				false,
			)
			_, err := inst.planPrerequisites(context.Background(), prerequisiteFixtureManifest)
			require.ErrorContains(t, err, "ownership")
			require.Empty(t, fixture.writes)
		})
	}
}

func TestCalicoPreflightValidatesTheWholeBundleBeforeWriting(t *testing.T) {
	t.Parallel()

	for name, suffix := range map[string]string{
		"malformed later document": "---\nmetadata: [\n",
		"duplicate CRD":            "---\n" + prerequisiteFixtureManifest,
		"unexpected namespaced resource": "---\napiVersion: v1\nkind: Secret\n" +
			"metadata: {name: unrelated, namespace: default}\n",
	} {
		t.Run(name, func(t *testing.T) {
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
			_, err := inst.planPrerequisites(
				context.Background(),
				prerequisiteFixtureManifest+suffix,
			)
			require.Error(t, err)
			require.Empty(t, fixture.writes)
		})
	}
}

func TestCalicoPrerequisitesRetainLegacyHelmStorage(t *testing.T) {
	t.Parallel()
	fixture := newPrerequisiteFixture(t)

	const storagePath = "/api/v1/namespaces/tigera-operator/secrets/sh.helm.release.v1.calico-crds.v1"

	storage := map[string]any{
		"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{
			"name":            "sh.helm.release.v1.calico-crds.v1",
			"namespace":       prerequisiteNamespace,
			"uid":             "legacy",
			"resourceVersion": "4",
			"labels":          map[string]any{"owner": "helm", "name": "calico-crds"},
		},
		"data": map[string]any{"release": "cmV0YWluZWQ="},
	}
	fixture.objects[storagePath] = storage
	inst := NewInstaller(
		nil,
		fixture.kubeconfig,
		"test-context",
		time.Second,
		v1alpha1.DistributionVanilla,
		false,
	)
	plan, err := inst.planPrerequisites(context.Background(), prerequisiteFixtureManifest)
	require.NoError(t, err)
	require.NoError(t, plan.apply(context.Background()))
	require.Equal(
		t,
		storage,
		fixture.objects[storagePath],
		"migration must preserve the old release record",
	)
	require.NoError(t, plan.established(context.Background(), time.Second))
	require.Len(
		t,
		plan.state.Resources,
		3,
		"the CRD, admission policy and retained storage each need an identity",
	)
	data, err := json.Marshal(plan.state)
	require.NoError(t, err)
	require.Less(t, len(data), 4096, "the inventory must store identities rather than CRD schemas")
}

func TestCalicoPreflightRefusesConflictingHelmOwnership(t *testing.T) {
	t.Parallel()

	for _, removal := range []bool{false, true} {
		t.Run(strconv.FormatBool(removal), func(t *testing.T) {
			t.Parallel()
			fixture := newPrerequisiteFixture(t)
			fixture.objects[fixtureCRDPath] = fixtureCRD("owned", "7")
			fixtureMetadata(fixture.objects[fixtureCRDPath])["annotations"] = map[string]any{
				"meta.helm.sh/release-name":      "foreign",
				"meta.helm.sh/release-namespace": "another-namespace",
			}
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
			inst := NewInstaller(
				nil,
				fixture.kubeconfig,
				"test-context",
				time.Second,
				v1alpha1.DistributionVanilla,
				false,
			)

			var err error
			if removal {
				_, err = inst.planPrerequisiteRemoval(context.Background())
			} else {
				_, err = inst.planPrerequisites(context.Background(), prerequisiteFixtureManifest)
			}

			require.ErrorContains(t, err, "ownership")
			require.Empty(t, fixture.writes)
		})
	}
}
